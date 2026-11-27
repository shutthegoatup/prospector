package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
)

// agentCard is what one of Prospector's agents' cards on the Agents page says, as the API
// gives it: its rail, and its state's words with its search and fallback.
func agentCard(t *testing.T, listed []api.Agent, name string) (string, string) {
	t.Helper()
	for _, a := range listed {
		if a.Name == name {
			return string(a.State.Rail), strings.Join([]string{a.State.Line, a.State.Todo, a.SearchText, a.FallbackText}, "\n")
		}
	}
	t.Fatalf("the Agents page has no card for %s", name)
	return "", ""
}

func TestTheAgentsPageSaysWhichAgentsAreReady(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	s, srv, ada := agentSite(t, fake)
	o := must(s.OrgBySlug("alpha"))
	settings := func(name string, extra map[string]any) map[string]any {
		body := map[string]any{"name": name, "base_url": fake.srv.URL + "/v1", "api_key": agentTestKey, "model": "router/auto"}
		for k, v := range extra {
			body[k] = v
		}
		return body
	}
	if code, body := saveAgentAt(t, ada, srv.URL, settings("Local", map[string]any{"thinking_off": true})); code != http.StatusOK {
		t.Fatalf("adding Local: %d %s", code, body)
	}
	local := must(s.AgentNamed(o.ID, "Local"))
	if !local.ThinkingOff {
		t.Fatal("thinking off wasn't kept")
	}
	if code, body := saveAgentAt(t, ada, srv.URL, settings("Gateway", map[string]any{"tools_url": fake.srv.URL + "/mcp",
		"tools": []string{"brave-search_brave_web_search"}, "fallback": local.ID})); code != http.StatusOK {
		t.Fatalf("adding Gateway: %d %s", code, body)
	}
	if code, body := saveAgentAt(t, ada, srv.URL, settings("Unpicked", map[string]any{"model": ""})); code != http.StatusOK {
		t.Fatalf("adding Unpicked: %d %s", code, body)
	}
	fake.limited = true
	if code, body := saveAgentAt(t, ada, srv.URL, settings("Spent", nil)); code != http.StatusOK {
		t.Fatalf("adding Spent: %d %s", code, body)
	}
	if _, _, err := s.CreateKey(o.ID, "research-bot", []string{"read", "research:write"}, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}

	listed := listedAgents(t, ada, srv.URL)
	for _, c := range []struct{ name, state, want string }{
		{"Gateway", "ok", "✓ Ready"},
		{"Gateway", "ok", "Local, after 15 minutes"},
		{"Gateway", "ok", "✓ through its tools"},
		{"Local", "warn", "Runs, but can't search the web"},
		{"Local", "warn", "nobody: it waits"},
		// A check refused for its limit leaves an agent that still runs.
		{"Spent", "warn", "Last check: Models: the endpoint answered 429"},
		{"Unpicked", "stop", "✕ Can't run"},
		{"Unpicked", "stop", "Pick one of the models the endpoint lists."},
	} {
		if rail, says := agentCard(t, listed, c.name); rail != c.state || !strings.Contains(says, c.want) {
			t.Errorf("%s's card should be %s and say %q: %s %s", c.name, c.state, c.want, rail, says)
		}
	}
	for _, a := range listed {
		if a.ThinkingOff != (a.Name == "Local") {
			t.Errorf("only Local has thinking off: %s %v", a.Name, a.ThinkingOff)
		}
	}
	// Prospector's agents' own keys belong to their cards, not the list of outside keys.
	code, body := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/keys?limit=200", "", nil)
	var keys struct{ Items []api.APIKey }
	if code != http.StatusOK || json.Unmarshal([]byte(body), &keys) != nil {
		t.Fatalf("the keys: %d %s", code, body)
	}
	var outside []string
	for _, k := range keys.Items {
		if !k.Builtin {
			outside = append(outside, k.Name)
		}
	}
	if strings.Join(outside, " ") != "research-bot" {
		t.Errorf("the outside agents' keys: %v", outside)
	}

	// A member sees each agent's state but not its setup.
	u := must(s.CreateUser("grace@example.com", "Grace Hopper", "correct horse battery"))
	if err := s.AddMember(o.ID, u.ID, "member", u.Name); err != nil {
		t.Fatal(err)
	}
	grace := newMailClient()
	if !signIn(t, grace, srv, "grace@example.com", "correct horse battery") {
		t.Fatal("Grace can't sign in")
	}
	if rail, says := agentCard(t, listedAgents(t, grace, srv.URL), "Gateway"); rail != "ok" || !strings.Contains(says, "✓ Ready") {
		t.Errorf("a member should see the agents' states: %s %s", rail, says)
	}
	if code, _ := saveAgentAt(t, grace, srv.URL, settings("Grace's", nil)); code != http.StatusForbidden {
		t.Errorf("a member set an agent up: %d", code)
	}
	if code, _ := call(t, grace, "POST", srv.URL+"/o/alpha/api/v1/keys", "application/json", map[string]any{"name": "mine", "scopes": []string{"read"}}); code != http.StatusForbidden {
		t.Errorf("a member made a key: %d", code)
	}
}

