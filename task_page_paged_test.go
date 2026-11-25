package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestATasksPageHasPagesOfEachList(t *testing.T) {
	s, o, srv, client := signedIn(t)
	task := must(s.CreateTask(o.ID, "", "Find makers of garden rooms", "Nia"))
	co := must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, AddCompanyIn{Name: "Larkmoor"}))
	notes := make([]TaskNote, taskNotesPageSize+5)
	for i := range notes {
		notes[i] = TaskNote{OrgID: o.ID, TaskID: task.ID, CompanyID: co.ID, Actor: "agent:scout", Kind: "next", Text: fmt.Sprint("Left note ", i, ".")}
	}
	if err := s.db.CreateInBatches(notes, 50).Error; err != nil {
		t.Fatal(err)
	}
	runs := []AgentRun{{OrgID: o.ID, TaskID: task.ID, AgentName: "Still going", Status: RunWorking, StartedBy: "Nia", StartedAt: time.Now()}}
	for i := range taskRunsPageSize + 3 {
		runs = append(runs, AgentRun{OrgID: o.ID, TaskID: task.ID, AgentName: fmt.Sprint("Ended run ", i, "."), Status: "done", StartedBy: "Nia", StartedAt: time.Now()})
	}
	if err := s.db.Create(&runs).Error; err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-100 * time.Hour)
	var events []Event
	for i := range taskRunsPageSize + 4 {
		events = append(events, Event{OrgID: o.ID, TaskID: task.ID, User: fmt.Sprintf("agent:worker%d", i), KeyID: int64(i + 1), Action: "add:fact",
			At: start.Add(time.Duration(i) * time.Hour)})
	}
	if err := s.db.CreateInBatches(events, 50).Error; err != nil {
		t.Fatal(err)
	}

	page, pg, err := s.TaskNotesPage(o.ID, task.ID, 2)
	if err != nil || len(page) != 5 || pg.Pages() != 2 || page[4].Text != "Left note 0." || page[0].Company != "Larkmoor" {
		t.Fatalf("the second page of a task's notes: %d notes, %+v, %v", len(page), pg, err)
	}

	url := fmt.Sprintf("%s/o/alpha/tasks/%d", srv.URL, task.ID)
	code, body := call(t, client, "GET", url, "", nil)
	if code != http.StatusOK || strings.Count(body, "Page 1 of 2") != 3 {
		t.Fatalf("the task's page should offer a second page of its notes, its runs and its work: %d, %d pagers", code, strings.Count(body, "Page 1 of 2"))
	}
	// The run still going is shown whatever page of ended ones is, with what stops it.
	if !strings.Contains(body, "Stop Still going") || strings.Contains(body, "Left note 0.") || strings.Contains(body, "Ended run 0.") {
		t.Fatal("the first page shows what belongs on the second, or leaves out the run still going")
	}
	_, body = call(t, client, "GET", url+"?notes=2&runs=2&work=2", "", nil)
	if !strings.Contains(body, "Left note 0.") || !strings.Contains(body, "Ended run 0.") || !strings.Contains(body, "Stop Still going") || strings.Count(body, "Page 2 of 2") != 3 {
		t.Fatal("the second page of each doesn't show its oldest, or loses the run still going")
	}
	// Each pager keeps the others where they are.
	if !strings.Contains(body, "?runs=2&amp;work=2") || !strings.Contains(body, "?notes=2&amp;work=2") || !strings.Contains(body, "?notes=2&amp;runs=2\"") {
		t.Fatal("going back a page of one list loses the page of the others")
	}
}
