package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/store"
)

// The tasks page lists closed tasks after the open ones when asked, and counts both, as
// the server-rendered page did.
func TestTheTaskListTakesClosedTasksAfterTheOpenOnes(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	var ids []int64
	for _, title := range []string{"Yachts", "Kitchens", "Bikes", "Tents"} {
		ids = append(ids, must(s.CreateTask(o.ID, title, title+" makers with showrooms.", "Ada Lovelace")).ID)
	}
	for _, id := range []int64{ids[0], ids[2]} {
		if err := s.SetTaskClosed(o.ID, id, true, "Ada Lovelace"); err != nil {
			t.Fatal(err)
		}
	}
	scoutKey, k, err := s.CreateKey(o.ID, "scout", []string{"read", "research:write"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	must(s.AgentTask(o.ID, store.Actor{Name: "agent:scout", KeyID: k.ID}, ids[3], paging.Page{}))

	type list struct {
		Items []struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
			Did    string `json:"last_agent_action"`
		}
		NextCursor string `json:"next_cursor"`
		Open, All  int
	}
	read := func(query string) list {
		t.Helper()
		code, body := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/tasks"+query, "", nil)
		if code != http.StatusOK {
			t.Fatalf("tasks%s: %d %s", query, code, body)
		}
		var l list
		json.Unmarshal([]byte(body), &l)
		return l
	}

	open := read("")
	if len(open.Items) != 2 || open.Open != 2 || open.All != 4 || open.Items[0].ID != ids[3] || open.Items[0].Did != "task:pickup" {
		t.Fatalf("the open tasks: %+v", open)
	}
	var got []int64
	var statuses []string
	for cursor, pages := "", 0; ; pages++ {
		if pages > 4 {
			t.Fatal("the pages of tasks never end")
		}
		page := read("?closed=true&limit=1&cursor=" + cursor)
		for _, it := range page.Items {
			got, statuses = append(got, it.ID), append(statuses, it.Status)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	want := []int64{ids[3], ids[1], ids[2], ids[0]}
	if fmt.Sprint(got) != fmt.Sprint(want) || statuses[2] != research.TaskClosed || statuses[3] != research.TaskClosed {
		t.Fatalf("with closed, a page at a time: %v %v, want %v", got, statuses, want)
	}
	views, _, err := s.Tasks(o.ID, true, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range views {
		if v.ID != want[i] {
			t.Fatalf("the API's order differs from the page's at %d: %d, want %d", i, want[i], v.ID)
		}
	}

	if code, body := asKey(t, "GET", srv.URL+"/o/alpha/api/v1/tasks?closed=true&cursor="+open.NextCursor+"x", scoutKey, nil); code != http.StatusBadRequest {
		t.Errorf("a cursor this list didn't give out: %d %s", code, body)
	}
}

// A task's page reads its prompt, a page of its notes with the company each is about,
// and the stretches of work agents did on it, all through the API.
func TestATasksPageReadsItsPromptNotesAndWorkThroughTheAPI(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	_, k, err := s.CreateKey(o.ID, "scout", []string{"read", "research:write"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	scout := store.Actor{Name: "agent:scout", KeyID: k.ID}
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders with a configurator.", "Ada Lovelace"))
	must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Larkmoor Yachts", Task: task.ID,
		Facts: []research.FactIn{{Field: "configurator", Value: "Day cruiser builder", SourceURL: "https://larkmoor.example/build"}}}))
	first := must(s.AgentNote(o.ID, scout, research.NoteIn{Task: task.ID, Kind: "dead_end", Text: "No listing of dealers anywhere."}))
	about := must(s.AgentNote(o.ID, scout, research.NoteIn{Task: task.ID, Company: "Larkmoor Yachts", Kind: "next", Text: "Look at their dealer list."}))
	base := fmt.Sprintf("%s/o/alpha/api/v1/tasks/%d/", srv.URL, task.ID)

	code, body := call(t, ada, "GET", base+"prompt", "", nil)
	var prompt struct{ Prompt string }
	json.Unmarshal([]byte(body), &prompt)
	if code != http.StatusOK || prompt.Prompt != research.TaskPrompt(o, *task) {
		t.Fatalf("the prompt: %d %s", code, body)
	}

	code, body = call(t, ada, "GET", base+"notes?limit=1", "", nil)
	var notes struct {
		Items []struct {
			ID        int64  `json:"id"`
			Company   string `json:"company"`
			CompanyID int64  `json:"company_id"`
		}
		NextCursor string `json:"next_cursor"`
	}
	json.Unmarshal([]byte(body), &notes)
	if code != http.StatusOK || len(notes.Items) != 1 || notes.Items[0].ID != about.ID || notes.Items[0].Company != "Larkmoor Yachts" ||
		notes.Items[0].CompanyID != about.CompanyID || notes.NextCursor == "" {
		t.Fatalf("the newest note, with its company: %d %s", code, body)
	}
	_, body = call(t, ada, "GET", base+"notes?limit=1&cursor="+notes.NextCursor, "", nil)
	notes.Items, notes.NextCursor = nil, ""
	json.Unmarshal([]byte(body), &notes)
	if len(notes.Items) != 1 || notes.Items[0].ID != first.ID || notes.Items[0].CompanyID != 0 || notes.NextCursor != "" {
		t.Fatalf("the older note: %s", body)
	}
	// The task's own read names the company of each note too.
	if _, body := call(t, ada, "GET", base[:len(base)-1], "", nil); !strings.Contains(body, fmt.Sprintf(`"company_id":%d`, about.CompanyID)) {
		t.Errorf("the task's notes should say which company each is about: %s", body)
	}

	code, body = call(t, ada, "GET", base+"work", "", nil)
	var work struct {
		Items []struct {
			Actor     string
			Going     bool
			Changes   int
			Companies int
		}
	}
	json.Unmarshal([]byte(body), &work)
	if code != http.StatusOK || len(work.Items) != 1 || work.Items[0].Actor != "agent:scout" || !work.Items[0].Going || work.Items[0].Companies != 1 {
		t.Fatalf("the work so far: %d %s", code, body)
	}

	// Another organisation's task has none of these here.
	beta := mustOrg(t, s, "beta")
	theirs := must(s.CreateTask(beta.ID, "Theirs", "Not yours.", "someone"))
	for _, p := range []string{"prompt", "notes", "work"} {
		if code, body := call(t, ada, "GET", fmt.Sprintf("%s/o/alpha/api/v1/tasks/%d/%s", srv.URL, theirs.ID, p), "", nil); code != http.StatusNotFound {
			t.Errorf("another organisation's task's %s: %d %s", p, code, body)
		}
	}
}

// Deleting an agent's note is sales work, and a key needs tasks:write for it: an agent
// that can only add research can't clear the notes others left.
func TestDeletingANoteIsSalesWorkThroughTheAPI(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	grace := mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	asGrace := signInAs(t, srv, "grace@example.com", "correct horse battery")
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders with a configurator.", "Ada Lovelace"))
	note := must(s.AgentNote(o.ID, store.Actor{Name: "Ada Lovelace"}, research.NoteIn{Task: task.ID, Kind: "next", Text: "Look at their dealer list."}))
	one := fmt.Sprintf("%s/o/alpha/api/v1/notes/%d", srv.URL, note.ID)
	areas := func(to ...string) {
		t.Helper()
		if code, body := call(t, ada, "PATCH", srv.URL+"/o/alpha/api/v1/members/"+itoa(grace.ID), "application/json", map[string]any{"areas": append([]string{}, to...)}); code != http.StatusOK {
			t.Fatalf("set areas %v: %d %s", to, code, body)
		}
	}

	areas("support")
	if code, body := call(t, asGrace, "DELETE", one, "", nil); code != http.StatusForbidden || !strings.Contains(body, "Team list") {
		t.Errorf("support-only deleting a note: %d %s", code, body)
	}
	agent, _, err := s.CreateKey(o.ID, "scout", []string{"read", "research:write"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	if code, body := asKey(t, "DELETE", one, agent, nil); code != http.StatusForbidden || !strings.Contains(body, "tasks:write") {
		t.Errorf("a research key deleting a note: %d %s", code, body)
	}
	beta := mustOrg(t, s, "beta")
	theirTask := must(s.CreateTask(beta.ID, "Theirs", "Not yours.", "someone"))
	theirs := must(s.AgentNote(beta.ID, store.Actor{Name: "someone"}, research.NoteIn{Task: theirTask.ID, Kind: "next", Text: "Theirs."}))
	if code, _ := call(t, ada, "DELETE", fmt.Sprintf("%s/o/alpha/api/v1/notes/%d", srv.URL, theirs.ID), "", nil); code != http.StatusNotFound {
		t.Errorf("another organisation's note: %d", code)
	}

	areas("sales")
	if code, body := call(t, asGrace, "DELETE", one, "", nil); code != http.StatusNoContent {
		t.Fatalf("a person in sales deleting a note: %d %s", code, body)
	}
	if n, _, _ := s.TaskNotesPage(o.ID, task.ID, 1); len(n) != 0 {
		t.Fatalf("the note should be gone: %+v", n)
	}
	if code, _ := call(t, asGrace, "DELETE", one, "", nil); code != http.StatusNotFound {
		t.Errorf("deleting it again: %d", code)
	}
	if n, _, _ := s.TaskNotesPage(beta.ID, theirTask.ID, 1); len(n) != 1 {
		t.Fatal("another organisation's note went too")
	}
}
