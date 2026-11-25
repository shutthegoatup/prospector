package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAKeyThatCanOnlyReadDoesNotPickATaskUp(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders.", "Ada Lovelace"))
	reader := Actor{Name: "agent:reader", KeyID: 1, ReadOnly: true}
	if got := must(s.AgentTask(o.ID, reader, task.ID)); got.Brief != "Yacht builders." {
		t.Fatalf("a read-only key should still read the task: %+v", got)
	}
	var events int64
	s.in(o.ID).Model(&Event{}).Where(`"user" = ?`, reader.Name).Count(&events)
	if list := must(firstOf(s.AgentTasks(o.ID, reader, PageIn{}))); events != 0 || list[0].Status != TaskWaiting {
		t.Fatalf("a key that can add nothing made the task look worked on: %d events, %s", events, list[0].Status)
	}
}

func TestOpenTasksAreListedBeforeClosedOnes(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	older := must(s.CreateTask(o.ID, "Yachts", "Yacht builders.", "Ada Lovelace"))
	newer := must(s.CreateTask(o.ID, "Kitchens", "Kitchen makers.", "Ada Lovelace"))
	if err := s.SetTaskClosed(o.ID, newer.ID, true, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	all := must(firstOf(s.Tasks(o.ID, true, 1, time.Now())))
	if len(all) != 2 || all[0].ID != older.ID || all[1].ID != newer.ID {
		t.Fatalf("the open task should come first: %+v", all)
	}
}

func TestPickingUpATaskIsRecordedOnceARun(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout, second := Actor{Name: "agent:scout", KeyID: 1}, Actor{Name: "agent:second", KeyID: 2}
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders.", "Ada Lovelace"))
	for range 3 {
		must(s.AgentTask(o.ID, scout, task.ID))
		must(s.AgentTask(o.ID, second, task.ID))
	}
	var pickups int64
	s.in(o.ID).Model(&Event{}).Where("action = ?", "task:pickup").Count(&pickups)
	if pickups != 2 {
		t.Fatalf("two agents reading a task in turn should each be recorded once, got %d", pickups)
	}
	if err := s.SetTaskClosed(o.ID, task.ID, true, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if got := must(s.AgentTask(o.ID, Actor{Name: "agent:third", KeyID: 3}, task.ID)); got.Status != TaskClosed {
		t.Fatalf("a closed task can still be read, as closed: %s", got.Status)
	}
	if s.in(o.ID).Model(&Event{}).Where("action = ?", "task:pickup").Count(&pickups); pickups != 2 {
		t.Fatal("reading a closed task was recorded as picking it up")
	}
	if err := s.SetTaskClosed(o.ID, task.ID, false, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if list := must(firstOf(s.AgentTasks(o.ID, scout, PageIn{}))); len(list) != 1 || list[0].Status != TaskWorking {
		t.Fatalf("a reopened task is offered to agents again: %+v", list)
	}
}

func TestACompanyNotedOnATaskCanStillBeFoundForIt(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := Actor{Name: "agent:scout", KeyID: 1}
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor Yachts"}))
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders.", "Ada Lovelace"))
	must(s.AgentNote(o.ID, scout, NoteIn{Task: task.ID, Company: "Larkmoor Yachts", Kind: "skipped", Text: "Looked, unsure."}))
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor Yachts", Task: task.ID}))
	if found := must(s.AgentTask(o.ID, scout, task.ID)).Companies; len(found) != 1 {
		t.Fatalf("a note on the company kept it out of the task's results: %+v", found)
	}
}

func TestErasingSomeoneWithNoCompanyTakesTheirTasksNotes(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := Actor{Name: "agent:scout", KeyID: 1}
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders.", "Ada Lovelace"))
	hannah := must(s.QuickAddPerson(o, QuickAdd{Linkedin: "https://www.linkedin.com/in/hannah-pryce", Name: "Hannah Pryce"}, "Ada Lovelace"))
	if hannah.CompanyID != nil {
		t.Fatal("this test needs a person filed under no company")
	}
	if err := s.AgentDraft(o.ID, scout, DraftIn{ContactID: hannah.ID, Kind: "email", Text: "Hannah, about the builder.", Task: task.ID}); err != nil {
		t.Fatal(err)
	}
	must(s.AgentNote(o.ID, scout, NoteIn{Task: task.ID, Kind: "next", Text: "Hannah Pryce hasn't answered; try her colleague."}))
	if err := s.ErasePerson(o.ID, hannah.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(must(s.AgentTask(o.ID, scout, task.ID))); strings.Contains(string(b), "Pryce") {
		t.Fatalf("an erased person is still named on a task they were part of: %s", b)
	}
}
