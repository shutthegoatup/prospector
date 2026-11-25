package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestTheListsInsideAnAnswerComeInPages(t *testing.T) {
	s, o, srv, _ := signedIn(t)
	scout := Actor{Name: "agent:scout", KeyID: 1}
	task := must(s.CreateTask(o.ID, "", "Find makers of garden rooms", "Nia"))
	co := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor", Task: task.ID}))
	for i := range 5 {
		must(s.AgentAddFact(o.ID, scout, AddFactIn{Company: co.Name, Task: task.ID,
			FactIn: FactIn{Field: "press", Value: fmt.Sprint("Award number ", i), SourceURL: "https://larkmoor.example/news"}}))
		must(s.AgentAddContact(o, scout, AddContactIn{Company: co.Name, Name: fmt.Sprintf("Quillon V%cle", rune(0x61+i)),
			SourceURL: "https://larkmoor.example/team", Task: task.ID}))
		must(s.AgentNote(o.ID, scout, NoteIn{Task: task.ID, Company: co.Name, Kind: "next", Text: fmt.Sprint("Note number ", i)}))
		must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: fmt.Sprint("Fenwick Rooms ", i), Task: task.ID}))
	}
	once := func(what string, seen map[int64]int, want int) {
		t.Helper()
		if len(seen) != want {
			t.Fatalf("%d %s were listed, want %d", len(seen), what, want)
		}
		for id, n := range seen {
			if n != 1 {
				t.Fatalf("%s %d was listed %d times", what, id, n)
			}
		}
	}

	// A company: every fact, person, line of history and note turns up once.
	facts, people, notes, history := map[int64]int{}, map[int64]int{}, map[int64]int{}, 0
	in, pages := PageIn{Limit: 2}, 0
	for {
		got := must(s.AgentCompany(o.ID, scout, co.Name, in))
		if len(got.Facts) > 2 || len(got.People) > 2 || len(got.History) > 2 || len(got.Notes) > 2 {
			t.Fatalf("a page of two held more: %d facts, %d people, %d history, %d notes", len(got.Facts), len(got.People), len(got.History), len(got.Notes))
		}
		for _, f := range got.Facts {
			facts[f.ID]++
		}
		for _, p := range got.People {
			people[p.ID]++
		}
		for _, n := range got.Notes {
			notes[n.ID]++
		}
		history += len(got.History)
		if pages++; got.NextCursor == "" || pages > 50 {
			break
		}
		in.Cursor = got.NextCursor
	}
	var events int64
	s.in(o.ID).Model(&Event{}).Where("company_id = ?", co.ID).Count(&events)
	once("facts", facts, 5)
	once("people", people, 5)
	once("notes", notes, 5)
	if history != int(events) || events < 12 || pages > 20 {
		t.Fatalf("walking a company two at a time: %d of %d lines of history in %d pages", history, events, pages)
	}
	// Asked for no particular size, the latest history and notes come as they did.
	usual := must(s.AgentCompany(o.ID, scout, co.Name, PageIn{}))
	if len(usual.Facts) != 5 || len(usual.People) != 5 || len(usual.Notes) != agentNotes || usual.NextCursor == "" {
		t.Fatalf("a company with no limit asked for: %d facts, %d people, %d notes, next %q", len(usual.Facts), len(usual.People), len(usual.Notes), usual.NextCursor)
	}
	if _, err := s.AgentCompany(o.ID, scout, co.Name, PageIn{Cursor: cursorOf(1, 2)}); err != errCursor {
		t.Fatalf("a cursor from another list: %v", err)
	}

	// A task: the companies and people found and the notes left, with the count of
	// all of them on every page.
	companies, found, left := map[int64]int{}, map[int64]int{}, map[int64]int{}
	in, pages = PageIn{Limit: 2}, 0
	for {
		got := must(s.AgentTask(o.ID, scout, task.ID, in))
		if got.Found.Companies != 6 || got.Found.People != 5 || len(got.Requests) != 1 || got.Brief == "" {
			t.Fatalf("a page of a task lost what isn't paged: %+v", got.Found)
		}
		for _, c := range got.Companies {
			companies[c.ID]++
		}
		for _, p := range got.People {
			found[p.ID]++
		}
		for _, n := range got.Notes {
			left[n.ID]++
		}
		if pages++; got.NextCursor == "" || pages > 50 {
			break
		}
		in.Cursor = got.NextCursor
	}
	once("companies", companies, 6)
	once("people", found, 5)
	once("notes", left, 5)
	if pages != 3 {
		t.Fatalf("a task with six companies, two a page, took %d pages", pages)
	}
	// More notes than the task's own page shows can all be reached.
	extra := make([]TaskNote, taskNotesShown+20)
	for i := range extra {
		extra[i] = TaskNote{OrgID: o.ID, TaskID: task.ID, Actor: "agent:scout", Kind: "next", Text: fmt.Sprint("Later note ", i)}
	}
	if err := s.db.CreateInBatches(extra, 60).Error; err != nil {
		t.Fatal(err)
	}
	left, in, pages = map[int64]int{}, PageIn{Limit: 50}, 0
	for {
		got := must(s.AgentTask(o.ID, scout, task.ID, in))
		for _, n := range got.Notes {
			left[n.ID]++
		}
		if pages++; got.NextCursor == "" || pages > 50 {
			break
		}
		in.Cursor = got.NextCursor
	}
	once("notes on a task with many", left, taskNotesShown+25)
	if err := s.db.Where("task_id = ? and text like ?", task.ID, "Later note%").Delete(&TaskNote{}).Error; err != nil {
		t.Fatal(err)
	}

	// Carrying on from a company's short first answer reads full pages.
	more := must(s.AgentCompany(o.ID, scout, co.Name, PageIn{Cursor: usual.NextCursor}))
	if len(more.Notes) != 5-agentNotes || more.NextCursor != "" {
		t.Fatalf("the rest of a company's notes after its first answer: %d notes, next %q", len(more.Notes), more.NextCursor)
	}

	if whole := must(s.AgentTask(o.ID, scout, task.ID, PageIn{})); len(whole.Companies) != 6 || whole.NextCursor != "" {
		t.Fatalf("a task that fits one page: %d companies, next %q", len(whole.Companies), whole.NextCursor)
	}

	key, _, err := s.CreateKey(o.ID, "bot", []string{"read"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"companies/Larkmoor?limit=2", fmt.Sprintf("tasks/%d?limit=2", task.ID)} {
		req, _ := http.NewRequest("GET", srv.URL+"/o/alpha/api/v1/"+path, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if link := res.Header.Get("Link"); res.StatusCode != http.StatusOK || !strings.HasSuffix(link, `>; rel="next"`) || !strings.Contains(link, "cursor=") {
			t.Errorf("%s: %d with Link %q", path, res.StatusCode, link)
		}
	}
}
