package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/api"
)

// The React Agents page draws each card from the API, so the API says what the card says.
func TestTheAPISaysWhatEachAgentsCardSays(t *testing.T) {
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
	local := must(s.agentNamed(o.ID, "Local"))
	code, body := saveAgentAt(t, ada, srv.URL, settings("Gateway", map[string]any{"tools_url": fake.srv.URL + "/mcp",
		"tools": []string{"brave-search_brave_web_search"}, "fallback": local.ID}))
	var saved api.AgentSaved
	if json.Unmarshal([]byte(body), &saved); code != http.StatusOK || saved.Agent.FallbackText != "Local, after 15 minutes" || saved.Agent.State.Rail != "ok" {
		t.Fatalf("a saved agent should come back with its card worked out: %d %s", code, body)
	}
	if code, body := saveAgentAt(t, ada, srv.URL, settings("Unpicked", map[string]any{"model": ""})); code != http.StatusOK {
		t.Fatalf("adding Unpicked: %d %s", code, body)
	}

	_, body = call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/agents", "", nil)
	var list struct{ Items []api.Agent }
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatal(err)
	}
	cards := map[string]api.Agent{}
	for _, a := range list.Items {
		cards[a.Name] = a
	}
	for _, c := range []struct {
		name, rail, line, todo, search, tools, fallback string
		searches                                        bool
	}{
		{"Gateway", "ok", "✓ Ready", "", "✓ through its tools", "1 of 2 allowed", "Local, after 15 minutes", true},
		{"Local", "warn", "● Runs, but can't search the web", "Add a search address or allow a search tool", "none", "built-in only", "nobody: it waits", false},
		{"Unpicked", "stop", "✕ Can't run", "Pick one of the models the endpoint lists.", "none", "built-in only", "nobody: it waits", false},
	} {
		a := cards[c.name]
		if string(a.State.Rail) != c.rail || !strings.HasPrefix(a.State.Line, c.line) || !strings.HasPrefix(a.State.Todo, c.todo) || a.SearchText != c.search ||
			a.ToolsText != c.tools || a.FallbackText != c.fallback || a.Searches != c.searches {
			t.Errorf("%s's card should be %+v, is %+v", c.name, c, a)
		}
	}
	if code, body := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/agents/"+itoa(local.ID), "", nil); code != http.StatusOK || !strings.Contains(body, `"rail":"warn"`) {
		t.Errorf("one agent read on its own should carry its card too: %d %s", code, body)
	}

	// Where an outside agent connects, at the server's public address, with every scope and tool the page lists.
	_, body = call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/connect", "", nil)
	var connect api.Connect
	if err := json.Unmarshal([]byte(body), &connect); err != nil {
		t.Fatal(err)
	}
	if connect.BaseUrl != testSite+"/o/alpha/" || connect.McpUrl != connect.BaseUrl+"mcp" || !connect.CanKeepKeys || len(connect.Scopes) != len(Scopes) ||
		len(connect.Tools) != len(mcpTools) || len(connect.Kinds) != len(AgentKinds) {
		t.Errorf("how to connect: %s", body)
	}
}

