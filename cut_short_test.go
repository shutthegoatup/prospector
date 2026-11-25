package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// firstOf drops the middle of three results, for must.
func firstOf[T, U any](v T, _ U, err error) (T, error) { return v, err }

func TestASearchSaysWhenItListsOnlySome(t *testing.T) {
	s, o, srv, _ := signedIn(t)
	for i := range searchShows {
		must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, AddCompanyIn{Name: fmt.Sprintf("Larkmoor Rooms %d", i)}))
	}
	hits, more, err := s.AgentSearch(o.ID, Actor{}, "larkmoor")
	if err != nil || len(hits) != searchShows || more {
		t.Fatalf("exactly as many as a search lists: %d hits, more=%v, %v", len(hits), more, err)
	}
	must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, AddCompanyIn{Name: "Larkmoor Rooms and one more"}))
	hits, more, err = s.AgentSearch(o.ID, Actor{}, "larkmoor")
	if err != nil || len(hits) != searchShows || !more {
		t.Fatalf("one more than a search lists: %d hits, more=%v, %v", len(hits), more, err)
	}

	co := must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, AddCompanyIn{Name: "Fenwick"}))
	for i := range searchShows + 1 {
		must(s.AgentAddContact(o, Actor{Name: "agent:scout"}, AddContactIn{Company: co.Name, Name: fmt.Sprintf("Quillon V%cle", rune(0x61+i)),
			SourceURL: "https://fenwick.example/team"}))
	}
	if hits, more, err := s.AgentSearch(o.ID, Actor{}, "quillon"); err != nil || len(hits) != searchShows || !more {
		t.Fatalf("more people than a search lists: %d hits, more=%v, %v", len(hits), more, err)
	}

	key, _, err := s.CreateKey(o.ID, "bot", []string{"read"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, AddCompanyIn{Name: "Harrow"}))
	for q, want := range map[string]bool{"larkmoor": true, "harrow": false} {
		req, _ := http.NewRequest("GET", srv.URL+"/o/alpha/api/v1/search?q="+q, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var out SearchOut
		err = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		if err != nil || out.More != want || len(out.Hits) == 0 {
			t.Fatalf("the API's search for %q: %d hits, more=%v, %v", q, len(out.Hits), out.More, err)
		}
	}
}

func TestTheTaskListSaysWhenThereAreMore(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	rows := make([]Task, tasksShown)
	for i := range rows {
		rows[i] = Task{OrgID: o.ID, Title: fmt.Sprint("Task ", i), CreatedBy: "Nia"}
	}
	if err := s.db.CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	if list, more, err := s.AgentTasks(o.ID, Actor{Name: "agent:scout", ReadOnly: true}); err != nil || len(list) != tasksShown || more {
		t.Fatalf("exactly as many as the list holds: %d, more=%v, %v", len(list), more, err)
	}
	must(s.CreateTask(o.ID, "", "One more", "Nia"))
	list, more, err := s.AgentTasks(o.ID, Actor{Name: "agent:scout", ReadOnly: true})
	if err != nil || len(list) != tasksShown || !more || !strings.Contains(list[0].Title, "One more") {
		t.Fatalf("one more than the list holds: %d, more=%v, %v", len(list), more, err)
	}
}
