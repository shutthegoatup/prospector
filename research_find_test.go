package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestFindStartsTheChosenAgentsOnANewSearch(t *testing.T) {
	fake := newFakeEndpoint(t)
	fake.chat = func(int, map[string]any) (int, string) { return 200, chatText("Nothing more to add.") }
	var app *App
	s, srv, ada, _ := mailSite(t, func(a *App) {
		a.agentClient = fake.srv.Client()
		app = a
	})
	t.Cleanup(app.runs.wg.Wait)
	o := must(s.OrgBySlug("alpha"))
	if code, body := saveAgentAt(t, ada, srv.URL, map[string]any{"name": "Gateway", "base_url": fake.srv.URL + "/v1",
		"api_key": agentTestKey, "model": "router/auto"}); code != http.StatusOK {
		t.Fatalf("adding Gateway: %d %s", code, body)
	}
	gateway := must(s.agentNamed(o.ID, "Gateway"))

	_, page := call(t, ada, "GET", srv.URL+"/o/alpha/research", "", nil)
	for _, want := range []string{`data-form="api/v1/tasks"`, `data-go="research?task={id}"`,
		fmt.Sprintf(`name="agents" value="%d" data-multi="true" data-number="true" checked`, gateway.ID)} {
		if !strings.Contains(page, want) {
			t.Errorf("Research should offer to find, sending Gateway: missing %s", want)
		}
	}
	if strings.Contains(page, `href="/o/alpha/tasks"`) {
		t.Error("the sidebar still has Tasks, which Research now does")
	}

	code, body := call(t, ada, "POST", srv.URL+"/o/alpha/api/v1/tasks", "application/json",
		map[string]any{"title": "", "brief": "Yacht builders with a configurator.", "agents": []int64{gateway.ID, 99999}})
	if code != http.StatusCreated {
		t.Fatalf("finding: %d %s", code, body)
	}
	var started struct {
		ID         int64            `json:"id"`
		Runs       []map[string]any `json:"runs"`
		NotStarted []string         `json:"not_started"`
	}
	if err := json.Unmarshal([]byte(body), &started); err != nil {
		t.Fatal(err)
	}
	if len(started.Runs) != 1 || started.Runs[0]["agent_name"] != "Gateway" || len(started.NotStarted) != 1 {
		t.Fatalf("Gateway should start and the unknown agent be said not to: %s", body)
	}
	app.runs.wg.Wait()

	_, page = call(t, ada, "GET", fmt.Sprintf("%s/o/alpha/research?task=%d", srv.URL, started.ID), "", nil)
	for _, want := range []string{"Yacht builders with a configurator.", "From “Yacht builders with a configurator.”", "Ask for more", "<b>Gateway</b> · "} {
		if !strings.Contains(page, want) {
			t.Errorf("the search's own view is missing %q", want)
		}
	}

	// A key needs agents:run as well to start agents on what it makes.
	key, _, err := s.CreateKey(o.ID, "planner", []string{"tasks:write"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	if code, body := asKey(t, "POST", srv.URL+"/o/alpha/api/v1/tasks", key,
		map[string]any{"title": "", "brief": "Caravan makers.", "agents": []int64{gateway.ID}}); code != http.StatusForbidden || !strings.Contains(body, "agents:run") {
		t.Fatalf("a key without agents:run started agents: %d %s", code, body)
	}
	var tasks int64
	s.db.Model(&Task{}).Where("org_id = ?", o.ID).Count(&tasks)
	if tasks != 1 {
		t.Fatalf("a refused start still made a task: %d tasks", tasks)
	}
	if code, body := asKey(t, "POST", srv.URL+"/o/alpha/api/v1/tasks", key, map[string]any{"title": "", "brief": "Caravan makers."}); code != http.StatusCreated {
		t.Fatalf("a task without agents needs only tasks:write: %d %s", code, body)
	}
}

func TestResearchNarrowsToWhatOneSearchFound(t *testing.T) {
	s, o, srv, client := signedIn(t)
	yachts := must(s.CreateTask(o.ID, "Yachts", "Yacht builders with a configurator.", "Ada Lovelace"))
	key, _, err := s.CreateKey(o.ID, "scout", []string{"read", "research:write"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	add := func(body map[string]any) {
		t.Helper()
		if code, out := asKey(t, "POST", srv.URL+"/o/alpha/api/v1/companies", key, body); code != http.StatusCreated && code != http.StatusOK {
			t.Fatalf("adding %v: %d %s", body["name"], code, out)
		}
	}
	add(map[string]any{"name": "Larkmoor Yachts", "domain": "larkmoor.example", "task": yachts.ID})
	add(map[string]any{"name": "Brindle Kitchens", "domain": "brindle.example"})

	_, all := call(t, client, "GET", srv.URL+"/o/alpha/research", "", nil)
	_, one := call(t, client, "GET", fmt.Sprintf("%s/o/alpha/research?task=%d", srv.URL, yachts.ID), "", nil)
	if !strings.Contains(all, "Larkmoor Yachts") || !strings.Contains(all, "Brindle Kitchens") {
		t.Fatal("the whole queue should hold both companies")
	}
	if !strings.Contains(one, "Larkmoor Yachts") || strings.Contains(one, "Brindle Kitchens") {
		t.Fatal("a search's queue should hold only what it found")
	}
	if !strings.Contains(one, fmt.Sprintf(`href="/o/alpha/research?f=rejected&amp;task=%d"`, yachts.ID)) {
		t.Error("the filters should keep to the search")
	}
	if !strings.Contains(all, "Show these") || !strings.Contains(all, "1 company found") {
		t.Error("the open search should show above the queue, with what it found")
	}

	if code, _ := call(t, client, "GET", srv.URL+"/o/alpha/research?task=99999", "", nil); code != http.StatusNotFound {
		t.Fatalf("an unknown search: %d", code)
	}
	other := mustOrg(t, s, "beta")
	theirs := must(s.CreateTask(other.ID, "Theirs", "Not yours.", "Grace Hopper"))
	if code, _ := call(t, client, "GET", fmt.Sprintf("%s/o/alpha/research?task=%d", srv.URL, theirs.ID), "", nil); code != http.StatusNotFound {
		t.Fatalf("another organisation's search was shown: %d", code)
	}
}
