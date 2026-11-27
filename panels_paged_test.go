package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestACompanysHistoryAndNotesHavePages(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	co := must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, AddCompanyIn{Name: "Larkmoor"}))
	var events []Event
	for i := range historyPageSize + 9 {
		action := "mark:email"
		if i%2 == 1 {
			action = "set:phone"
		}
		events = append(events, Event{OrgID: o.ID, CompanyID: &co.ID, User: "Nia", Action: action, Value: str(fmt.Sprint("2020-01-", 10+i%10)),
			At: time.Date(2020, 1, 1, 0, i, 0, 0, time.UTC)})
	}
	if err := s.DB.CreateInBatches(events, 50).Error; err != nil {
		t.Fatal(err)
	}
	notes := make([]TaskNote, companyNotesPageSize+3)
	for i := range notes {
		notes[i] = TaskNote{OrgID: o.ID, CompanyID: co.ID, Actor: "agent:scout", Kind: "next", Text: fmt.Sprint("Left note ", i, ".")}
	}
	if err := s.DB.CreateInBatches(notes, 50).Error; err != nil {
		t.Fatal(err)
	}

	first, pg, err := s.History(o.ID, co.ID, 1)
	if err != nil || len(first) != historyPageSize || pg.Pages() != 2 || pg.Total != len(events)+1 {
		t.Fatalf("the first page of history: %d rows, %+v, %v", len(first), pg, err)
	}
	last, _, err := s.History(o.ID, co.ID, 2)
	if err != nil || len(last) != 10 || !last[len(last)-1].At.Equal(events[0].At) {
		t.Fatalf("the last page of history should end on the oldest thing: %d rows, %v", len(last), err)
	}
	// Half of them were someone being reached out to, whichever page is on show.
	if n, err := s.Touches(o.ID, co.ID); err != nil || n != (historyPageSize+9+1)/2 {
		t.Fatalf("touches across the whole history: %d, %v", n, err)
	}

	// The company's page reads each list through the API, a page at a time and each on
	// its own cursor.
	base := fmt.Sprintf("%s/o/alpha/api/v1/companies/%d/", srv.URL, co.ID)
	history := apiPages(t, client, base+"history?limit="+fmt.Sprint(historyPageSize))
	if len(history) != 2 || len(history[0].Items) != historyPageSize || len(history[1].Items) != 10 {
		t.Fatalf("the company's history should come in two pages: %d", len(history))
	}
	if oldest, _ := time.Parse(time.RFC3339, fmt.Sprint(history[1].Items[9]["at"])); !oldest.Equal(events[0].At) {
		t.Fatalf("the last page of history should end on the oldest thing: %v", history[1].Items[9]["at"])
	}
	notePages := apiPages(t, client, base+"notes?limit="+fmt.Sprint(companyNotesPageSize))
	if len(notePages) != 2 || len(notePages[1].Items) != 3 {
		t.Fatalf("the company's notes should come in two pages: %d", len(notePages))
	}
	for _, n := range notePages[0].Items {
		if n["text"] == "Left note 0." {
			t.Fatal("the first page of notes shows one that belongs on the second")
		}
	}
	if notePages[1].Items[2]["text"] != "Left note 0." {
		t.Fatalf("the second page of notes doesn't end on the oldest: %v", notePages[1].Items[2]["text"])
	}
}

func TestAgentActivityHasPages(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	events := make([]Event, activityPageSize+4)
	for i := range events {
		events[i] = Event{OrgID: o.ID, User: "agent:scout", Action: "add:fact", Value: str(fmt.Sprint("fact number ", i, ".")),
			At: time.Date(2020, 1, 1, 0, i, 0, 0, time.UTC)}
	}
	if err := s.DB.CreateInBatches(events, 50).Error; err != nil {
		t.Fatal(err)
	}
	first, pg := s.AgentActivity(o.ID, 1)
	if len(first) != activityPageSize || pg.Pages() != 2 {
		t.Fatalf("the first page of activity: %d rows, %+v", len(first), pg)
	}
	if last, _ := s.AgentActivity(o.ID, 2); len(last) != 4 || !last[3].At.Equal(events[0].At) {
		t.Fatalf("the last page of activity: %d rows", len(last))
	}
	pages := apiPages(t, client, srv.URL+"/o/alpha/api/v1/agent-activity?limit="+fmt.Sprint(activityPageSize))
	if len(pages) != 2 || len(pages[0].Items) != activityPageSize || len(pages[1].Items) != 4 || pages[1].Items[3]["value"] != "fact number 0." {
		t.Fatalf("agent activity through the API should come in two pages, oldest last: %d", len(pages))
	}
}

// TestAgentActivityPagesThroughEventsAtTheSameMoment: a page is read from where the last
// one left off, so events that share a moment, as a batch an agent sends does, are
// each listed once, in order, however the pages fall between them. A person's own
// events aren't agents', and stay out.
func TestAgentActivityPagesThroughEventsAtTheSameMoment(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	at := time.Date(2020, 1, 1, 9, 0, 0, 0, time.UTC)
	var events []Event
	for i := range 7 {
		// Three moments, two or three events in each.
		events = append(events, Event{OrgID: o.ID, User: "agent:scout", Action: "add:fact", Value: str(fmt.Sprint("fact ", i)), At: at.Add(time.Duration(i/3) * time.Minute)})
	}
	events = append(events, Event{OrgID: o.ID, User: "Ada Lovelace", Action: "note", Value: str("by hand"), At: at})
	if err := s.DB.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	pages := apiPages(t, client, srv.URL+"/o/alpha/api/v1/agent-activity?limit=2")
	var seen []string
	for _, p := range pages {
		for _, item := range p.Items {
			seen = append(seen, fmt.Sprint(item["value"]))
		}
	}
	// Newest first, and within a moment the last added first.
	if want := "fact 6 fact 5 fact 4 fact 3 fact 2 fact 1 fact 0"; strings.Join(seen, " ") != want {
		t.Fatalf("listed %q, want %q", strings.Join(seen, " "), want)
	}
	if len(pages) != 4 {
		t.Fatalf("seven events two to a page are four pages, not %d", len(pages))
	}
}
