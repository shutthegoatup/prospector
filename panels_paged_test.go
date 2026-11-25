package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestACompanysHistoryAndNotesHavePages(t *testing.T) {
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
	if err := s.db.CreateInBatches(events, 50).Error; err != nil {
		t.Fatal(err)
	}
	notes := make([]TaskNote, companyNotesPageSize+3)
	for i := range notes {
		notes[i] = TaskNote{OrgID: o.ID, CompanyID: co.ID, Actor: "agent:scout", Kind: "next", Text: fmt.Sprint("Left note ", i, ".")}
	}
	if err := s.db.CreateInBatches(notes, 50).Error; err != nil {
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

	page := fmt.Sprintf("%s/o/alpha/companies/%d", srv.URL, co.ID)
	code, body := call(t, client, "GET", page, "", nil)
	if code != http.StatusOK || strings.Count(body, "Page 1 of 2") != 2 {
		t.Fatalf("the company's page should offer a second page of its notes and of its history: %d", code)
	}
	if !strings.Contains(body, "?history=2") || !strings.Contains(body, "?notes=2") || strings.Contains(body, "Left note 0.") {
		t.Fatal("the first page links to neither list's second page, or shows a note that belongs on it")
	}
	// Turning one list's page keeps the other where it was.
	_, body = call(t, client, "GET", page+"?history=2&notes=2", "", nil)
	if !strings.Contains(body, "Left note 0.") || strings.Count(body, "Page 2 of 2") != 2 {
		t.Fatal("the second page of each doesn't show its oldest rows")
	}
	// Going back a page of one keeps the other on its second.
	if !strings.Contains(body, `?history=2"`) || !strings.Contains(body, `?notes=2"`) {
		t.Fatal("going back a page of notes loses the page of history, or the other way about")
	}
}

func TestAgentActivityHasPages(t *testing.T) {
	s, o, srv, client := signedIn(t)
	events := make([]Event, activityPageSize+4)
	for i := range events {
		events[i] = Event{OrgID: o.ID, User: "agent:scout", Action: "add:fact", Value: str(fmt.Sprint("fact number ", i, ".")),
			At: time.Date(2020, 1, 1, 0, i, 0, 0, time.UTC)}
	}
	if err := s.db.CreateInBatches(events, 50).Error; err != nil {
		t.Fatal(err)
	}
	first, pg := s.AgentActivity(o.ID, 1)
	if len(first) != activityPageSize || pg.Pages() != 2 {
		t.Fatalf("the first page of activity: %d rows, %+v", len(first), pg)
	}
	if last, _ := s.AgentActivity(o.ID, 2); len(last) != 4 || !last[3].At.Equal(events[0].At) {
		t.Fatalf("the last page of activity: %d rows", len(last))
	}
	code, body := call(t, client, "GET", srv.URL+"/o/alpha/agents", "", nil)
	if code != http.StatusOK || !strings.Contains(body, "Page 1 of 2") || !strings.Contains(body, "agents?activity=2") {
		t.Fatalf("the Agents page doesn't offer the second page of activity: %d", code)
	}
}
