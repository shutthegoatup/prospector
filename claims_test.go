package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/research"
)

func TestTwoAgentsOnATaskTakeDifferentRequests(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout, second := Actor{Name: "agent:scout", KeyID: 1}, Actor{Name: "agent:second", KeyID: 2}
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders with a configurator.", "Ada Lovelace"))
	now := time.Now()

	first := must(s.AgentTaskAt(o.ID, scout, task.ID, now))
	brief := first.Requests[0].ID
	if first.Claimed != brief || !first.Requests[0].Yours || first.Status != research.TaskWorking {
		t.Fatalf("the first agent should be kept the brief: %+v", first)
	}
	got := must(s.AgentTaskAt(o.ID, second, task.ID, now.Add(time.Minute)))
	if got.Claimed != 0 || got.Requests[0].HeldBy != "agent:scout" || got.Requests[0].Yours || got.Requests[0].Until == nil {
		t.Fatalf("the second agent should see the brief is the first's: %+v", got)
	}
	if list := must(firstOf(s.AgentTasks(o.ID, second, paging.Page{}))); list[0].Status != research.TaskWorking || list[0].Open[0].HeldBy != "agent:scout" {
		t.Fatalf("list_tasks should say who holds the brief: %+v", list[0])
	}

	if _, err := s.AskTask(o.ID, task.ID, "Find ten more in Italy.", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	got = must(s.AgentTaskAt(o.ID, second, task.ID, now.Add(2*time.Minute)))
	more := got.Requests[1].ID
	if got.Claimed != more || !got.Requests[1].Yours || got.Requests[0].HeldBy != "agent:scout" {
		t.Fatalf("the second agent should take the new request: %+v", got)
	}
	if again := must(s.AgentTaskAt(o.ID, scout, task.ID, now.Add(3*time.Minute))); again.Claimed != brief {
		t.Fatalf("the first agent reading the task again should keep what it had, got %d", again.Claimed)
	}

	// A key under a name another key had is a different agent.
	renamed := Actor{Name: "agent:scout", KeyID: 9}
	if got := must(s.AgentTaskAt(o.ID, renamed, task.ID, now.Add(4*time.Minute))); got.Claimed != 0 || got.Requests[0].Yours {
		t.Fatalf("a new key took an old key's claim because they share a name: %+v", got)
	}

	// A key that can only read takes nothing.
	reader := Actor{Name: "agent:reader", KeyID: 3, ReadOnly: true}
	if err := s.SetRequestDone(o.ID, more, true, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AskTask(o.ID, task.ID, "And Spain.", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if got := must(s.AgentTaskAt(o.ID, reader, task.ID, now.Add(5*time.Minute))); got.Claimed != 0 || got.Requests[2].HeldBy != "" {
		t.Fatalf("a read-only key was kept a request: %+v", got.Requests[2])
	}
	var claims []RequestClaim
	s.DB.Where("org_id = ?", o.ID).Order("request_id").Find(&claims)
	if len(claims) != 1 || claims[0].RequestID != brief {
		t.Fatalf("finishing a request should end its claim, and reading take none: %+v", claims)
	}
}

func TestAClaimLapsesUnlessTheHolderKeepsAdding(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout, second := Actor{Name: "agent:scout", KeyID: 1}, Actor{Name: "agent:second", KeyID: 2}
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders.", "Ada Lovelace"))
	start := time.Now()
	brief := must(s.AgentTaskAt(o.ID, scout, task.ID, start)).Claimed

	expires := func() time.Time {
		var c RequestClaim
		if err := s.DB.First(&c, "request_id = ?", brief).Error; err != nil {
			t.Fatal(err)
		}
		return time.UnixMilli(c.ExpiresAt)
	}
	// Ten minutes in, the holder adds a company for the task, which keeps the claim.
	if err := s.DB.Model(&RequestClaim{}).Where("request_id = ?", brief).Update("expires_at", time.Now().Add(5*time.Minute).UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Task: task.ID, Name: "Larkmoor Yachts"}))
	if left := time.Until(expires()); left < research.ClaimFor-time.Minute {
		t.Fatalf("a write for the task should renew the claim, %s left", left)
	}
	// Another agent's write for the task leaves the claim where it was.
	before := expires()
	must(s.AgentAddCompany(o.ID, second, research.AddCompanyIn{Task: task.ID, Name: "Corvane"}))
	if !expires().Equal(before) {
		t.Fatal("another agent's write moved the holder's claim")
	}

	// Once it has lapsed the request is free for whoever picks the task up next.
	later := time.Now().Add(research.ClaimFor + time.Minute)
	got := must(s.AgentTaskAt(o.ID, second, task.ID, later))
	if got.Claimed != brief || !got.Requests[0].Yours {
		t.Fatalf("a lapsed claim should pass to the next agent: %+v", got)
	}
	if mine := must(s.AgentTaskAt(o.ID, scout, task.ID, later)); mine.Claimed != 0 || mine.Requests[0].HeldBy != "agent:second" {
		t.Fatalf("the agent that let it lapse should see it taken: %+v", mine)
	}
	if v := must(s.Task(o.ID, task.ID, later.Add(research.ClaimFor+research.RunGap))); v.Status != research.TaskWaiting || v.Requests[0].HeldBy != "" {
		t.Fatalf("a task nobody has touched since its claim lapsed should be waiting: %s %+v", v.Status, v.Requests[0])
	}
}

func TestOnlyOneOfManyAgentsWinsARequest(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders.", "Ada Lovelace"))
	request := must(s.Task(o.ID, task.ID, time.Now())).Requests[0].ID
	now := time.Now()
	const agents = 8
	won := make([]bool, agents)
	errs := make([]error, agents)
	var wg sync.WaitGroup
	for i := range agents {
		wg.Add(1)
		go func() {
			defer wg.Done()
			won[i], errs[i] = s.Claim(o.ID, task.ID, request, Actor{Name: "agent:" + strings.Repeat("x", i+1), KeyID: int64(i + 1)}, now)
		}()
	}
	wg.Wait()
	winners := 0
	for i := range agents {
		if errs[i] != nil {
			t.Fatalf("agent %d: %v", i, errs[i])
		}
		if won[i] {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("%d agents claimed the same request at once", winners)
	}
	var n int64
	if s.DB.Model(&RequestClaim{}).Count(&n); n != 1 {
		t.Fatalf("%d claim rows", n)
	}
}

func mcpSession(t *testing.T, site, secret string) *mcp.ClientSession {
	t.Helper()
	transport := &mcp.StreamableClientTransport{Endpoint: site + "/o/alpha/mcp", HTTPClient: &http.Client{
		Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			r.Header.Set("Authorization", "Bearer "+secret)
			return http.DefaultTransport.RoundTrip(r)
		})}}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func TestTwoClientsOnOneKeyTakeDifferentRequests(t *testing.T) {
	t.Parallel()
	s, o, srv, _ := signedIn(t)
	key, _, _ := s.CreateKey(o.ID, "claude", []string{"read", "research:write"}, "test")
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders.", "Ada Lovelace"))
	if _, err := s.AskTask(o.ID, task.ID, "And Italy.", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	claimed := func(session *mcp.ClientSession) int64 {
		t.Helper()
		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_task", Arguments: map[string]any{"id": task.ID}})
		if err != nil || res.IsError {
			t.Fatalf("get_task: %v %+v", err, res)
		}
		var out research.TaskOut
		raw, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out.Claimed
	}
	one, two := mcpSession(t, srv.URL, key), mcpSession(t, srv.URL, key)
	first, second := claimed(one), claimed(two)
	if first == 0 || second == 0 || first == second {
		t.Fatalf("two clients on one key should take different requests: %d and %d", first, second)
	}
	if again := claimed(one); again != first {
		t.Fatalf("a client reading the task again should keep its request: %d, then %d", first, again)
	}
}

func TestAClaimIsntTakenOnAFinishedRequestOrShownOnAClosedTask(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout, second := Actor{Name: "agent:scout", KeyID: 1}, Actor{Name: "agent:second", KeyID: 2}
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders.", "Ada Lovelace"))
	now := time.Now()
	stale := must(s.Task(o.ID, task.ID, now))
	request := stale.Requests[0].ID

	// Finished between the agent reading the task and claiming it.
	if err := s.SetRequestDone(o.ID, request, true, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if got := must(s.ClaimNext(o.ID, stale, scout, now)); got != 0 {
		t.Fatalf("a finished request was claimed: %d", got)
	}
	var n int64
	if s.DB.Model(&RequestClaim{}).Count(&n); n != 0 {
		t.Fatalf("%d claims on a finished request", n)
	}

	// The holder's claim lapsed and went to another since the holder read the task.
	if err := s.SetRequestDone(o.ID, request, false, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	must(s.AgentTaskAt(o.ID, scout, task.ID, now))
	held := must(s.Task(o.ID, task.ID, now))
	later := now.Add(research.ClaimFor + time.Minute)
	if won := must(s.Claim(o.ID, task.ID, request, second, later)); !won {
		t.Fatal("a lapsed claim should pass to another agent")
	}
	if got := must(s.ClaimNext(o.ID, held, scout, later)); got != 0 {
		t.Fatalf("an agent was told it kept a request another had taken: %d", got)
	}

	if err := s.SetTaskClosed(o.ID, task.ID, true, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if v := must(s.Task(o.ID, task.ID, later)); v.Requests[0].HeldBy != "" {
		t.Fatalf("a closed task still shows an agent on a request: %+v", v.Requests[0])
	}
}

func TestAnotherOrganisationCantClaimThroughATaskItWasHanded(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o, other := mustOrg(t, s, "alpha"), mustOrg(t, s, "beta")
	scout := Actor{Name: "agent:scout", KeyID: 1}
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders.", "Ada Lovelace"))
	now := time.Now()
	v := must(s.Task(o.ID, task.ID, now))
	if got := must(s.ClaimNext(other.ID, v, scout, now)); got != 0 {
		t.Fatalf("another organisation claimed request %d of this one's task", got)
	}
	var n int64
	if s.DB.Model(&RequestClaim{}).Count(&n); n != 0 {
		t.Fatalf("%d claims made by another organisation", n)
	}
	if got := must(s.ClaimNext(o.ID, v, scout, now)); got != v.Requests[0].ID {
		t.Fatalf("the task's own organisation should claim its request, got %d", got)
	}
}
