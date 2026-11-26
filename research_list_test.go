package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type researchPage struct {
	Items []struct {
		Name     string `json:"name"`
		Criteria []struct {
			Field string  `json:"field"`
			Must  bool    `json:"must"`
			State string  `json:"state"`
			At    *string `json:"at"`
		} `json:"criteria"`
		Fit struct {
			Text        string `json:"text"`
			MustsMissed int    `json:"musts_missed"`
		} `json:"fit"`
	} `json:"items"`
	Ranked     bool   `json:"ranked"`
	NextCursor string `json:"next_cursor"`
}

func TestTheResearchQueueIsInTheAPI(t *testing.T) {
	s, o, srv, client := signedIn(t)
	if code, body := call(t, client, "PATCH", srv.URL+"/o/alpha/api/v1/settings", "application/json",
		map[string]any{"name": "Alpha", "color": "#56c5f4", "look_for": criteriaLookFor}); code != http.StatusOK {
		t.Fatalf("saving what we look for: %d %s", code, body)
	}
	task := must(s.CreateTask(o.ID, "Makers", "Boat makers with a configurator.", "Ada Lovelace"))
	bot := Actor{Name: "agent:bot"}
	fact := func(field, site, answer string) FactIn {
		return FactIn{Field: field, Value: answer + " " + field, SourceURL: "https://" + site + "/" + field, Answer: answer}
	}
	for _, c := range []struct {
		name  string
		facts []FactIn
	}{
		{"Corran Craft", []FactIn{fact("configurator", "corran.example", "yes"), fact("payments", "corran.example", "yes")}},
		{"Brine Boats", []FactIn{fact("configurator", "brine.example", "yes")}},
		{"Anchor Works", []FactIn{fact("configurator", "anchor.example", "no"), fact("payments", "anchor.example", "yes")}},
	} {
		if _, err := s.AgentAddCompany(o.ID, bot, AddCompanyIn{Name: c.name, Domain: strings.ToLower(strings.Fields(c.name)[0]) + ".example",
			Facts: c.facts, Task: task.ID}); err != nil {
			t.Fatal(err)
		}
	}
	key, _, err := s.CreateKey(o.ID, "reader", []string{"read"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	list := func(query string) (int, researchPage, string) {
		t.Helper()
		code, body := asKey(t, "GET", srv.URL+"/o/alpha/api/v1/research"+query, key, nil)
		var p researchPage
		if code == http.StatusOK {
			if err := json.Unmarshal([]byte(body), &p); err != nil {
				t.Fatal(err)
			}
		}
		return code, p, body
	}

	code, ranked, body := list(fmt.Sprintf("?task=%d", task.ID))
	if code != http.StatusOK || !ranked.Ranked || len(ranked.Items) != 3 {
		t.Fatalf("a search's queue: %d %s", code, body)
	}
	if got := ranked.Items[0].Name + ", " + ranked.Items[1].Name + ", " + ranked.Items[2].Name; got != "Corran Craft, Brine Boats, Anchor Works" {
		t.Fatalf("the API should rank as the page does: %s", got)
	}
	corran := ranked.Items[0]
	if corran.Fit.Text != "2 of 3 · 1 unchecked" || !corran.Criteria[0].Must || corran.Criteria[0].State != assetMet ||
		corran.Criteria[2].State != assetUnchecked || corran.Criteria[2].At != nil || corran.Criteria[0].At == nil {
		t.Fatalf("each company's answers and fit: %+v", corran)
	}
	if ranked.Items[2].Fit.MustsMissed != 1 {
		t.Fatalf("Anchor misses its must: %+v", ranked.Items[2])
	}

	// A page at a time, as every list is.
	code, first, _ := list(fmt.Sprintf("?task=%d&limit=2", task.ID))
	if code != http.StatusOK || len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("the first page: %d %+v", code, first)
	}
	_, rest, _ := list(fmt.Sprintf("?task=%d&limit=2&cursor=%s", task.ID, first.NextCursor))
	if len(rest.Items) != 1 || rest.Items[0].Name != "Anchor Works" || rest.NextCursor != "" {
		t.Fatalf("the rest: %+v", rest)
	}
	_, all, _ := list("?limit=2")
	_, after, _ := list("?limit=2&cursor=" + all.NextCursor)
	if all.Ranked || len(all.Items) != 2 || all.Items[0].Name != "Anchor Works" || len(after.Items) != 1 {
		t.Fatalf("the whole queue comes newest first, a page at a time: %+v then %+v", all, after)
	}
	if _, rejected, _ := list("?filter=rejected"); len(rejected.Items) != 0 || rejected.Ranked {
		t.Fatalf("nothing is rejected: %+v", rejected)
	}

	if code, _, _ := list("?filter=sideways"); code != http.StatusBadRequest {
		t.Fatalf("an unknown filter: %d", code)
	}
	theirs := must(s.CreateTask(mustOrg(t, s, "beta").ID, "Theirs", "Not yours.", "Grace Hopper"))
	if code, _, _ := list(fmt.Sprintf("?task=%d", theirs.ID)); code != http.StatusNotFound {
		t.Fatalf("another organisation's search: %d", code)
	}
	if code, _ := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/research", "", nil); code != http.StatusOK {
		t.Fatalf("a member reads it too: %d", code)
	}
}
