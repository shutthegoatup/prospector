package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/internal/store"
)

const claudeTestKey = "sk-ant-test-fedcba9876543210"

// fakeClaude answers as Anthropic's API does: a model list, and messages scripted turn
// by turn, recording what each request carried.
type fakeClaude struct {
	srv     *httptest.Server
	mu      sync.Mutex
	reply   func(n int) (int, string)
	bodies  []map[string]any
	headers []http.Header
}

func newFakeClaude(t *testing.T) *fakeClaude {
	t.Helper()
	f := &fakeClaude{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		if claudeKeyOf(r.Header) != claudeTestKey {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"type":"model","id":"claude-opus-5-5","display_name":"Claude Opus 5.5","created_at":"2026-10-01T00:00:00Z"},` +
			`{"type":"model","id":"claude-haiku-5-5","display_name":"Claude Haiku 5.5","created_at":"2026-10-01T00:00:00Z"}],` +
			`"has_more":false,"first_id":"claude-opus-5-5","last_id":"claude-haiku-5-5"}`))
	})
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		n := len(f.bodies)
		f.bodies = append(f.bodies, body)
		f.headers = append(f.headers, r.Header.Clone())
		reply := f.reply
		f.mu.Unlock()
		status, text := reply(n)
		if status < 0 {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "45")
		}
		w.WriteHeader(status)
		w.Write([]byte(text))
	})
	f.srv = httptest.NewTLSServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// claudeKeyOf is the API key a request carries, in either header Anthropic takes.
func claudeKeyOf(h http.Header) string {
	if key := h.Get("X-Api-Key"); key != "" {
		return key
	}
	return strings.TrimPrefix(h.Get("Authorization"), "Bearer ")
}

// claudeTurn is a message from Claude calling tools, given as name, input pairs.
func claudeTurn(calls ...string) string {
	content := []map[string]any{{"type": "text", "text": "Working on it."}}
	for i := 0; i+1 < len(calls); i += 2 {
		var input any
		json.Unmarshal([]byte(calls[i+1]), &input)
		content = append(content, map[string]any{"type": "tool_use", "id": fmt.Sprintf("toolu_%d", i/2), "name": calls[i], "input": input})
	}
	stop := "tool_use"
	if len(calls) == 0 {
		stop = "end_turn"
	}
	raw, _ := json.Marshal(map[string]any{"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
		"content": content, "stop_reason": stop,
		"usage": map[string]any{"input_tokens": 300, "output_tokens": 40, "cache_read_input_tokens": 900, "cache_creation_input_tokens": 100}})
	return string(raw)
}

// claudeSetup is a run setup whose agent is Claude, through the fake, with the tools
// on the fake OpenAI-compatible endpoint's MCP server.
func claudeSetup(t *testing.T) (*runSetup, *fakeClaude, *fakeEndpoint) {
	t.Helper()
	tools, claude := newFakeEndpoint(t), newFakeClaude(t)
	tools.srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs.AddCert(claude.srv.Certificate())
	r := newRunSetup(t, tools)
	c := must(r.a.saveAgent(context.Background(), r.o.ID, AgentSettingsIn{Name: "Claude", Kind: store.KindAnthropic, BaseURL: claude.srv.URL,
		APIKey: claudeTestKey, ToolsURL: tools.srv.URL + "/mcp", Tools: []string{searchTool}}, "Ada Lovelace"))
	r.agent = c.ID
	return r, claude, tools
}

func TestAClaudeAgentIsSetUpFromAnthropicsModelList(t *testing.T) {
	t.Parallel()
	r, _, tools := claudeSetup(t)
	c := must(r.s.AgentByID(r.o.ID, r.agent))
	if c.Kind != store.KindAnthropic || c.Model != defaultClaude || strings.Join(c.ModelList(), " ") != "claude-haiku-5-5 claude-opus-5-5" || !c.Ready() {
		t.Fatalf("an Anthropic agent should list Anthropic's models and pick Claude Opus 5.5: %+v", c)
	}
	if len(c.ToolList()) != 2 || c.Problem != "" {
		t.Fatalf("its tools should be listed from the MCP server: %+v", c)
	}
	_, toolAuth := tools.auths()
	for _, h := range toolAuth {
		if strings.Contains(h, claudeTestKey) {
			t.Fatal("the Anthropic key was sent to a tool server")
		}
	}
	if (store.Agent{Kind: store.KindAnthropic, APIKey: "sealed", Model: defaultClaude}).Ready() != true {
		t.Fatal("an Anthropic agent needs no endpoint of its own")
	}
	if (store.Agent{Kind: store.KindOpenAI, APIKey: "sealed", Model: "gpt"}).Ready() {
		t.Fatal("an OpenAI-compatible agent needs its endpoint")
	}
	if _, err := r.a.saveAgent(context.Background(), r.o.ID, AgentSettingsIn{Name: "Odd", Kind: "carrier-pigeon", BaseURL: "https://x.example/v1", APIKey: agentTestKey}, "Ada Lovelace"); err == nil {
		t.Fatal("an unknown kind was taken")
	}
}

func TestAClaudeAgentWorksATaskThroughAnthropicsAPI(t *testing.T) {
	t.Parallel()
	r, claude, tools := claudeSetup(t)
	claude.reply = func(n int) (int, string) {
		switch n {
		case 0:
			return 200, claudeTurn(searchTool, `{"query":"yacht builders with a configurator"}`)
		case 1:
			return 200, claudeTurn(
				"add_company", `{"name":"Larkmoor Yachts","facts":[{"field":"configurator","value":"Day cruiser builder","source_url":"https://larkmoor.example/build"}]}`,
				"add_fact", `{"company":"Larkmoor Yachts","field":"size","value":"40 staff","source_url":"https://made-up.example/about"}`)
		}
		return 200, claudeTurn("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	got := r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
	if got.Status != RunFinished || got.Turns != 3 || got.Added != 1 || got.TokensIn != 3*1300 || got.TokensOut != 3*40 {
		t.Fatalf("a run with Claude: %+v", got)
	}
	if facts := must(r.s.Facts(r.o.ID, must(r.s.CompanyRef(r.o.ID, "Larkmoor Yachts")).ID)); len(facts) != 1 {
		t.Fatalf("only the fact from a page Claude was shown should be stored: %+v", facts)
	}
	tools.mu.Lock()
	used := strings.Join(tools.toolUse, " ")
	tools.mu.Unlock()
	if used != searchTool {
		t.Fatalf("the MCP tool Claude called: %q", used)
	}

	claude.mu.Lock()
	defer claude.mu.Unlock()
	first := claude.bodies[0]
	if claudeKeyOf(claude.headers[0]) != claudeTestKey || !strings.Contains(claude.headers[0].Get("Anthropic-Beta"), "server-side-fallback-2026-07-01") ||
		first["fallbacks"] != "default" || first["model"] != defaultClaude || first["cache_control"] == nil || fmt.Sprint(first["thinking"]) != "map[type:adaptive]" {
		t.Fatalf("a request to Claude: headers %v, body %v", claude.headers[0], first)
	}
	if sys, _ := first["system"].([]any); len(sys) != 1 || !strings.Contains(fmt.Sprint(sys[0]), "not instructions") {
		t.Fatalf("the system prompt: %v", first["system"])
	}
	var names []string
	for _, tool := range first["tools"].([]any) {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	if all := " " + strings.Join(names, " ") + " "; !strings.Contains(all, " get_task ") || !strings.Contains(all, " "+searchTool+" ") || strings.Contains(all, " draft_message ") {
		t.Fatalf("Claude's tools: %v", names)
	}
	// Each request holds the one before it as it was, and answers every tool call it made.
	third := claude.bodies[2]["messages"].([]any)
	if len(third) != 5 {
		t.Fatalf("the third request should hold the whole exchange: %d messages", len(third))
	}
	prior, _ := json.Marshal(claude.bodies[1]["messages"])
	so, _ := json.Marshal(third[:3])
	if string(prior) != string(so) {
		t.Fatal("the history Claude was sent changed between turns")
	}
	answers := third[4].(map[string]any)
	results, _ := answers["content"].([]any)
	if answers["role"] != "user" || len(results) != 2 || !strings.Contains(fmt.Sprint(results[1]), "wasn't on any page") ||
		results[0].(map[string]any)["tool_use_id"] != "toolu_0" || results[1].(map[string]any)["tool_use_id"] != "toolu_1" {
		t.Fatalf("both tool results should go back in one message, each to its call: %v", answers)
	}
}

func TestAnAgentChangesKindOnlyWhenTold(t *testing.T) {
	t.Parallel()
	r, _, tools := claudeSetup(t)
	ctx := context.Background()
	c := must(r.s.AgentByID(r.o.ID, r.agent))
	same := AgentSettingsIn{ID: c.ID, Name: c.Name, BaseURL: c.BaseURL, Model: c.Model, ToolsURL: c.ToolsURL, Tools: []string{searchTool}}
	kept := must(r.a.saveAgent(ctx, r.o.ID, same, "Ada Lovelace"))
	if kept.Kind != store.KindAnthropic || kept.Problem != "" {
		t.Fatalf("a save that names no kind should keep the agent's own, and its key: %+v", kept)
	}

	toOpenAI := same
	toOpenAI.Kind = store.KindOpenAI
	if _, err := r.a.saveAgent(ctx, r.o.ID, toOpenAI, "Ada Lovelace"); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("a change of kind should need its key entered again: %v", err)
	}
	toOpenAI.BaseURL, toOpenAI.APIKey, toOpenAI.Model = tools.srv.URL+"/v1", agentTestKey, ""
	moved := must(r.a.saveAgent(ctx, r.o.ID, toOpenAI, "Ada Lovelace"))
	stored := must(r.s.AgentByID(r.o.ID, r.agent))
	if stored.Kind != store.KindOpenAI || slices.Contains(stored.ModelList(), defaultClaude) || moved.Kind != store.KindOpenAI {
		t.Fatalf("the new kind should be stored, with the old endpoint's models gone: %+v", stored)
	}
}

func TestAClaudeRunWaitsOutAnOverloadAnOutageAndADroppedConnection(t *testing.T) {
	t.Parallel()
	r, claude, _ := claudeSetup(t)
	claude.reply = func(n int) (int, string) {
		switch n {
		case 0:
			return 529, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`
		case 1:
			return http.StatusBadGateway, `bad gateway`
		case 2:
			return -1, ""
		case 3:
			// Claude can answer a tool's result with nothing at all.
			return 200, `{"id":"msg_3","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":0}}`
		}
		return 200, claudeTurn("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	got := r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
	if got.Status != RunFinished || len(r.sleeps) != 3 {
		t.Fatalf("a run should wait out an overload, an outage and a dropped connection, and go on past an empty answer: %+v, waited %v", got, r.sleeps)
	}
	claude.mu.Lock()
	defer claude.mu.Unlock()
	for _, m := range claude.bodies[4]["messages"].([]any) {
		if content, _ := m.(map[string]any)["content"].([]any); len(content) == 0 {
			t.Fatalf("a message with nothing in it was sent on: %v", claude.bodies[4]["messages"])
		}
	}
}

