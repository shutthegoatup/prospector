package main

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm"
)

// queries counts the reads a store makes while do runs.
func queries(t *testing.T, s *Store, do func()) (n int) {
	t.Helper()
	name := "count:" + t.Name()
	if err := s.db.Callback().Query().Before("gorm:query").Register(name, func(*gorm.DB) { n++ }); err != nil {
		t.Fatal(err)
	}
	defer s.db.Callback().Query().Remove(name)
	do()
	return n
}

func TestTheTaskListTakesNoMoreQueriesForMoreTasks(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout, second := Actor{Name: "agent:scout", KeyID: 1}, Actor{Name: "agent:second", KeyID: 2}
	work := func(i int) {
		task := must(s.CreateTask(o.ID, "", fmt.Sprint("Find makers of garden rooms, part ", i), "Nia"))
		must(s.AgentTask(o.ID, scout, task.ID))
		co := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: fmt.Sprint("Larkmoor ", i), Task: task.ID}))
		must(s.AgentAddContact(o, scout, AddContactIn{Company: co.Name, Name: fmt.Sprintf("Ada V%cle", rune(0x61+i)),
			SourceURL: "https://larkmoor.example/team", Task: task.ID}))
		must(s.AgentNote(o.ID, second, NoteIn{Task: task.ID, Company: "Larkmoor 0", Kind: "skipped", Text: "No team page"}))
		if err := s.AskTask(o.ID, task.ID, "And their head of digital", "Nia"); err != nil {
			t.Fatal(err)
		}
	}
	work(0)
	now := time.Now()
	var one, many []TaskView
	forOne := queries(t, s, func() { one = must(firstOf(s.Tasks(o.ID, true, 1, now))) })
	for i := 1; i < 12; i++ {
		work(i)
	}
	forMany := queries(t, s, func() { many = must(firstOf(s.Tasks(o.ID, true, 1, now))) })
	if len(one) != 1 || len(many) != 12 {
		t.Fatalf("listed %d and %d tasks", len(one), len(many))
	}
	if forMany != forOne {
		t.Fatalf("a list of 12 tasks took %d queries where a list of one took %d", forMany, forOne)
	}

	// Each task in the list reads as it does by itself.
	for _, listed := range many {
		alone := must(s.Task(o.ID, listed.ID, now))
		if len(listed.Requests) != 2 || len(listed.Companies) != 1 || len(listed.People) != 1 || len(listed.Notes) != 1 || listed.Last == nil {
			t.Fatalf("task %d in the list is missing what was done for it: %+v", listed.ID, listed)
		}
		if listed.Notes[0].Company != "Larkmoor 0" {
			t.Fatalf("a note about a company the task didn't find lost its name: %+v", listed.Notes[0])
		}
		if !reflect.DeepEqual(listed, *alone) {
			t.Fatalf("task %d reads differently in the list:\nlisted %+v\nalone  %+v", listed.ID, listed, *alone)
		}
	}
}
