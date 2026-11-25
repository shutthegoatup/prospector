package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	fake := newFakeEndpoint(t)
	s, srv, ada := agentSite(t, fake)
	o := must(s.OrgBySlug("alpha"))
	// Each save after the first is to the agent it made, as its form posts its id.
	save := func(body map[string]any) (int, string) {
		t.Helper()
		body["name"] = "Prospector"
		if c, err := s.agentNamed(o.ID, "Prospector"); err == nil {
			body["id"] = c.ID
		}
		return saveAgentAt(t, ada, srv.URL, body)
	}

	code, body := save(map[string]any{"base_url": fake.srv.URL + "/v1/", "api_key": agentTestKey, "tools_url": fake.srv.URL + "/mcp"})
	if code != http.StatusOK || !strings.Contains(body, "Pick a model") {
		t.Fatalf("saving without a model: %d %s", code, body)
	}
	c := must(s.agentNamed(o.ID, "Prospector"))
	if c.BaseURL != fake.srv.URL+"/v1" || strings.Contains(c.APIKey, agentTestKey) || !sealed(c.APIKey) {
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
	c = must(s.agentNamed(o.ID, "Prospector"))
	if !c.Ready() || c.Model != "router/auto" || strings.Join(c.AllowedTools(), " ") != "fetch_fetch" {
		t.Fatalf("only a tool on offer can be allowed: %+v %v", c, c.AllowedTools())
	}
	if key, err := s.keys.open(c.APIKey); err != nil || key != agentTestKey {
		t.Fatalf("the kept key: %v", err)
	}

	_, page := call(t, ada, "GET", srv.URL+"/o/alpha/settings", "", nil)
	for _, want := range []string{"Prospector&#39;s agents", `value="router/auto"`, "brave-search_brave_web_search", `value="fetch_fetch" data-multi="true" checked`, "saved: leave empty to keep it"} {
		if !strings.Contains(page, want) {
			t.Errorf("Settings is missing %q", want)
		}
	}
	if strings.Contains(page, agentTestKey) || strings.Contains(page, c.APIKey) {
		t.Fatal("Settings shows the key")
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
	_, page := call(t, ada, "GET", srv.URL+"/o/alpha/settings", "", nil)
	for _, a := range agents {
		if !strings.Contains(page, fmt.Sprintf(`list="agent-models-%d"`, a.ID)) || !strings.Contains(page, fmt.Sprintf(`id="agent-models-%d"`, a.ID)) {
			t.Fatalf("each agent's form should have its own model list: %s", a.Name)
		}
	}

	// Renaming carries the new name into its key, which is what its work is recorded under.
	claude := must(s.agentNamed(o.ID, "Claude"))
	key := must(s.agentKey(claude))
	if code, body := add(map[string]any{"id": claude.ID, "name": "Claude Opus", "model": "router/auto"}); code != http.StatusOK {
		t.Fatalf("renaming: %d %s", code, body)
	}
	var renamed APIKey
	s.db.First(&renamed, key.ID)
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
	if must(s.agentNamed(beta.ID, "Theirs")).Name != "Theirs" {
		t.Fatal("the other organisation's agent changed")
	}

	// Deleting one revokes its key and forgets it.
	if code, body := call(t, ada, "DELETE", fmt.Sprintf("%s/o/alpha/api/v1/agents/%d", srv.URL, claude.ID), "", nil); code != http.StatusNoContent {
		t.Fatalf("deleting: %d %s", code, body)
	}
	if _, err := s.AgentByID(o.ID, claude.ID); err == nil {
		t.Fatal("a deleted agent is still there")
	}
	s.db.First(&renamed, key.ID)
	if renamed.RevokedAt == nil {
		t.Fatal("a deleted agent's key wasn't revoked")
	}
	export, _ := json.Marshal(must(s.ExportOrg(o.ID, "Ada Lovelace")))
	if strings.Count(string(export), `"kind":"openai"`) != 3 || strings.Contains(string(export), "Theirs") {
		t.Fatalf("the export should carry this organisation's three agents: %s", export)
	}
}

func TestAToolServerElsewhereIsntSentTheKey(t *testing.T) {
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
	if len(must(s.agentNamed(must(s.OrgBySlug("alpha")).ID, "Prospector")).ToolList()) != 2 {
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
	fake := newFakeEndpoint(t)
	fake.limited = true
	s, srv, ada := agentSite(t, fake)
	code, body := saveAgentAt(t, ada, srv.URL,
		map[string]any{"name": "Prospector", "base_url": fake.srv.URL + "/v1", "api_key": agentTestKey, "model": "router/auto"})
	if code != http.StatusOK || !strings.Contains(body, "answered 429") || !strings.Contains(body, "until") {
		t.Fatalf("a check refused for its limit should be reported, and the settings kept: %d %s", code, body)
	}
	if c := must(s.agentNamed(must(s.OrgBySlug("alpha")).ID, "Prospector")); !c.Ready() {
		t.Fatalf("the settings weren't kept: %+v", c)
	}
	if _, page := call(t, ada, "GET", srv.URL+"/o/alpha/settings", "", nil); !strings.Contains(page, "Last check: Models: the endpoint answered 429") {
		t.Fatal("Settings doesn't say the last check was refused")
	}
	if models, _ := fake.auths(); len(models) != 1 {
		t.Fatalf("rendering Settings called the endpoint: %d calls", len(models))
	}
}

func TestOnlyOwnersSetUpTheAgentAndOnlyWithASecretKey(t *testing.T) {
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
	s.keys = nil
	if _, err := a.saveAgent(context.Background(), o.ID, AgentSettingsIn{Name: "Prospector", BaseURL: fake.srv.URL + "/v1", APIKey: agentTestKey}, "ada"); err == nil || !strings.Contains(err.Error(), "PROSPECTOR_SECRET_KEYS") {
		t.Fatalf("a server with no secret key took an API key: %v", err)
	}
}

func TestTheAgentsClientRefusesThisMachine(t *testing.T) {
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
	fake := newFakeEndpoint(t)
	s, _, _ := agentSite(t, fake)
	o := must(s.OrgBySlug("alpha"))
	a := &App{s: s, agentClient: fake.srv.Client()}
	if _, err := a.saveAgent(context.Background(), o.ID, AgentSettingsIn{Name: "Prospector", BaseURL: fake.srv.URL + "/v1", APIKey: agentTestKey, Model: "router/auto"}, "ada"); err != nil {
		t.Fatal(err)
	}
	s.keys = mustKeys(t, "2:"+strings.Repeat("cd", 32)+",1:"+mailTestKey)
	if moved, err := s.Reseal(); err != nil || moved != 1 {
		t.Fatalf("resealing: %d %v", moved, err)
	}
	c := must(s.agentNamed(o.ID, "Prospector"))
	if key, err := s.keys.open(c.APIKey); err != nil || key != agentTestKey || sealedVersion(c.APIKey) != 2 {
		t.Fatalf("the agent's key didn't move to the new secret key: %v", err)
	}
}

// A database as the release with one agent per organisation left it: its settings in
// agent_configs, its built-in key, and a run it had going when the server stopped.
func TestTheOneAgentFromBeforeCarriesOnAsOneOfSeveral(t *testing.T) {
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	s := r.s
	if err := s.db.Exec("delete from agents").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Exec(`create table agent_configs (org_id integer primary key, base_url text not null default '', api_key text not null default '',
		model text not null default '', tools_url text not null default '', models text not null default '[]', tools text not null default '[]',
		allowed text not null default '[]', problem text not null default '', checked_at timestamp, updated_by text not null default '', updated_at timestamp)`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Exec("drop table if exists agent_configs") })
	sealedKey := must(s.keys.seal(agentTestKey))
	if err := s.db.Exec("insert into agent_configs (org_id, base_url, api_key, model, tools_url, tools, allowed, updated_by, updated_at) values (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		r.o.ID, fake.srv.URL+"/v1", sealedKey, "router/auto", fake.srv.URL+"/mcp", `[{"name":"`+searchTool+`","description":"Search."}]`,
		`["`+searchTool+`"]`, "Ada Lovelace", time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	old := APIKey{OrgID: r.o.ID, Name: "prospector", Prefix: builtinPrefix, Hash: "builtin:old", Scopes: "read research:write", CreatedBy: "prospector"}
	s.db.Create(&old)
	now := time.Now()
	left := AgentRun{OrgID: r.o.ID, TaskID: r.task.ID, StartedBy: "Ada Lovelace", Model: "router/auto", Status: RunWorking,
		Owner: "a server that stopped", LeaseUntil: now.Add(-time.Minute).UnixMilli(), StartedAt: now, Turns: 3}
	s.db.Create(&left)
	s.db.Create(&RequestClaim{RequestID: r.brief, OrgID: r.o.ID, TaskID: r.task.ID, Holder: fmt.Sprintf("key:%d", old.ID), Actor: "agent:prospector",
		ClaimedAt: now.UnixMilli(), ExpiresAt: now.Add(10 * time.Minute).UnixMilli()})

	if err := migrateAgentConfigs(s.db); err != nil {
		t.Fatal(err)
	}
	moved := must(s.agentNamed(r.o.ID, "Prospector"))
	if moved.KeyID != old.ID || moved.APIKey != sealedKey || moved.Model != "router/auto" || !moved.Allows(searchTool) {
		t.Fatalf("the agent should move with its key, settings and record: %+v", moved)
	}
	var rows int64
	if s.db.Table("agent_configs").Count(&rows); rows != 0 {
		t.Fatal("the old row should go with the move, so a deleted agent doesn't come back")
	}
	if err := migrateAgentConfigs(s.db); err != nil || len(must(s.Agents(r.o.ID))) != 1 {
		t.Fatalf("moving twice should change nothing: %v", err)
	}

	fake.chat = func(int, map[string]any) (int, string) {
		return 200, chatTools("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	r.a.resumeLapsed(time.Now())
	got := r.finished(t, left.ID)
	if got.AgentID != moved.ID || got.AgentName != "Prospector" || got.Status != RunFinished || got.Turns != 4 {
		t.Fatalf("the run it had going should carry on, as the moved agent's: %+v", got)
	}
}

func TestGoingBackAReleaseAndForwardAgainStillStarts(t *testing.T) {
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	s := r.s
	other := must(r.a.saveAgent(context.Background(), r.o.ID, AgentSettingsIn{Name: "Claude", BaseURL: fake.srv.URL + "/v1", APIKey: agentTestKey, Model: "router/auto"}, "Ada Lovelace"))
	now := time.Now()
	for _, agent := range []int64{r.agent, other.ID} {
		if err := s.db.Create(&AgentRun{OrgID: r.o.ID, TaskID: r.task.ID, AgentID: agent, Status: RunWaiting, Owner: "elsewhere",
			LeaseUntil: now.Add(time.Hour).UnixMilli(), StartedAt: now}).Error; err != nil {
			t.Fatalf("two agents going on one task: %v", err)
		}
	}
	// What the release before several agents runs at start-up, word for word.
	if err := s.db.Exec("create unique index if not exists idx_agent_runs_one_going on agent_runs (task_id) where status in ('running', 'waiting')").Error; err != nil {
		t.Fatalf("going back a release would fail to start: %v", err)
	}
	if err := s.db.Create(&AgentRun{OrgID: r.o.ID, TaskID: r.task.ID, AgentID: other.ID, Status: RunWorking, StartedAt: now}).Error; err == nil {
		t.Fatal("the database let one agent go twice on one task")
	}

	// A database the earlier release made has its rule; this release replaces it.
	s.db.Exec("delete from agent_runs")
	for _, stmt := range []string{"drop index idx_agent_runs_one_going",
		"create unique index idx_agent_runs_one_going on agent_runs (task_id) where status in ('running', 'waiting')"} {
		if err := s.db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := oneGoingRunPerAgent(s.db); err != nil {
		t.Fatal(err)
	}
	if err := oneGoingRunPerAgent(s.db); err != nil {
		t.Fatalf("a second start: %v", err)
	}
	for _, agent := range []int64{r.agent, other.ID} {
		if err := s.db.Create(&AgentRun{OrgID: r.o.ID, TaskID: r.task.ID, AgentID: agent, Status: RunWorking, StartedAt: now}).Error; err != nil {
			t.Fatalf("after the rule is replaced, two agents should share a task: %v", err)
		}
	}

	// Entered again on the earlier release, then forward again: the newer settings win.
	if err := s.db.Exec(`create table agent_configs (org_id integer primary key, base_url text not null default '', api_key text not null default '',
		model text not null default '', tools_url text not null default '', models text not null default '[]', tools text not null default '[]',
		allowed text not null default '[]', problem text not null default '', checked_at timestamp, updated_by text not null default '', updated_at timestamp)`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Exec("drop table if exists agent_configs") })
	if err := s.db.Exec("insert into agent_configs (org_id, base_url, api_key, model, updated_at) values (?, ?, ?, ?, ?)",
		r.o.ID, fake.srv.URL+"/v1", must(s.keys.seal(agentTestKey)), "qwen/qwen-small", time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateAgentConfigs(s.db); err != nil {
		t.Fatalf("going forward again would fail to start: %v", err)
	}
	if got := must(s.agentNamed(r.o.ID, "Prospector")); got.Model != "qwen/qwen-small" || len(must(s.Agents(r.o.ID))) != 2 {
		t.Fatalf("the settings entered on the earlier release should be the agent's: %+v", got)
	}
}

func TestAMovedAgentNeverTakesAKeyAnotherAgentHolds(t *testing.T) {
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	s := r.s
	// Prospector, moved and given its key, then renamed Claude.
	claude := must(s.AgentByID(r.o.ID, r.agent))
	key := must(s.agentKey(claude))
	if _, err := r.a.saveAgent(context.Background(), r.o.ID, AgentSettingsIn{ID: claude.ID, Name: "Claude", BaseURL: fake.srv.URL + "/v1",
		APIKey: agentTestKey, Model: "router/auto"}, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	// Back a release, the agent entered again there, and forward once more.
	if err := s.db.Exec(`create table agent_configs (org_id integer primary key, base_url text not null default '', api_key text not null default '',
		model text not null default '', tools_url text not null default '', models text not null default '[]', tools text not null default '[]',
		allowed text not null default '[]', problem text not null default '', checked_at timestamp, updated_by text not null default '', updated_at timestamp)`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Exec("drop table if exists agent_configs") })
	if err := s.db.Exec("insert into agent_configs (org_id, base_url, api_key, model, updated_at) values (?, ?, ?, ?, ?)",
		r.o.ID, fake.srv.URL+"/v1", must(s.keys.seal(agentTestKey)), "qwen/qwen-small", time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateAgentConfigs(s.db); err != nil {
		t.Fatal(err)
	}
	moved := must(s.agentNamed(r.o.ID, "Prospector"))
	if moved.KeyID == key.ID {
		t.Fatal("the agent entered again was given the key Claude holds, so their records and claims would be one")
	}
	var claudesKey APIKey
	s.db.First(&claudesKey, key.ID)
	if claudesKey.Name != "Claude (built in)" {
		t.Fatalf("Claude's key was renamed: %q", claudesKey.Name)
	}
	if own := must(s.agentKey(moved)); own.ID == key.ID || own.Name != "Prospector (built in)" {
		t.Fatalf("its first run should make it a key of its own: %+v", own)
	}
}

func TestAnAgentDeletedWhileSavingStaysDeleted(t *testing.T) {
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	c := must(r.s.AgentByID(r.o.ID, r.agent))
	key := must(r.s.agentKey(c))
	if err := r.a.deleteAgent(r.o.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	c.Model = "something else"
	if err := r.s.writeAgent(c); err == nil || !strings.Contains(err.Error(), "deleted meanwhile") {
		t.Fatalf("saving a deleted agent: %v", err)
	}
	if _, err := r.s.AgentByID(r.o.ID, c.ID); err == nil {
		t.Fatal("saving brought a deleted agent back")
	}
	// A key made by a first run while its settings were being checked isn't written over.
	fresh := must(r.a.saveAgent(context.Background(), r.o.ID, AgentSettingsIn{Name: "Gateway", BaseURL: fake.srv.URL + "/v1", APIKey: agentTestKey, Model: "router/auto"}, "Ada Lovelace"))
	stale := *fresh
	made := must(r.s.agentKey(fresh))
	stale.Model = "qwen/qwen-small"
	if err := r.s.writeAgent(&stale); err != nil {
		t.Fatal(err)
	}
	if got := must(r.s.AgentByID(r.o.ID, fresh.ID)); got.KeyID != made.ID || got.Model != "qwen/qwen-small" || key.ID == made.ID {
		t.Fatalf("an owner's save wrote over the key its run made: %+v", got)
	}
}

func TestEveryGoingRunIsOnTheTaskPage(t *testing.T) {
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	now := time.Now()
	old := AgentRun{OrgID: r.o.ID, TaskID: r.task.ID, AgentID: r.agent, Status: RunWaiting, StartedAt: now.Add(-48 * time.Hour)}
	r.s.db.Create(&old)
	for i := range 12 {
		r.s.db.Create(&AgentRun{OrgID: r.o.ID, TaskID: r.task.ID, AgentID: r.agent, Status: RunFinished, StartedAt: now.Add(time.Duration(i) * time.Minute)})
	}
	runs := must(r.s.AgentRuns(r.o.ID, r.task.ID, 10))
	if len(runs) != 11 || runs[0].ID != old.ID {
		t.Fatalf("a run going since before ten newer ones should still be listed, so it can be stopped: %d runs, first %d", len(runs), runs[0].ID)
	}
}

func TestRefusalsReadBothErrorShapes(t *testing.T) {
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
