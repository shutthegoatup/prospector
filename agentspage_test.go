package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// agentCard is the HTML of one of Prospector's agents' cards on the Agents page.
func agentCard(t *testing.T, s *Store, page, name string) string {
	t.Helper()
	c := must(s.agentNamed(must(s.OrgBySlug("alpha")).ID, name))
	at := strings.Index(page, fmt.Sprintf(`id="agent-%d"`, c.ID))
	if at < 0 {
		t.Fatalf("the Agents page has no card for %s", name)
	}
	start := strings.LastIndex(page[:at], "<details")
	end := strings.Index(page[at:], "</details>")
	return page[start : at+end]
}

func TestTheAgentsPageSaysWhichAgentsAreReady(t *testing.T) {
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
	local := must(s.agentNamed(o.ID, "Local"))
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

	_, page := call(t, ada, "GET", srv.URL+"/o/alpha/agents", "", nil)
	gateway, localCard, spent := agentCard(t, s, page, "Gateway"), agentCard(t, s, page, "Local"), agentCard(t, s, page, "Spent")
	unpicked := agentCard(t, s, page, "Unpicked")
	for _, c := range []struct{ html, state, want string }{
		{gateway, "ok", "✓ Ready"},
		{gateway, "ok", "Local, after 15 minutes"},
		{gateway, "ok", "✓ through its tools"},
		{localCard, "warn", "Runs, but can&#39;t search the web"},
		{localCard, "warn", "thinking off"},
		{localCard, "warn", "nobody: it waits"},
		// A check refused for its limit leaves an agent that still runs.
		{spent, "warn", "Last check: Models: the endpoint answered 429"},
		{unpicked, "stop", "✕ Can&#39;t run"},
		{unpicked, "stop", "Pick one of the models the endpoint lists."},
	} {
		if !strings.Contains(c.html, `data-state="`+c.state+`"`) || !strings.Contains(c.html, c.want) {
			t.Errorf("a card should be %s and say %q:\n%s", c.state, c.want, c.html)
		}
	}
	if !strings.Contains(localCard, `name="thinking_off" checked`) {
		t.Error("Local's form should have thinking off ticked")
	}
	for _, want := range []string{"1 ready", "2 missing something", "1 can&#39;t run", "research-bot", "Connects with a key", "Your own agent connects"} {
		if !strings.Contains(page, want) {
			t.Errorf("the Agents page is missing %q", want)
		}
	}
	// Prospector's agents' own keys belong to their cards, not the list of keys.
	if strings.Contains(page, builtinKeyName("Gateway")) {
		t.Error("an agent's built-in key is shown as an outside agent")
	}

	_, settingsPage := call(t, ada, "GET", srv.URL+"/o/alpha/settings", "", nil)
	if strings.Contains(settingsPage, `data-form="api/v1/agents"`) || !strings.Contains(settingsPage, `href="/o/alpha/agents"`) {
		t.Error("Settings should point to the Agents page rather than set agents up itself")
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
	_, page = call(t, grace, "GET", srv.URL+"/o/alpha/agents", "", nil)
	if !strings.Contains(page, "✓ Ready") || strings.Contains(page, `data-form="api/v1/agents`) || strings.Contains(page, `data-form="ui/keys"`) {
		t.Error("a member should see the agents' states without their forms")
	}
}

func TestAnAgentWithThinkingOffAsksItsModelNotToThink(t *testing.T) {
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
	dsn := filepath.Join(t.TempDir(), "old.sqlite")
	if pg := os.Getenv("PROSPECTOR_TEST_PG"); pg != "" {
		dsn = pg
	}
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
	if c := must(s.agentNamed(o.ID, "Gateway")); c.ThinkingOff {
		t.Fatalf("an agent from before should still think: %+v", c)
	}
}