func TestAnAgentWithThinkingOffAsksItsModelNotToThink(t *testing.T) {
	t.Parallel()
	fake := newFakeEndpoint(t)
	r := newRunSetup(t, fake)
	first := must(r.s.AgentByID(r.o.ID, r.agent))
	in := AgentSettingsIn{ID: first.ID, Name: first.Name, BaseURL: first.BaseURL, Model: first.Model, ToolsURL: first.ToolsURL,
		Tools: first.AllowedTools(), ThinkingOff: true}
	must(r.a.saveAgent(context.Background(), r.o.ID, in, "Ada Lovelace"))
	fake.chat = func(int, map[string]any) (int, string) {
		return 200, chatTools("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	if run := r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, first.ID, "Ada Lovelace")).ID); run.Status != RunFinished {
		t.Fatalf("the run: %+v", run)
	}
	fake.mu.Lock()
	body := fake.bodies[len(fake.bodies)-1]
	fake.mu.Unlock()
	kwargs, _ := body["chat_template_kwargs"].(map[string]any)
	if kwargs["enable_thinking"] != false {
		t.Fatalf("the request should turn thinking off: %v", body["chat_template_kwargs"])
	}

	// An agent left to think sends nothing an endpoint might not know.
	in.ThinkingOff = false
	must(r.a.saveAgent(context.Background(), r.o.ID, in, "Ada Lovelace"))
	if err := r.s.SetRequestDone(r.o.ID, r.brief, false, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, first.ID, "Ada Lovelace")).ID)
	fake.mu.Lock()
	body = fake.bodies[len(fake.bodies)-1]
	fake.mu.Unlock()
	if _, ok := body["chat_template_kwargs"]; ok {
		t.Fatal("an agent that thinks was sent chat_template_kwargs")
	}

	if c := must(r.a.saveAgent(context.Background(), r.o.ID, AgentSettingsIn{Name: "Claude", Kind: kindAnthropic, APIKey: claudeTestKey,
		Model: defaultClaude, ThinkingOff: true}, "Ada Lovelace")); c.ThinkingOff {
		t.Fatal("thinking off was kept for an Anthropic agent, which ignores it")
	}
}

// A database from before thinking could be turned off has agents, and they keep thinking.
func TestAgentsFromBeforeThinkingOffStillThink(t *testing.T) {
	t.Parallel()
	dsn := freshDatabase(t)
	old := must(Open(dsn))
	if err := old.db.Migrator().DropTable(models...); err != nil {
		t.Fatal(err)
	}
	if err := old.db.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	o := mustOrg(t, old, "alpha")
	if err := old.db.Create(&Agent{OrgID: o.ID, Name: "Gateway", BaseURL: "https://llm.example/v1", Model: "router/auto"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := old.db.Migrator().DropColumn(&Agent{}, "thinking_off"); err != nil {
		t.Fatal(err)
	}

	s := must(Open(dsn))
	if c := must(s.AgentNamed(o.ID, "Gateway")); c.ThinkingOff {
		t.Fatalf("an agent from before should still think: %+v", c)
	}
}
