package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/store"
)

const agentTestKey = "sk-test-0123456789abcdef"

// fakeEndpoint is an OpenAI-compatible model endpoint and an MCP tool server on one
// TLS address, recording the credentials each is sent.
type fakeEndpoint struct {
	srv       *httptest.Server
	mu        sync.Mutex
	modelAuth []string
	toolAuth  []string
	limited   bool
	// chat answers the nth chat completion (from 0), given the request's body; a 429
	// carries retryAfter, an hour unless set.
	chat       func(n int, body map[string]any) (int, string)
	retryAfter string
	chats      int
	bodies     []map[string]any
	toolUse    []string
}

func newFakeEndpoint(t *testing.T) *fakeEndpoint {
	t.Helper()
	f := &fakeEndpoint{}
	tools := mcp.NewServer(&mcp.Implementation{Name: "fake-tools", Version: "1"}, nil)
	handle := func(_ context.Context, r *mcp.CallToolRequest, _ struct {
		Query string `json:"query"`
	}) (*mcp.CallToolResult, struct{}, error) {
		f.mu.Lock()
		f.toolUse = append(f.toolUse, r.Params.Name)
		f.mu.Unlock()
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{
			Text: "1. Larkmoor Yachts: day cruisers, with a configurator. https://larkmoor.example/build"}}}, struct{}{}, nil
	}
	mcp.AddTool(tools, &mcp.Tool{Name: "brave-search_brave_web_search", Description: strings.Repeat("Search the web. ", 60)}, handle)
	mcp.AddTool(tools, &mcp.Tool{Name: "fetch_fetch", Description: "Read a page."}, handle)
	toolsHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return tools }, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.modelAuth = append(f.modelAuth, r.Header.Get("Authorization"))
		limited := f.limited
		f.mu.Unlock()
		if limited {
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"code":"429","message":"exceeded requests-per-day limit (3000)","type":"rate_limit_error"}}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+agentTestKey {
			http.Error(w, `{"error":{"code":401,"message":"bad key"}}`, http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"object":"list","data":[{"id":"router/auto"},{"id":"qwen/qwen-small"},{"id":"router/auto"}]}`))
	})
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		n := f.chats
		f.chats++
		f.bodies = append(f.bodies, body)
		chat := f.chat
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+agentTestKey || chat == nil {
			http.Error(w, `{"error":{"code":401,"message":"bad key"}}`, http.StatusUnauthorized)
			return
		}
		status, reply := chat(n, body)
		if status == http.StatusTooManyRequests {
			after := "3600"
			if f.retryAfter != "" {
				after = f.retryAfter
			}
			w.Header().Set("Retry-After", after)
		}
		w.WriteHeader(status)
		w.Write([]byte(reply))
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.toolAuth = append(f.toolAuth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		toolsHandler.ServeHTTP(w, r)
	})
	f.srv = httptest.NewTLSServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeEndpoint) auths() (models, tools []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.modelAuth...), append([]string{}, f.toolAuth...)
}

func agentSite(t *testing.T, fake *fakeEndpoint) (*Store, *httptest.Server, *http.Client) {
	t.Helper()
	s, srv, ada, _ := mailSite(t, func(a *App) { a.agentClient = fake.srv.Client() })
	return s, srv, ada
}

func TestAnOwnerSetsUpTheAgent(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	s, srv, ada := agentSite(t, fake)
	o := must(s.OrgBySlug("alpha"))
	// Each save after the first is to the agent it made, as its form posts its id.
	save := func(body map[string]any) (int, string) {
		t.Helper()
		body["name"] = "Prospector"
		if c, err := s.AgentNamed(o.ID, "Prospector"); err == nil {
			body["id"] = c.ID
		}
		return saveAgentAt(t, ada, srv.URL, body)
	}

	code, body := save(map[string]any{"base_url": fake.srv.URL + "/v1/", "api_key": agentTestKey, "tools_url": fake.srv.URL + "/mcp"})
	if code != http.StatusOK || !strings.Contains(body, "Pick a model") {
		t.Fatalf("saving without a model: %d %s", code, body)
	}
	c := must(s.AgentNamed(o.ID, "Prospector"))
	if c.BaseURL != fake.srv.URL+"/v1" || strings.Contains(c.APIKey, agentTestKey) || !store.Sealed(c.APIKey) {
		t.Fatalf("the endpoint or its key wasn't kept as it should be: %+v", c)
	}
	if got := strings.Join(c.ModelList(), " "); got != "qwen/qwen-small router/auto" {
		t.Fatalf("the endpoint's models: %q", got)
	}
	tools := c.ToolList()
	if len(tools) != 2 || tools[0].Name != "brave-search_brave_web_search" || len([]rune(tools[0].Description)) > maxToolText || len(c.AllowedTools()) != 0 {
		t.Fatalf("its tools should be listed, briefly, and none allowed until ticked: %+v %v", tools, c.AllowedTools())
	}
	if models, toolAuth := fake.auths(); models[0] != "Bearer "+agentTestKey || toolAuth[0] != "Bearer "+agentTestKey {
		t.Fatalf("a tool server on the endpoint's own address should be sent its key: %v %v", models, toolAuth)
	}

	// The key, left empty, is kept for the same endpoint.
	code, body = save(map[string]any{"base_url": fake.srv.URL + "/v1", "model": "router/auto", "tools_url": fake.srv.URL + "/mcp", "tools": []string{"fetch_fetch", "made_up"}})
	if code != http.StatusOK || !strings.Contains(body, `"message":"Prospector saved."`) {
		t.Fatalf("saving the model and a tool: %d %s", code, body)
	}
	c = must(s.AgentNamed(o.ID, "Prospector"))
	if !c.Ready() || c.Model != "router/auto" || strings.Join(c.AllowedTools(), " ") != "fetch_fetch" {
		t.Fatalf("only a tool on offer can be allowed: %+v %v", c, c.AllowedTools())
	}
	if key, err := s.Keyring.Open(c.APIKey); err != nil || key != agentTestKey {
		t.Fatalf("the kept key: %v", err)
	}

	_, page := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/agents", "", nil)
	listed := listedAgents(t, ada, srv.URL)
	if len(listed) != 1 || listed[0].Model != "router/auto" || !listed[0].HasKey || strings.Join(listed[0].AllowedTools, " ") != "fetch_fetch" ||
		!slices.ContainsFunc(listed[0].Tools, func(tl api.Tool) bool { return tl.Name == "brave-search_brave_web_search" }) {
		t.Errorf("the agent as the Agents page reads it: %+v", listed)
	}
	if strings.Contains(page, agentTestKey) || strings.Contains(page, c.APIKey) {
		t.Fatal("the agents' list shows the key")
	}

	// A key isn't sent to an address it wasn't given for.
	if code, body := save(map[string]any{"base_url": "https://elsewhere.example/v1", "model": "router/auto"}); code != http.StatusBadRequest || !strings.Contains(body, "API key") {
		t.Fatalf("a new endpoint should need its key entered: %d %s", code, body)
	}
	if code, body := save(map[string]any{"base_url": "http://plain.example/v1", "api_key": agentTestKey}); code != http.StatusBadRequest || !strings.Contains(body, "https://") {
		t.Fatalf("an endpoint not on https: %d %s", code, body)
	}
	if code, _ := save(map[string]any{"base_url": "https://user:pass@llm.example/v1", "api_key": agentTestKey}); code != http.StatusBadRequest {
		t.Fatal("an endpoint carrying a password in its address was taken")
	}

	export, _ := json.Marshal(must(s.ExportOrg(o.ID, "Ada Lovelace")))
	if !strings.Contains(string(export), `"name":"Prospector","kind":"openai"`) || !strings.Contains(string(export), `"model":"router/auto"`) ||
		strings.Contains(string(export), agentTestKey) || strings.Contains(string(export), c.APIKey) {
		t.Fatalf("the export should carry the set-up but not the key: %s", export)
	}
	if code, body := save(map[string]any{"base_url": ""}); code != http.StatusBadRequest || !strings.Contains(body, "endpoint") {
		t.Fatalf("an agent with no endpoint: %d %s", code, body)
	}
}

func TestAnOrganisationHasSeveralAgents(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	s, srv, ada := agentSite(t, fake)
	o := must(s.OrgBySlug("alpha"))
	add := func(body map[string]any) (int, string) {
		t.Helper()
		body["base_url"], body["api_key"] = fake.srv.URL+"/v1", agentTestKey
		return saveAgentAt(t, ada, srv.URL, body)
	}
	for _, name := range []string{"Claude", "ChatGPT", "Gateway"} {
		if code, body := add(map[string]any{"name": name, "model": "router/auto"}); code != http.StatusOK {
			t.Fatalf("adding %s: %d %s", name, code, body)
		}
	}
	if code, body := add(map[string]any{"name": " claude ", "model": "router/auto"}); code != http.StatusOK {
		t.Fatalf("names are told apart as typed, trimmed: %d %s", code, body)
	}
	if code, body := add(map[string]any{"name": "Claude", "model": "router/auto"}); code != http.StatusConflict || !strings.Contains(body, "already") {
		t.Fatalf("a second agent with a name already taken: %d %s", code, body)
	}
	if code, _ := add(map[string]any{"name": "", "model": "router/auto"}); code != http.StatusBadRequest {
		t.Fatal("an agent with no name was taken")
	}
	agents := must(s.Agents(o.ID))
	if len(agents) != 4 || agents[0].Name != "ChatGPT" {
		t.Fatalf("the agents, by name: %+v", agents)
	}
	listed := listedAgents(t, ada, srv.URL)
	if len(listed) != len(agents) {
		t.Fatalf("the agents' list: %+v", listed)
	}
	for i, a := range agents {
		if listed[i].Id != a.ID || listed[i].Name != a.Name || listed[i].Models == nil {
			t.Fatalf("each agent comes with its own model list: %+v", listed[i])
		}
	}

	// Renaming carries the new name into its key, which is what its work is recorded under.
	claude := must(s.AgentNamed(o.ID, "Claude"))
	key := must(s.AgentKey(o.ID, claude))
	if code, body := add(map[string]any{"id": claude.ID, "name": "Claude Opus", "model": "router/auto"}); code != http.StatusOK {
		t.Fatalf("renaming: %d %s", code, body)
	}
	var renamed store.APIKey
	s.DB.First(&renamed, key.ID)
	if renamed.Name != "Claude Opus (built in)" {
		t.Fatalf("its key's name: %q", renamed.Name)
	}

	// Another organisation's agent can be neither saved over nor deleted.
	beta := mustOrg(t, s, "beta")
	theirs := must((&App{s: s, agentClient: fake.srv.Client()}).saveAgent(context.Background(), beta.ID,
		AgentSettingsIn{Name: "Theirs", BaseURL: fake.srv.URL + "/v1", APIKey: agentTestKey, Model: "router/auto"}, "Grace Hopper"))
	if code, body := add(map[string]any{"id": theirs.ID, "name": "Mine now", "model": "router/auto"}); code != http.StatusNotFound {
		t.Fatalf("an owner saved over another organisation's agent: %d %s", code, body)
	}
	if code, _ := call(t, ada, "DELETE", fmt.Sprintf("%s/o/alpha/api/v1/agents/%d", srv.URL, theirs.ID), "", nil); code != http.StatusNotFound {
		t.Fatal("an owner deleted another organisation's agent")
	}
	if must(s.AgentNamed(beta.ID, "Theirs")).Name != "Theirs" {
		t.Fatal("the other organisation's agent changed")
	}

	// Deleting one revokes its key and forgets it.
	if code, body := call(t, ada, "DELETE", fmt.Sprintf("%s/o/alpha/api/v1/agents/%d", srv.URL, claude.ID), "", nil); code != http.StatusNoContent {
		t.Fatalf("deleting: %d %s", code, body)
	}
	if _, err := s.AgentByID(o.ID, claude.ID); err == nil {
		t.Fatal("a deleted agent is still there")
	}
	s.DB.First(&renamed, key.ID)
	if renamed.RevokedAt == nil {
		t.Fatal("a deleted agent's key wasn't revoked")
	}
	export, _ := json.Marshal(must(s.ExportOrg(o.ID, "Ada Lovelace")))
	if strings.Count(string(export), `"kind":"openai"`) != 3 || strings.Contains(string(export), "Theirs") {
		t.Fatalf("the export should carry this organisation's three agents: %s", export)
	}
}

func TestAToolServerElsewhereIsntSentTheKey(t *testing.T) {
	t.Parallel()
	fake, other := newFakeEndpoint(t), newFakeEndpoint(t)
	s, srv, ada := agentSite(t, fake)
	pool := fake.srv.Client()
	pool.Transport.(*http.Transport).TLSClientConfig.RootCAs.AddCert(other.srv.Certificate())
	code, body := saveAgentAt(t, ada, srv.URL,
		map[string]any{"name": "Prospector", "base_url": fake.srv.URL + "/v1", "api_key": agentTestKey, "model": "router/auto", "tools_url": other.srv.URL + "/mcp"})
	if code != http.StatusOK {
		t.Fatalf("saving: %d %s", code, body)
	}
	if _, toolAuth := other.auths(); len(toolAuth) == 0 || slicesContainsPrefix(toolAuth, "Bearer") {
		t.Fatalf("a tool server on another address was sent the endpoint's key: %v", toolAuth)
	}
	if len(must(s.AgentNamed(must(s.OrgBySlug("alpha")).ID, "Prospector")).ToolList()) != 2 {
		t.Fatal("its tools should still be listed")
	}
}

func slicesContainsPrefix(list []string, prefix string) bool {
	for _, v := range list {
		if strings.HasPrefix(v, prefix) {
			return true
		}
	}
	return false
}

func TestAnEndpointAtItsLimitStillSaves(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	fake.limited = true
	s, srv, ada := agentSite(t, fake)
	code, body := saveAgentAt(t, ada, srv.URL,
		map[string]any{"name": "Prospector", "base_url": fake.srv.URL + "/v1", "api_key": agentTestKey, "model": "router/auto"})
	if code != http.StatusOK || !strings.Contains(body, "answered 429") || !strings.Contains(body, "until") {
		t.Fatalf("a check refused for its limit should be reported, and the settings kept: %d %s", code, body)
	}
	if c := must(s.AgentNamed(must(s.OrgBySlug("alpha")).ID, "Prospector")); !c.Ready() {
		t.Fatalf("the settings weren't kept: %+v", c)
	}
	if listed := listedAgents(t, ada, srv.URL); len(listed) != 1 || !strings.Contains(listed[0].State.Todo, "Last check: Models: the endpoint answered 429") {
		t.Fatalf("the agent doesn't say its last check was refused: %+v", listed)
	}
	if models, _ := fake.auths(); len(models) != 1 {
		t.Fatalf("listing the agents called the endpoint: %d calls", len(models))
	}
}

func TestOnlyOwnersSetUpTheAgentAndOnlyWithASecretKey(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	s, srv, _ := agentSite(t, fake)
	o := must(s.OrgBySlug("alpha"))
	grace := must(s.CreateUser("grace@example.com", "Grace Hopper", "grace's own password"))
	if err := s.AddMember(o.ID, grace.ID, "member", grace.Name); err != nil {
		t.Fatal(err)
	}
	member := newMailClient()
	signIn(t, member, srv, grace.Email, "grace's own password")
	body := map[string]any{"name": "Prospector", "base_url": fake.srv.URL + "/v1", "api_key": agentTestKey, "model": "router/auto"}
	if code, _ := saveAgentAt(t, member, srv.URL, body); code != http.StatusForbidden {
		t.Fatalf("a member saving the agent: %d", code)
	}

	a := &App{s: s, agentClient: fake.srv.Client()}
	s.Keyring = nil
	if _, err := a.saveAgent(context.Background(), o.ID, AgentSettingsIn{Name: "Prospector", BaseURL: fake.srv.URL + "/v1", APIKey: agentTestKey}, "ada"); err == nil || !strings.Contains(err.Error(), "PROSPECTOR_SECRET_KEYS") {
		t.Fatalf("a server with no secret key took an API key: %v", err)
	}
}

func TestTheAgentsClientRefusesThisMachine(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	_, err := (&App{}).listModels(context.Background(), fake.srv.URL+"/v1", agentTestKey)
	if err == nil || !strings.Contains(err.Error(), "isn't a public address") {
		t.Fatalf("the agent's client reached a loopback address: %v", err)
	}
	if _, err := (&App{}).listTools(context.Background(), fake.srv.URL+"/mcp", ""); err == nil {
		t.Fatal("the agent's client reached a loopback tool server")
	}
}

func TestAnAgentKeyMovesToANewSecretKey(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	s, _, _ := agentSite(t, fake)
	o := must(s.OrgBySlug("alpha"))
	a := &App{s: s, agentClient: fake.srv.Client()}
	if _, err := a.saveAgent(context.Background(), o.ID, AgentSettingsIn{Name: "Prospector", BaseURL: fake.srv.URL + "/v1", APIKey: agentTestKey, Model: "router/auto"}, "ada"); err != nil {
		t.Fatal(err)
	}
	s.Keyring = mustKeys(t, "2:"+strings.Repeat("cd", 32)+",1:"+mailTestKey)
	if moved, err := s.Reseal(); err != nil || moved != 1 {
		t.Fatalf("resealing: %d %v", moved, err)
	}
	c := must(s.AgentNamed(o.ID, "Prospector"))
	if key, err := s.Keyring.Open(c.APIKey); err != nil || key != agentTestKey || store.SealedVersion(c.APIKey) != 2 {
		t.Fatalf("the agent's key didn't move to the new secret key: %v", err)
	}
}

func TestAnAgentDeletedWhileSavingStaysDeleted(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	c := must(r.s.AgentByID(r.o.ID, r.agent))
	key := must(r.s.AgentKey(r.o.ID, c))
	if err := r.a.deleteAgent(r.o.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	c.Model = "something else"
	if err := r.s.SaveAgentRow(r.o.ID, c); err == nil || !strings.Contains(err.Error(), "deleted meanwhile") {
		t.Fatalf("saving a deleted agent: %v", err)
	}
	if _, err := r.s.AgentByID(r.o.ID, c.ID); err == nil {
		t.Fatal("saving brought a deleted agent back")
	}
	// A key made by a first run while its settings were being checked isn't written over.
	fresh := must(r.a.saveAgent(context.Background(), r.o.ID, AgentSettingsIn{Name: "Gateway", BaseURL: fake.srv.URL + "/v1", APIKey: agentTestKey, Model: "router/auto"}, "Ada Lovelace"))
	stale := *fresh
	made := must(r.s.AgentKey(r.o.ID, fresh))
	stale.Model = "qwen/qwen-small"
	if err := r.s.SaveAgentRow(r.o.ID, &stale); err != nil {
		t.Fatal(err)
	}
	if got := must(r.s.AgentByID(r.o.ID, fresh.ID)); got.KeyID != made.ID || got.Model != "qwen/qwen-small" || key.ID == made.ID {
		t.Fatalf("an owner's save wrote over the key its run made: %+v", got)
	}
}

func TestEveryGoingRunIsOnTheTaskPage(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	now := time.Now()
	old := store.AgentRun{OrgID: r.o.ID, TaskID: r.task.ID, AgentID: r.agent, Status: store.RunWaiting, StartedAt: now.Add(-48 * time.Hour)}
	r.s.DB.Create(&old)
	for i := range 12 {
		r.s.DB.Create(&store.AgentRun{OrgID: r.o.ID, TaskID: r.task.ID, AgentID: r.agent, Status: RunFinished, StartedAt: now.Add(time.Duration(i) * time.Minute)})
	}
	runs := must(r.s.AgentRuns(r.o.ID, r.task.ID, 10))
	if len(runs) != 11 || runs[0].ID != old.ID {
		t.Fatalf("a run going since before ten newer ones should still be listed, so it can be stopped: %d runs, first %d", len(runs), runs[0].ID)
	}
}

func TestRefusalsReadBothErrorShapes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, body := range []string{`{"error":{"code":"429","message":"slow down"}}`, `{"error":{"code":429,"message":"slow down"}}`} {
		rec := httptest.NewRecorder()
		rec.Header().Set("Retry-After", "120")
		rec.WriteHeader(http.StatusTooManyRequests)
		rec.WriteString(body)
		var e *endpointError
		if err := refusal(rec.Result(), now); !errors.As(err, &e) || e.Message != "slow down" || !e.RetryAfter.Equal(now.Add(2*time.Minute)) {
			t.Fatalf("%s: %+v", body, e)
		}
	}
}

// saveAgentAt saves an agent through the API as a browser's form does: a new one
// posted, one with an id put to its own address. A save answers 200 either way.
// listedAgents are the organisation's agents as the Agents page reads them.
func listedAgents(t *testing.T, c *http.Client, base string) []api.Agent {
	t.Helper()
	code, body := call(t, c, "GET", base+"/o/alpha/api/v1/agents?limit=200", "", nil)
	var out struct{ Items []api.Agent }
	if code != http.StatusOK || json.Unmarshal([]byte(body), &out) != nil {
		t.Fatalf("listing the agents: %d %s", code, body)
	}
	return out.Items
}

func saveAgentAt(t *testing.T, c *http.Client, base string, body map[string]any) (int, string) {
	t.Helper()
	method, path := "POST", base+"/o/alpha/api/v1/agents"
	if id, ok := body["id"].(int64); ok && id != 0 {
		method, path = "PUT", fmt.Sprintf("%s/%d", path, id)
	}
	delete(body, "id")
	code, out := call(t, c, method, path, "application/json", body)
	if code == http.StatusCreated {
		code = http.StatusOK
	}
	return code, out
}