func TestAClaudeRunWaitsOutItsLimitAndEndsOnAWrongModelOrARefusal(t *testing.T) {
	t.Parallel()
	r, claude, _ := claudeSetup(t)
	claude.reply = func(n int) (int, string) {
		if n == 0 {
			return http.StatusTooManyRequests, `{"type":"error","error":{"type":"rate_limit_error","message":"Number of requests has exceeded your rate limit"}}`
		}
		return 200, claudeTurn("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	got := r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
	if got.Status != RunFinished || len(r.sleeps) != 1 || r.sleeps[0].Round(time.Second) != 45*time.Second {
		t.Fatalf("a run should wait as long as Anthropic says, then carry on: %+v, waited %v", got, r.sleeps)
	}

	for reason, reply := range map[string]string{
		"model: claude-opus-9": `{"type":"error","error":{"type":"not_found_error","message":"model: claude-opus-9"}}`,
		"declined":             `{"id":"msg_2","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":"refusal","usage":{"input_tokens":10,"output_tokens":0}}`,
	} {
		if err := r.s.SetRequestDone(r.o.ID, r.brief, false, "Ada Lovelace"); err != nil {
			t.Fatal(err)
		}
		status := http.StatusNotFound
		if reason == "declined" {
			status = http.StatusOK
		}
		claude.reply = func(int) (int, string) { return status, reply }
		got := r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
		if got.Status != RunFailed || !strings.Contains(got.StopReason, reason) {
			t.Fatalf("a run should end saying %q: %+v", reason, got)
		}
	}
}

func TestAClaudeAgentNeverUsesTheServersOwnAnthropicCredentials(t *testing.T) {
	claude := newFakeClaude(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-the-operators-own")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "the-operators-token")
	t.Setenv("ANTHROPIC_BASE_URL", "https://elsewhere.example")
	t.Setenv("ANTHROPIC_CUSTOM_HEADERS", "X-Operator: secret")
	a := &App{agentClient: claude.srv.Client()}
	claude.reply = func(int) (int, string) { return 200, claudeTurn() }
	chat := a.newClaudeChat(&store.Agent{Kind: store.KindAnthropic, BaseURL: claude.srv.URL, Model: defaultClaude}, claudeTestKey, nil, "system", "hello")
	if _, err := chat.next(context.Background()); err != nil {
		t.Fatalf("a message through the agent's own endpoint: %v", err)
	}
	claude.mu.Lock()
	defer claude.mu.Unlock()
	h := claude.headers[0]
	if claudeKeyOf(h) != claudeTestKey || h.Get("X-Operator") != "" || strings.Contains(fmt.Sprint(h), "operators") {
		t.Fatalf("the server's own Anthropic credentials or headers went with the agent's request: %v", h)
	}
}

func TestClaudeToolsKeepTheirSchemas(t *testing.T) {
	t.Parallel()
	defs := []toolDef{{Type: "function"}}
	defs[0].Function.Name, defs[0].Function.Description = "add_fact", "Add a fact."
	defs[0].Function.Parameters = json.RawMessage(`{"type":"object","properties":{"company":{"type":"string"}},"required":["company"],"additionalProperties":false}`)
	raw, _ := json.Marshal(claudeTools(defs))
	var got []struct {
		Name        string         `json:"name"`
		InputSchema map[string]any `json:"input_schema"`
	}
	json.Unmarshal(raw, &got)
	var want map[string]any
	json.Unmarshal(defs[0].Function.Parameters, &want)
	if len(got) != 1 || got[0].Name != "add_fact" || fmt.Sprint(got[0].InputSchema) != fmt.Sprint(want) {
		t.Fatalf("a tool's schema as Claude gets it: %s", raw)
	}
}
