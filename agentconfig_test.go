package main

import (
	"context"
	"encoding/json"
	"errors"
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
	save := func(body map[string]any) (int, string) {
		t.Helper()
		return call(t, ada, "POST", srv.URL+"/o/alpha/ui/agent", "application/json", body)
	}

	code, body := save(map[string]any{"base_url": fake.srv.URL + "/v1/", "api_key": agentTestKey, "tools_url": fake.srv.URL + "/mcp"})
	if code != http.StatusOK || !strings.Contains(body, "Pick a model") {
		t.Fatalf("saving without a model: %d %s", code, body)
	}
	c := must(s.AgentConfigOf(o.ID))
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
	if code != http.StatusOK || body != `{"message":"Saved."}`+"\n" {
		t.Fatalf("saving the model and a tool: %d %s", code, body)
	}
	c = must(s.AgentConfigOf(o.ID))
	if !c.Ready() || c.Model != "router/auto" || strings.Join(c.AllowedTools(), " ") != "fetch_fetch" {
		t.Fatalf("only a tool on offer can be allowed: %+v %v", c, c.AllowedTools())
	}
	if key, err := s.keys.open(c.APIKey); err != nil || key != agentTestKey {
		t.Fatalf("the kept key: %v", err)
	}

	_, page := call(t, ada, "GET", srv.URL+"/o/alpha/settings", "", nil)
	for _, want := range []string{"Prospector&#39;s agent", `value="router/auto"`, "brave-search_brave_web_search", `value="fetch_fetch" data-multi="true" checked`, "saved: leave empty to keep it"} {
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
	if !strings.Contains(string(export), `"model":"router/auto"`) || strings.Contains(string(export), agentTestKey) || strings.Contains(string(export), c.APIKey) {
		t.Fatalf("the export should carry the set-up but not the key: %s", export)
	}

	if code, body := save(map[string]any{"base_url": ""}); code != http.StatusOK || !strings.Contains(body, "off") {
		t.Fatalf("clearing the endpoint: %d %s", code, body)
	}
	if c := must(s.AgentConfigOf(o.ID)); c.APIKey != "" || c.Ready() {
		t.Fatalf("clearing the endpoint should forget its key: %+v", c)
	}
}

func TestAToolServerElsewhereIsntSentTheKey(t *testing.T) {
	fake, other := newFakeEndpoint(t), newFakeEndpoint(t)
	s, srv, ada := agentSite(t, fake)
	pool := fake.srv.Client()
	pool.Transport.(*http.Transport).TLSClientConfig.RootCAs.AddCert(other.srv.Certificate())
	code, body := call(t, ada, "POST", srv.URL+"/o/alpha/ui/agent", "application/json",
		map[string]any{"base_url": fake.srv.URL + "/v1", "api_key": agentTestKey, "model": "router/auto", "tools_url": other.srv.URL + "/mcp"})
	if code != http.StatusOK {
		t.Fatalf("saving: %d %s", code, body)
	}
	if _, toolAuth := other.auths(); len(toolAuth) == 0 || slicesContainsPrefix(toolAuth, "Bearer") {
		t.Fatalf("a tool server on another address was sent the endpoint's key: %v", toolAuth)
	}
	if len(must(s.AgentConfigOf(must(s.OrgBySlug("alpha")).ID)).ToolList()) != 2 {
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
	code, body := call(t, ada, "POST", srv.URL+"/o/alpha/ui/agent", "application/json",
		map[string]any{"base_url": fake.srv.URL + "/v1", "api_key": agentTestKey, "model": "router/auto"})
	if code != http.StatusOK || !strings.Contains(body, "answered 429") || !strings.Contains(body, "until") {
		t.Fatalf("a check refused for its limit should be reported, and the settings kept: %d %s", code, body)
	}
	if c := must(s.AgentConfigOf(must(s.OrgBySlug("alpha")).ID)); !c.Ready() {
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
	body := map[string]any{"base_url": fake.srv.URL + "/v1", "api_key": agentTestKey, "model": "router/auto"}
	if code, _ := call(t, member, "POST", srv.URL+"/o/alpha/ui/agent", "application/json", body); code != http.StatusForbidden {
		t.Fatalf("a member saving the agent: %d", code)
	}

	a := &App{s: s, agentClient: fake.srv.Client()}
	s.keys = nil
	if _, err := a.saveAgentConfig(context.Background(), o.ID, AgentSettingsIn{BaseURL: fake.srv.URL + "/v1", APIKey: agentTestKey}, "ada"); err == nil || !strings.Contains(err.Error(), "PROSPECTOR_SECRET_KEYS") {
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
	if _, err := a.saveAgentConfig(context.Background(), o.ID, AgentSettingsIn{BaseURL: fake.srv.URL + "/v1", APIKey: agentTestKey, Model: "router/auto"}, "ada"); err != nil {
		t.Fatal(err)
	}
	s.keys = mustKeys(t, "2:"+strings.Repeat("cd", 32)+",1:"+mailTestKey)
	if moved, err := s.Reseal(); err != nil || moved != 1 {
		t.Fatalf("resealing: %d %v", moved, err)
	}
	c := must(s.AgentConfigOf(o.ID))
	if key, err := s.keys.open(c.APIKey); err != nil || key != agentTestKey || sealedVersion(c.APIKey) != 2 {
		t.Fatalf("the agent's key didn't move to the new secret key: %v", err)
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