func TestWhatAgentsDidIsInTheAPI(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	scout, _, err := s.CreateKey(o.ID, "scout", []string{"read", "research:write"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	if code, body := asKey(t, "POST", srv.URL+"/o/alpha/api/v1/companies", scout, map[string]any{"name": "Larkmoor Yachts", "domain": "larkmoor.example"}); code >= 300 {
		t.Fatalf("the scout adding a company: %d %s", code, body)
	}

	_, body := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/agent-scores", "", nil)
	if !strings.Contains(body, `"agent":"scout"`) || !strings.Contains(body, `"text":"Companies: 1 suggested, 0 qualified (0%), 0 rejected"`) {
		t.Errorf("the scout's score: %s", body)
	}
	_, body = call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/agent-work", "", nil)
	var work struct{ Items []api.AgentWork }
	if json.Unmarshal([]byte(body), &work); len(work.Items) != 1 || work.Items[0].Actor != "agent:scout" || !work.Items[0].Going ||
		!strings.Contains(work.Items[0].Text, "1 change at 1 company") {
		t.Errorf("the scout's work: %s", body)
	}

	// Activity comes a page at a time, saying how much there is for the page's "Page 1 of 2".
	_, before := s.agentEvents(o.ID, 0, 0)
	start := time.Now().Add(-time.Hour)
	var events []Event
	for i := range 24 {
		events = append(events, Event{OrgID: o.ID, User: "agent:scout", Action: "add:fact", Value: new("employees: 40"), At: start.Add(time.Duration(i) * time.Second)})
	}
	if err := s.db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	var seen []api.AgentEvent
	cursor := ""
	for pages := 0; pages < 5; pages++ {
		_, body = call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/agent-activity?limit=20&cursor="+cursor, "", nil)
		var page struct {
			Items      []api.AgentEvent
			Total      int
			NextCursor string `json:"next_cursor"`
		}
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatal(err)
		}
		if page.Total != before+24 {
			t.Fatalf("activity should say there are %d in all: %s", before+24, body)
		}
		seen = append(seen, page.Items...)
		if cursor = page.NextCursor; cursor == "" {
			break
		}
	}
	if len(seen) != before+24 || seen[0].Actor != "scout" || !strings.HasPrefix(seen[0].Text, "added the company") || seen[len(seen)-1].Text != "added a fact: employees: 40" {
		t.Errorf("walking the activity: %d of %d, newest %+v, oldest %+v", len(seen), before+24, seen[0], seen[len(seen)-1])
	}
}

func TestOnlyASignedInOwnerMakesKeysThroughTheAPI(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	keys := srv.URL + "/o/alpha/api/v1/keys"

	code, body := call(t, ada, "POST", keys, "application/json", map[string]any{"name": "research-bot", "scopes": []string{"read", "research:write"}})
	var made api.KeyMade
	if json.Unmarshal([]byte(body), &made); code != http.StatusCreated || !strings.HasPrefix(made.Secret, "pk_") ||
		made.Message != "Copy this key now; it isn't shown again: "+made.Secret || made.Key.Builtin || made.Key.Prefix != made.Secret[:7] {
		t.Fatalf("an owner making a key: %d %s", code, body)
	}
	if code, _ := asKey(t, "GET", srv.URL+"/o/alpha/api/v1/agents", made.Secret, nil); code != http.StatusOK {
		t.Fatalf("the key made doesn't work: %d", code)
	}
	if _, body := call(t, ada, "GET", keys, "", nil); !strings.Contains(body, `"name":"research-bot"`) || strings.Contains(body, made.Secret) || strings.Contains(body, "hash") {
		t.Errorf("the list of keys should name it without its secret: %s", body)
	}

	path := keys + "/" + itoa(made.Key.Id)
	code, body = call(t, ada, "PATCH", path, "application/json", map[string]any{"scopes": []string{"drafts:write", "read"}})
	var changed api.KeyChanged
	if json.Unmarshal([]byte(body), &changed); code != http.StatusOK || changed.Message != "Saved ✓ Agents connected over MCP have to reconnect." ||
		strings.Join(changed.Key.Scopes, " ") != "read drafts:write" {
		t.Fatalf("an owner changing a key's scopes: %d %s", code, body)
	}

	// No key makes, widens or revokes a key, not even one that configures agents.
	configurer, _, err := s.CreateKey(o.ID, "configurer", []string{"read", "agents:configure", "settings:write"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"POST", keys, map[string]any{"name": "wider", "scopes": []string{"import"}}},
		{"PATCH", path, map[string]any{"scopes": []string{"import"}}},
		{"DELETE", path, nil},
	} {
		if code, body := asKey(t, c.method, c.path, configurer, c.body); code != http.StatusForbidden || !strings.Contains(body, "no key can") {
			t.Errorf("a key at %s %s: %d %s", c.method, c.path, code, body)
		}
	}
	if code, _ := asKey(t, "GET", keys, configurer, nil); code != http.StatusOK {
		t.Errorf("a read key listing keys: %d", code)
	}

	// A member reads the keys, as the page shows them, but only an owner changes them.
	mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	grace := signInAs(t, srv, "grace@example.com", "correct horse battery")
	if code, _ := call(t, grace, "GET", keys, "", nil); code != http.StatusOK {
		t.Errorf("a member listing keys: %d", code)
	}
	if code, _ := call(t, grace, "POST", keys, "application/json", map[string]any{"name": "mine", "scopes": []string{"read"}}); code != http.StatusForbidden {
		t.Errorf("a member making a key: %d", code)
	}
	if code, _ := call(t, grace, "DELETE", path, "", nil); code != http.StatusForbidden {
		t.Errorf("a member revoking a key: %d", code)
	}

	if code, body := call(t, ada, "DELETE", path, "", nil); code != http.StatusNoContent {
		t.Fatalf("an owner revoking a key: %d %s", code, body)
	}
	if code, _ := asKey(t, "GET", srv.URL+"/o/alpha/api/v1/agents", made.Secret, nil); code != http.StatusUnauthorized {
		t.Errorf("a revoked key still works: %d", code)
	}
	if _, body := call(t, ada, "GET", keys, "", nil); strings.Contains(body, "research-bot") {
		t.Errorf("a revoked key is still listed: %s", body)
	}
}
