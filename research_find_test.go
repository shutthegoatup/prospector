package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
)

func TestFindStartsTheChosenAgentsOnANewSearch(t *testing.T) {
	t.Parallel()
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
	gateway := must(s.AgentNamed(o.ID, "Gateway"))

	// Research offers to find with the agents that are ready, the first of them chosen.
	_, list := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/agents", "", nil)
	var agents struct{ Items []api.Agent }
	if json.Unmarshal([]byte(list), &agents) != nil || len(agents.Items) != 1 || agents.Items[0].Id != gateway.ID || !agents.Items[0].Ready {
		t.Errorf("Research should be able to send Gateway: %s", list)
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

	// The search's own view: its brief, what it found and Gateway's run.
	_, one := call(t, ada, "GET", fmt.Sprintf("%s/o/alpha/api/v1/tasks/%d", srv.URL, started.ID), "", nil)
	if !strings.Contains(one, `"brief":"Yacht builders with a configurator."`) {
		t.Errorf("the search's own view: %s", one)
	}
	_, runs := call(t, ada, "GET", fmt.Sprintf("%s/o/alpha/api/v1/tasks/%d/runs", srv.URL, started.ID), "", nil)
	if !strings.Contains(runs, `"agent_name":"Gateway"`) {
		t.Errorf("the search's runs lack Gateway's: %s", runs)
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
	t.Parallel()
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

	names := func(query string) (int, string) {
		t.Helper()
		code, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/research"+query, "", nil)
		var page struct {
			Items  []api.ResearchCompany
			Counts map[string]int
		}
		if code != http.StatusOK {
			return code, ""
		}
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range page.Items {
			out = append(out, c.Name)
		}
		return code, strings.Join(out, ", ") + fmt.Sprintf(" (%d to review)", page.Counts["review"])
	}
	_, all := names("")
	_, one := names(fmt.Sprintf("?task=%d", yachts.ID))
	if !strings.Contains(all, "Larkmoor Yachts") || !strings.Contains(all, "Brindle Kitchens") || !strings.Contains(all, "(2 to review)") {
		t.Fatalf("the whole queue should hold both companies: %s", all)
	}
	if !strings.Contains(one, "Larkmoor Yachts") || strings.Contains(one, "Brindle Kitchens") || !strings.Contains(one, "(1 to review)") {
		t.Fatalf("a search's queue, and its counts, should hold only what it found: %s", one)
	}
	if _, rejected := names(fmt.Sprintf("?filter=rejected&task=%d", yachts.ID)); rejected != " (1 to review)" {
		t.Errorf("the filters should keep to the search: %s", rejected)
	}
	// The open search shows above the queue, with what it found.
	if _, open := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/tasks?limit=5", "", nil); !strings.Contains(open, `"title":"Yachts"`) || !strings.Contains(open, `"found":{"companies":1`) {
		t.Errorf("the open search with what it found: %s", open)
	}

	if code, _ := names("?task=99999"); code != http.StatusNotFound {
		t.Fatalf("an unknown search: %d", code)
	}
	other := mustOrg(t, s, "beta")
	theirs := must(s.CreateTask(other.ID, "Theirs", "Not yours.", "Grace Hopper"))
	if code, _ := names(fmt.Sprintf("?task=%d", theirs.ID)); code != http.StatusNotFound {
		t.Fatalf("another organisation's search was shown: %d", code)
	}
}
