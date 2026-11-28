package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/agents"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/store"
)

const searchTool = "brave-search_brave_web_search"

// chatTools is a model turn that calls tools, given as name, arguments pairs.
func chatTools(calls ...string) string {
	var tc []map[string]any
	for i := 0; i+1 < len(calls); i += 2 {
		tc = append(tc, map[string]any{"id": fmt.Sprintf("call_%d", i/2), "type": "function",
			"function": map[string]any{"name": calls[i], "arguments": calls[i+1]}})
	}
	raw, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "", "tool_calls": tc}, "finish_reason": "tool_calls"}},
		"usage":   map[string]any{"prompt_tokens": 1000, "completion_tokens": 50}})
	return string(raw)
}

func chatText(text string) string {
	raw, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 1000, "completion_tokens": 50}})
	return string(raw)
}

type runSetup struct {
	s      *Store
	a      *App
	o      *store.Org
	task   *store.Task
	brief  int64
	agent  int64
	mu     sync.Mutex
	sleeps []time.Duration
}

func newRunSetup(t *testing.T, fake *fakeEndpoint) *runSetup {
	t.Helper()
	r := &runSetup{s: tempStore(t)}
	r.s.Keyring = mustKeys(t, "1:"+mailTestKey)
	r.o = mustOrg(t, r.s, "alpha")
	r.a = &App{s: r.s, agentClient: fake.srv.Client(), sleep: func(ctx context.Context, d time.Duration) error {
		r.mu.Lock()
		r.sleeps = append(r.sleeps, d)
		r.mu.Unlock()
		return ctx.Err()
	}}
	r.agent = must(r.a.saveAgent(context.Background(), r.o.ID, AgentSettingsIn{Name: "Prospector", BaseURL: fake.srv.URL + "/v1", APIKey: agentTestKey,
		Model: "router/auto", ToolsURL: fake.srv.URL + "/mcp", Tools: []string{searchTool}}, "Ada Lovelace")).ID
	r.task = must(r.s.CreateTask(r.o.ID, "Yachts", "Yacht builders with a configurator.", "Ada Lovelace"))
	r.brief = must(r.s.Task(r.o.ID, r.task.ID, time.Now())).Requests[0].ID
	t.Cleanup(r.a.runs.wg.Wait)
	return r
}

