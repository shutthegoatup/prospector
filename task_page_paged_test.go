package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/internal/research"
)

func TestATasksPageHasPagesOfEachList(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	task := must(s.CreateTask(o.ID, "", "Find makers of garden rooms", "Nia"))
	co := must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, research.AddCompanyIn{Name: "Larkmoor"}))
	notes := make([]TaskNote, research.TaskNotesPageSize+5)
	for i := range notes {
		notes[i] = TaskNote{OrgID: o.ID, TaskID: task.ID, CompanyID: co.ID, Actor: "agent:scout", Kind: "next", Text: fmt.Sprint("Left note ", i, ".")}
	}
	if err := s.DB.CreateInBatches(notes, 50).Error; err != nil {
		t.Fatal(err)
	}
	runs := []AgentRun{{OrgID: o.ID, TaskID: task.ID, AgentName: "Still going", Status: RunWorking, StartedBy: "Nia", StartedAt: time.Now()}}
	for i := range taskRunsPageSize + 3 {
		runs = append(runs, AgentRun{OrgID: o.ID, TaskID: task.ID, AgentName: fmt.Sprint("Ended run ", i, "."), Status: "done", StartedBy: "Nia", StartedAt: time.Now()})
	}
	if err := s.DB.Create(&runs).Error; err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-100 * time.Hour)
	var events []Event
	for i := range taskRunsPageSize + 4 {
		events = append(events, Event{OrgID: o.ID, TaskID: task.ID, User: fmt.Sprintf("agent:worker%d", i), KeyID: int64(i + 1), Action: "add:fact",
			At: start.Add(time.Duration(i) * time.Hour)})
	}
	if err := s.DB.CreateInBatches(events, 50).Error; err != nil {
		t.Fatal(err)
	}

	page, pg, err := s.TaskNotesPage(o.ID, task.ID, 2)
	if err != nil || len(page) != 5 || pg.Pages() != 2 || page[4].Text != "Left note 0." || page[0].Company != "Larkmoor" {
		t.Fatalf("the second page of a task's notes: %d notes, %+v, %v", len(page), pg, err)
	}

	// The task's page reads each list through the API, a page at a time and each on its
	// own cursor.
	base := fmt.Sprintf("%s/o/alpha/api/v1/tasks/%d/", srv.URL, task.ID)
	notePages := apiPages(t, client, base+"notes?limit="+fmt.Sprint(research.TaskNotesPageSize))
	if len(notePages) != 2 || len(notePages[1].Items) != 5 || notePages[1].Items[4]["text"] != "Left note 0." || notePages[0].Items[0]["company"] != "Larkmoor" {
		t.Fatalf("the task's notes should come in two pages, oldest last: %d", len(notePages))
	}
	// The run still going comes first, whatever page of ended ones follows.
	runPages := apiPages(t, client, fmt.Sprintf("%s/o/alpha/api/v1/tasks/%d/runs?limit=%d", srv.URL, task.ID, taskRunsPageSize))
	if len(runPages) != 2 || runPages[0].Items[0]["agent_name"] != "Still going" {
		t.Fatalf("the task's runs should come in two pages, the one going first: %d", len(runPages))
	}
	var ended []any
	for _, pg := range runPages {
		for _, r := range pg.Items {
			ended = append(ended, r["agent_name"])
		}
	}
	if len(ended) != taskRunsPageSize+4 || ended[len(ended)-1] != "Ended run 0." {
		t.Fatalf("every run, once each: %v", ended)
	}
	workPages := apiPages(t, client, base+"work?limit="+fmt.Sprint(taskRunsPageSize))
	if len(workPages) != 2 || len(workPages[0].Items)+len(workPages[1].Items) != taskRunsPageSize+4 || workPages[1].Items[len(workPages[1].Items)-1]["actor"] != "agent:worker0" {
		t.Fatalf("the task's work should come in two pages, the oldest last: %d", len(workPages))
	}
}