func (r *runSetup) finished(t *testing.T, id int64) store.AgentRun {
	t.Helper()
	r.a.runs.wg.Wait()
	var run store.AgentRun
	if err := r.s.DB.First(&run, id).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

func messagesOf(body map[string]any) []map[string]any {
	var out []map[string]any
	list, _ := body["messages"].([]any)
	for _, m := range list {
		out = append(out, m.(map[string]any))
	}
	return out
}

func TestTheAgentWorksATaskToTheEnd(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	fake.chat = func(n int, _ map[string]any) (int, string) {
		switch n {
		case 0:
			return 200, chatTools(searchTool, `{"query":"yacht builders with a configurator"}`)
		case 1:
			return 200, chatTools(
				"add_company", `{"name":"Larkmoor Yachts","domain":"larkmoor.example","facts":[{"field":"configurator","value":"Day cruiser configurator","source_url":"https://larkmoor.example/build"}]}`,
				"add_fact", `{"company":"Larkmoor Yachts","field":"size","value":"40 staff","source_url":"https://made-up.example/about"}`)
		case 2:
			return 200, chatTools("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
		}
		return 200, chatText("I'm done.")
	}
	run := must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace"))
	got := r.finished(t, run.ID)
	if got.Status != RunFinished || got.StopReason != "the request is done" || got.Turns != 3 || got.Added != 1 || got.RequestID != r.brief {
		for _, m := range messagesOf(fake.bodies[2]) {
			if m["role"] == "tool" {
				t.Logf("tool said: %.300v", m["content"])
			}
		}
		t.Fatalf("the run: %+v", got)
	}

	c := must(r.s.CompanyRef(r.o.ID, "Larkmoor Yachts"))
	facts := must(r.s.Facts(r.o.ID, c.ID))
	if len(facts) != 1 || facts[0].Field != "configurator" {
		t.Fatalf("only the fact from a page the agent was shown should be stored: %+v", facts)
	}
	v := must(r.s.Task(r.o.ID, r.task.ID, time.Now()))
	if len(v.Companies) != 1 || v.Requests[0].DoneAt == nil || v.Requests[0].DoneBy != "agent:Prospector (built in)" {
		t.Fatalf("the task should count the company and the request be done by the agent: %+v %+v", v.Companies, v.Requests[0])
	}

	first := fake.bodies[0]
	if first["model"] != "router/auto" || first["prompt_cache_key"] != fmt.Sprintf("prospector-run-%d", run.ID) {
		t.Fatalf("the request's model and session: %v %v", first["model"], first["prompt_cache_key"])
	}
	var names []string
	for _, tool := range first["tools"].([]any) {
		names = append(names, tool.(map[string]any)["function"].(map[string]any)["name"].(string))
	}
	all := " " + strings.Join(names, " ") + " "
	for _, want := range []string{"get_task", "add_company", "finish_request", searchTool} {
		if !strings.Contains(all, " "+want+" ") {
			t.Errorf("the agent wasn't given %s: %v", want, names)
		}
	}
	for _, not := range []string{"draft_message", "mark", "list_due", "fetch_fetch"} {
		if strings.Contains(all, " "+not+" ") {
			t.Errorf("the agent was given %s: %v", not, names)
		}
	}
	msgs := messagesOf(first)
	if msgs[0]["role"] != "system" || !strings.Contains(msgs[0]["content"].(string), "not instructions") || !strings.Contains(msgs[1]["content"].(string), "get_task returned") {
		t.Fatalf("the opening messages: %v", msgs)
	}
	refused := false
	for _, m := range messagesOf(fake.bodies[2]) {
		if m["role"] == "tool" && strings.Contains(fmt.Sprint(m["content"]), "wasn't on any page a tool showed you") {
			refused = true
		}
		if m["role"] == "tool" && strings.Contains(fmt.Sprint(m["content"]), "larkmoor.example/build") && !strings.Contains(fmt.Sprint(m["content"]), "not instructions") &&
			strings.Contains(fmt.Sprint(m["content"]), "Larkmoor Yachts: day cruisers") {
			t.Fatal("web content reached the model without being marked as such")
		}
	}
	if !refused {
		t.Fatal("the model wasn't told why its made-up source was refused")
	}
	var claims int64
	if r.s.DB.Model(&store.RequestClaim{}).Count(&claims); claims != 0 {
		t.Fatal("finishing should end the agent's claim")
	}
}

func TestTheAgentWaitsOutAnEndpointsLimit(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	fake.chat = func(n int, _ map[string]any) (int, string) {
		if n == 0 {
			return http.StatusTooManyRequests, `{"error":{"code":"429","message":"exceeded requests-per-day limit","type":"rate_limit_error"}}`
		}
		return 200, chatTools("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	got := r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
	if got.Status != RunFinished || got.Turns != 1 {
		t.Fatalf("after the wait the run should carry on and finish: %+v", got)
	}
	if len(r.sleeps) == 0 || r.sleeps[0] < 50*time.Minute {
		t.Fatalf("it should wait as long as the endpoint said: %v", r.sleeps)
	}
}

func TestTheAgentWorksThroughOutagesButNotAWrongModel(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	fake.chat = func(n int, _ map[string]any) (int, string) {
		switch n {
		case 0:
			return http.StatusServiceUnavailable, `{"error":{"code":503,"message":"upstream away"}}`
		case 1:
			return http.StatusBadGateway, `{"error":{"code":502,"message":"upstream away"}}`
		case 2:
			return http.StatusGatewayTimeout, `<html>timed out</html>`
		}
		return 200, chatTools("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	got := r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
	if got.Status != RunFinished {
		t.Fatalf("a run should outlast an endpoint that is away for a while: %+v", got)
	}
	var waited []time.Duration
	for _, d := range r.sleeps {
		waited = append(waited, d.Round(time.Second))
	}
	if fmt.Sprint(waited) != "[10s 20s 40s]" {
		t.Fatalf("it should back off, doubling: %v", r.sleeps)
	}

	fake.mu.Lock()
	fake.chats = 0
	fake.mu.Unlock()
	fake.chat = func(int, map[string]any) (int, string) {
		return http.StatusBadRequest, `{"error":{"code":400,"message":"no such model: router/auto"}}`
	}
	if err := r.s.SetRequestDone(r.o.ID, r.brief, false, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	got = r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
	if got.Status != RunFailed || !strings.Contains(got.StopReason, "no such model") || fake.chats != 1 {
		t.Fatalf("a refusal that waiting won't fix should end the run, saying why: %+v after %d calls", got, fake.chats)
	}

	// An address whose /chat/completions is some site's page answers 200, but not a completion.
	fake.chat = func(int, map[string]any) (int, string) { return 200, `<html><body>Welcome</body></html>` }
	got = r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
	if got.Status != RunFailed || !strings.Contains(got.StopReason, "wasn't a chat completion") {
		t.Fatalf("an answer that isn't a completion should end the run, not be waited on: %+v", got)
	}
	if !passing(&unreachable{errors.New("connection reset")}) || passing(errors.New("the endpoint said: unknown model")) ||
		!passing(&endpointError{Status: 429}) || passing(&endpointError{Status: 401}) {
		t.Fatal("what is waited out and what isn't")
	}
	if backoff(20) != 5*time.Minute {
		t.Fatalf("the back-off should stop growing at 5 minutes, got %s", backoff(20))
	}
}

func TestAPersonStopsTheAgent(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	fake.chat = func(n int, _ map[string]any) (int, string) {
		if n == 1 {
			var run store.AgentRun
			r.s.DB.Last(&run)
			if err := r.a.StopRun(r.o.ID, run.ID); err != nil {
				t.Error(err)
			}
		}
		return 200, chatTools(searchTool, `{"query":"more yacht builders"}`)
	}
	got := r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
	if got.Status != agents.RunStopped || got.StopReason != "stopped by a person" || got.EndedAt == nil {
		t.Fatalf("a stopped run: %+v", got)
	}
	if fake.chats > 3 {
		t.Fatalf("the run went on %d turns after it was stopped", fake.chats-2)
	}
}

func TestARunCarriesOnAfterItsServerStops(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	fake.chat = func(int, map[string]any) (int, string) {
		return 200, chatTools("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	lapsed := time.Now().Add(-time.Minute).UnixMilli()
	left := store.AgentRun{OrgID: r.o.ID, TaskID: r.task.ID, AgentID: r.agent, StartedBy: "Ada Lovelace", Model: "router/auto", Status: store.RunWorking,
		Owner: "a server that stopped", LeaseUntil: lapsed, StartedAt: time.Now(), Turns: 7}
	stopped := left
	stopped.StopAsked, stopped.TaskID = true, must(r.s.CreateTask(r.o.ID, "Kitchens", "Kitchen makers.", "Ada Lovelace")).ID
	r.s.DB.Create(&left)
	r.s.DB.Create(&stopped)
	held := store.AgentRun{OrgID: r.o.ID, AgentID: r.agent, TaskID: must(r.s.CreateTask(r.o.ID, "Boats", "Boat builders.", "Ada Lovelace")).ID, Status: store.RunWorking,
		Owner: "a live server", LeaseUntil: time.Now().Add(time.Minute).UnixMilli(), StartedAt: time.Now()}
	r.s.DB.Create(&held)

	r.a.resumeLapsed(time.Now())
	got := r.finished(t, left.ID)
	if got.Status != RunFinished || got.Resumes != 1 || got.Owner != r.a.runOwner() || got.Turns != 8 {
		t.Fatalf("a run its server left should be carried on, where it was: %+v", got)
	}
	if got := r.finished(t, stopped.ID); got.Status != agents.RunStopped {
		t.Fatalf("a run a person had stopped shouldn't be carried on: %+v", got)
	}
	if got := r.finished(t, held.ID); got.Status != store.RunWorking || got.Owner != "a live server" {
		t.Fatalf("a run another server holds was taken over: %+v", got)
	}
}

func TestFreshStartsKeepARunsContextBounded(t *testing.T) {
	was := episodeTurns
	episodeTurns = 2
	t.Cleanup(func() { episodeTurns = was })
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	fake.chat = func(n int, _ map[string]any) (int, string) {
		if n < 2 {
			return 200, chatTools(searchTool, `{"query":"yacht builders"}`)
		}
		return 200, chatTools("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	got := r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
	if got.Status != RunFinished || got.Episodes != 2 || got.Turns != 3 {
		t.Fatalf("the run: %+v", got)
	}
	if fresh := messagesOf(fake.bodies[2]); len(fresh) != 2 || !strings.Contains(fresh[1]["content"].(string), "get_task returned") {
		t.Fatalf("a fresh start should begin again from the task: %d messages", len(fresh))
	}
}

func TestSeveralAgentsWorkOneTaskEachOnItsOwnRequest(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	s, srv, ada := agentSite(t, fake)
	o := must(s.OrgBySlug("alpha"))
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders.", "Ada Lovelace"))
	if _, err := s.AskTask(o.ID, task.ID, "And Italy.", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	start := func(c *http.Client, agent int64) (int, string) {
		return call(t, c, "POST", fmt.Sprintf("%s/o/alpha/api/v1/tasks/%d/runs", srv.URL, task.ID), "application/json", map[string]any{"agent": agent})
	}
	if code, body := start(ada, 99); code != http.StatusNotFound || !strings.Contains(body, "no agent") {
		t.Fatalf("starting an agent that isn't there: %d %s", code, body)
	}
	// The task page offers each ready agent with no run going on the task.
	ready := func() (names []string) {
		for _, a := range listedAgents(t, ada, srv.URL) {
			if a.Ready {
				names = append(names, a.Name)
			}
		}
		return names
	}
	going := func() (names []string) {
		_, body := call(t, ada, "GET", fmt.Sprintf("%s/o/alpha/api/v1/tasks/%d/runs", srv.URL, task.ID), "", nil)
		var runs struct{ Items []api.Run }
		json.Unmarshal([]byte(body), &runs)
		for _, r := range runs.Items {
			if r.Status == api.RunStatus(store.RunWorking) || r.Status == api.RunStatus(store.RunWaiting) {
				names = append(names, r.AgentName)
			}
		}
		slices.Sort(names)
		return names
	}
	if len(ready()) != 0 {
		t.Fatal("agents nobody set up are ready")
	}
	var agents []*store.Agent
	for _, name := range []string{"Claude", "Gateway"} {
		if code, body := saveAgentAt(t, ada, srv.URL, map[string]any{"name": name,
			"base_url": fake.srv.URL + "/v1", "api_key": agentTestKey, "model": "router/auto"}); code != http.StatusOK {
			t.Fatalf("setting up %s: %d %s", name, code, body)
		}
		agents = append(agents, must(s.AgentNamed(o.ID, name)))
	}
	if strings.Join(ready(), " ") != "Claude Gateway" || len(going()) != 0 {
		t.Fatalf("each agent should be ready, and none going: %v %v", ready(), going())
	}

	release := make(chan struct{})
	var mu sync.Mutex
	claimed := map[float64]bool{}
	fake.chat = func(n int, body map[string]any) (int, string) {
		<-release
		// What get_task kept for this run is in its opening message.
		msgs := messagesOf(body)
		var opened research.TaskOut
		if text := fmt.Sprint(msgs[1]["content"]); strings.Contains(text, "{") {
			json.Unmarshal([]byte(text[strings.IndexByte(text, '{'):]), &opened)
		}
		mu.Lock()
		claimed[float64(opened.Claimed)] = true
		mu.Unlock()
		return 200, chatText("Nothing more to do.")
	}
	grace := must(s.CreateUser("grace@example.com", "Grace Hopper", "grace's own password"))
	if err := s.AddMember(o.ID, grace.ID, "member", grace.Name, "Owner One"); err != nil {
		t.Fatal(err)
	}
	member := newMailClient()
	signIn(t, member, srv, grace.Email, "grace's own password")
	if code, body := start(member, agents[0].ID); code != http.StatusCreated || !strings.Contains(body, `"agent_name":"Claude"`) {
		t.Fatalf("a member starting an agent: %d %s", code, body)
	}
	if code, body := start(ada, agents[1].ID); code != http.StatusCreated {
		t.Fatalf("a second agent on the same task: %d %s", code, body)
	}
	if code, body := start(ada, agents[0].ID); code != http.StatusConflict || !strings.Contains(body, "Claude is already working") {
		t.Fatalf("the same agent twice on one task: %d %s", code, body)
	}
	if strings.Join(going(), " ") != "Claude Gateway" {
		t.Fatalf("both runs should be going, so neither is offered again: %v", going())
	}
	close(release)
	var runs []store.AgentRun
	waitFor(t, func() bool {
		s.DB.Where("task_id = ?", task.ID).Find(&runs)
		return len(runs) == 2 && !runs[0].Going() && !runs[1].Going()
	})
	if runs[0].RequestID == 0 || runs[0].RequestID == runs[1].RequestID {
		t.Fatalf("two agents on one task should take different requests: %d and %d", runs[0].RequestID, runs[1].RequestID)
	}
	var keys []int64
	s.DB.Model(&store.AgentRun{}).Where("task_id = ?", task.ID).Distinct().Pluck("agent_id", &keys)
	if len(keys) != 2 {
		t.Fatalf("each run should be its agent's: %v", keys)
	}
}

// waitFor polls a condition that background runs make true.
func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if done() {
			return
		}
	}
	t.Fatal("timed out waiting")
}

func TestAToolNobodyTickedIsntRun(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	fake.chat = func(n int, _ map[string]any) (int, string) {
		if n == 0 {
			return 200, chatTools("fetch_fetch", `{"query":"https://evil.example/x"}`, "draft_message", `{"contact_id":1,"kind":"email","text":"hi"}`)
		}
		return 200, chatTools("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, used := range fake.toolUse {
		if used == "fetch_fetch" {
			t.Fatal("a tool on the server that nobody ticked was run because the model asked for it")
		}
	}
	told := 0
	for _, m := range messagesOf(fake.bodies[1]) {
		if m["role"] == "tool" && strings.Contains(fmt.Sprint(m["content"]), "there is no tool called") {
			told++
		}
	}
	if told != 2 {
		t.Fatalf("the model should be told neither tool exists, got %d", told)
	}
}

func TestOneServerDoesntTakeOverARunItIsDriving(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	var once sync.Once
	fake.chat = func(n int, _ map[string]any) (int, string) {
		if n == 0 {
			// A turn long enough for its lease to lapse, as the sweep sees it.
			once.Do(func() { r.a.resumeLapsed(time.Now().Add(4 * time.Minute)) })
		}
		if n < 3 {
			return 200, chatTools(searchTool, `{"query":"yachts"}`)
		}
		return 200, chatTools("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	got := r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
	openings := 0
	for _, b := range fake.bodies {
		if len(messagesOf(b)) == 2 {
			openings++
		}
	}
	if openings != 1 || got.Resumes != 0 || got.Status != RunFinished || got.Turns != 4 {
		t.Fatalf("one run should be driven once: %d openings, %+v", openings, got)
	}
}

func TestOnlyOneRunStartsOnATask(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	release := make(chan struct{})
	fake.chat = func(int, map[string]any) (int, string) {
		<-release
		return 200, chatText("done")
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	started := 0
	for range 8 {
		wg.Go(func() {
			if _, err := r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace"); err == nil {
				mu.Lock()
				started++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	close(release)
	if started != 1 {
		t.Fatalf("%d runs started on one task", started)
	}
	bad := store.AgentRun{OrgID: r.o.ID, TaskID: r.task.ID, AgentID: r.agent, Status: store.RunWaiting, StartedAt: time.Now()}
	if err := r.s.DB.Create(&bad).Error; err == nil {
		t.Fatal("the database let a second going run onto the task")
	}
}

func TestARunCarriedOnKeepsItsOwnClaim(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	fake.chat = func(int, map[string]any) (int, string) {
		return 200, chatTools("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	bk := must(r.s.AgentKey(r.o.ID, must(r.s.AgentByID(r.o.ID, r.agent))))
	now := time.Now()
	// The claim its run took before the server stopped, under a session that is gone.
	r.s.DB.Create(&store.RequestClaim{RequestID: r.brief, OrgID: r.o.ID, TaskID: r.task.ID, Holder: fmt.Sprintf("key:%d session:gone", bk.ID),
		Actor: "agent:prospector", ClaimedAt: now.Add(-5 * time.Minute).UnixMilli(), ExpiresAt: now.Add(10 * time.Minute).UnixMilli()})
	left := store.AgentRun{OrgID: r.o.ID, TaskID: r.task.ID, AgentID: r.agent, StartedBy: "Ada Lovelace", Model: "router/auto", Status: store.RunWorking,
		Owner: "a server that stopped", LeaseUntil: now.Add(-time.Second).UnixMilli(), StartedAt: now}
	r.s.DB.Create(&left)
	r.a.resumeLapsed(now)
	if got := r.finished(t, left.ID); got.Status != RunFinished || got.StopReason != "the request is done" {
		t.Fatalf("a run carried on should pick up its own request again: %+v", got)
	}
}

func TestTheSourceGuardHoldsForEveryWayToCiteAPage(t *testing.T) {
	t.Parallel()
	ts := &turnState{seen: map[string]bool{}}
	ts.see(`See https://en.wikipedia.org/wiki/Acme_(company). And (https://larkmoor.example/team) and {"u":"https://x.example/a?b=1&c=2"}`)
	for _, u := range []string{"https://en.wikipedia.org/wiki/Acme_(company)", "https://larkmoor.example/team", "https://x.example/a?b=1&c=2"} {
		if !ts.seen[research.NormalURL(u)] {
			t.Errorf("a page shown wasn't recognised: %s (seen %v)", u, ts.seen)
		}
	}

	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	fake.chat = func(n int, _ map[string]any) (int, string) {
		switch n {
		case 0:
			// A URL typed into a search doesn't become a page the agent was shown.
			return 200, chatTools(searchTool, `{"query":"https://made-up.example/about"}`)
		case 1:
			return 200, chatTools(
				"add_company", `{"name":"Corvane","configurator_url":"https://made-up.example/build","platform":"Shopify"}`,
				"add_company", `{"name":"Halvard","facts":[{"field":"size","value":"40 staff","source_url":"https://made-up.example/about"}]}`)
		}
		return 200, chatTools("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
	for _, name := range []string{"Corvane", "Halvard"} {
		if _, err := r.s.CompanyRef(r.o.ID, name); err == nil {
			t.Errorf("%s was added with a source the agent was never shown", name)
		}
	}
}

func TestARunStartingLeavesOtherKeysClaimsAlone(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	bk := must(r.s.AgentKey(r.o.ID, must(r.s.AgentByID(r.o.ID, r.agent))))
	other := fmt.Sprintf("key:%d0 session:abc", bk.ID)
	now := time.Now()
	r.s.DB.Create(&store.RequestClaim{RequestID: r.brief, OrgID: r.o.ID, TaskID: r.task.ID, Holder: other, Actor: "agent:claude",
		ClaimedAt: now.UnixMilli(), ExpiresAt: now.Add(10 * time.Minute).UnixMilli()})
	fake.chat = func(int, map[string]any) (int, string) { return 200, chatText("unused") }
	got := r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
	if got.StopReason != "other agents hold every open request" {
		t.Fatalf("another agent's claim, on a key whose id starts like the built-in one's, was dropped: %+v", got)
	}
	var c store.RequestClaim
	if r.s.DB.First(&c, "request_id = ?", r.brief); c.Holder != other {
		t.Fatalf("the other agent's claim: %+v", c)
	}
}

func TestTheAgentCantCiteAPageItWasntShownAnyOtherWay(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	fake.chat = func(n int, _ map[string]any) (int, string) {
		if n == 0 {
			return 200, chatTools("add_company", `{"name":"Ghost Yachts","platform":"Made-up Platform","built_by":"Invented Agency"}`)
		}
		return 200, chatTools("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
	if _, err := r.s.CompanyRef(r.o.ID, "Ghost Yachts"); err == nil {
		t.Fatal("a company was added with a platform and builder sourced from nowhere")
	}
}

func TestNextToNoRetryAfterBacksOffAsAnOutageDoes(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	fake.chat = func(n int, _ map[string]any) (int, string) {
		if n < 2 {
			return http.StatusTooManyRequests, `{"error":{"code":429,"message":"slow down"}}`
		}
		return 200, chatTools("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	fake.retryAfter = "0"
	r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
	if len(r.sleeps) != 2 || r.sleeps[0].Round(time.Second) != 10*time.Second || r.sleeps[1].Round(time.Second) != 20*time.Second {
		t.Fatalf("a Retry-After of nothing should back off as an outage does: %v", r.sleeps)
	}
}

func TestRevokingTheAgentsKeyStopsItsRun(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	fake.chat = func(n int, _ map[string]any) (int, string) {
		if n == 1 {
			bk := must(r.s.AgentKey(r.o.ID, must(r.s.AgentByID(r.o.ID, r.agent))))
			r.s.DB.Model(&store.APIKey{}).Where("id = ?", bk.ID).Update("revoked_at", time.Now())
		}
		return 200, chatTools(searchTool, `{"query":"yachts"}`)
	}
	got := r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
	if got.Status != agents.RunStopped || !strings.Contains(got.StopReason, "revoked") {
		t.Fatalf("a run whose key was revoked: %+v", got)
	}
}
