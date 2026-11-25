package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestATaskIsCarriedOnByAnotherAgent(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout, second := Actor{Name: "agent:scout", KeyID: 1}, Actor{Name: "agent:second", KeyID: 2}
	// Known before anyone wrote the task.
	old := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Ostrel Aviation", Domain: "ostrel.example"}))

	task, err := s.CreateTask(o.ID, "", "Yacht builders with an online configurator.\nAbout twenty.", "Ada Lovelace")
	if err != nil || task.Title != "Yacht builders with an online configurator." {
		t.Fatalf("create task: %+v %v", task, err)
	}
	if list := must(firstOf(s.AgentTasks(o.ID, scout, PageIn{}))); len(list) != 1 || list[0].Status != TaskWaiting || len(list[0].Open) != 1 || list[0].LastBy != "" {
		t.Fatalf("a new task should be waiting with its brief open: %+v", list)
	}
	first := must(s.AgentTask(o.ID, scout, task.ID, PageIn{}))
	if !strings.HasPrefix(first.Brief, "Yacht builders") || len(first.Companies) != 0 || len(first.Requests) != 1 {
		t.Fatalf("the brief as an agent first reads it: %+v", first)
	}
	if list := must(firstOf(s.AgentTasks(o.ID, second, PageIn{}))); list[0].Status != TaskWorking || list[0].LastBy != "agent:scout" || list[0].LastYou {
		t.Fatalf("picking a task up should show before anything is added: %+v", list[0])
	}

	fact := FactIn{Field: "configurator", Value: "Day Cruiser 45 builder", SourceURL: "https://larkmoor.example/build"}
	larkmoor := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor Yachts", Facts: []FactIn{fact}, Task: task.ID}))
	hannah := must(s.AgentAddContact(o, scout, AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", SourceURL: "https://larkmoor.example/team", Task: task.ID}))
	corvane := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Corvane"}))
	if _, err := s.AgentNote(o.ID, scout, NoteIn{Task: task.ID, Company: "Corvane", Kind: "skipped", Text: "Sells through dealers only, no configurator."}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCompanyStage(o.ID, larkmoor.ID, CompanyRejected, "", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if err := s.AskTask(o.ID, task.ID, "Find some in Switzerland too.", "Grace Hopper"); err != nil {
		t.Fatal(err)
	}

	got := must(s.AgentTask(o.ID, second, task.ID, PageIn{}))
	if len(got.Companies) != 1 || got.Companies[0].Name != "Larkmoor Yachts" || got.Companies[0].Stage != CompanyRejected {
		t.Fatalf("the next agent should see what was found, and that a person rejected it: %+v", got.Companies)
	}
	if len(got.People) != 1 || got.People[0].ID != hannah.ID || got.People[0].Company != "Larkmoor Yachts" {
		t.Fatalf("people found: %+v", got.People)
	}
	if len(got.Notes) != 1 || got.Notes[0].Company != "Corvane" || got.Notes[0].Kind != "skipped" || got.Notes[0].ByYou {
		t.Fatalf("the first agent's note: %+v", got.Notes)
	}
	if len(got.Open) != 2 || got.Open[1].Text != "Find some in Switzerland too." || got.Open[1].AskedBy != "Grace Hopper" ||
		got.Open[0].Since != (TaskCounts{1, 1}) || got.Open[1].Since != (TaskCounts{}) {
		t.Fatalf("open requests, and what each has gained: %+v", got.Open)
	}

	// The second agent covers the same ground, and one company that was already here.
	again := must(s.AgentAddCompany(o.ID, second, AddCompanyIn{Name: "larkmoor yachts", Facts: []FactIn{fact}, Task: task.ID}))
	same := must(s.AgentAddContact(o, second, AddContactIn{Company: "Larkmoor Yachts", Name: "hannah pryce", SourceURL: "https://larkmoor.example/about", Task: task.ID}))
	known := must(s.AgentAddCompany(o.ID, second, AddCompanyIn{Name: "Ostrel", Domain: "www.ostrel.example", Task: task.ID}))
	must(s.AgentAddCompany(o.ID, second, AddCompanyIn{Name: "Ostrel Aviation", Task: task.ID}))
	if !again.Existed || !same.Existed || same.ID != hannah.ID || !known.Existed || known.ID != old.ID {
		t.Fatalf("continuing a task made duplicates: %+v %+v %+v", again, same, known)
	}
	var companies, people, matches int64
	s.in(o.ID).Model(&Company{}).Count(&companies)
	s.in(o.ID).Model(&Contact{}).Count(&people)
	s.in(o.ID).Model(&Event{}).Where("action = ?", "task:match").Count(&matches)
	if companies != 3 || people != 1 || matches != 1 {
		t.Fatalf("want 3 companies, 1 person and the one new match recorded once, got %d, %d, %d", companies, people, matches)
	}
	if err := s.AgentFinish(o.ID, second, FinishIn{Request: got.Open[1].ID}); err != nil {
		t.Fatal(err)
	}
	after := must(s.AgentTask(o.ID, scout, task.ID, PageIn{}))
	if len(after.Companies) != 2 || after.Companies[0].ID != old.ID || after.Found != (TaskCounts{2, 1}) {
		t.Fatalf("a company known before the task should count once an agent matches it: %+v", after.Companies)
	}
	if len(after.Open) != 1 || !after.Requests[1].Done || after.Requests[1].DoneBy != "agent:second" || after.Requests[1].Since.Companies != 1 {
		t.Fatalf("the finished request: %+v", after.Requests)
	}
	if after.Status != TaskWorking || after.LastBy != "agent:second" || !after.Notes[0].ByYou {
		t.Fatalf("status and who was last: %+v", after.TaskBrief)
	}
	if corvane.ID == 0 || len(after.Companies) == 3 {
		t.Fatal("a company an agent only left a note about was counted as found")
	}

	view := must(s.Task(o.ID, task.ID, time.Now()))
	if len(view.Runs) != 2 || view.Runs[0].Actor != "agent:second" || view.Runs[1].Actor != "agent:scout" || view.Runs[1].People != 1 || view.Runs[1].Companies != 2 {
		t.Fatalf("each agent's stretch of work: %+v", view.Runs)
	}
	later := must(s.Task(o.ID, task.ID, time.Now().Add(time.Hour)))
	if later.Status != TaskWaiting {
		t.Fatalf("an hour after the last change, with a request open, the task should be waiting: %s", later.Status)
	}
	if err := s.SetRequestDone(o.ID, after.Open[0].ID, true, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if done := must(s.Task(o.ID, task.ID, time.Now())); done.Status != TaskDone {
		t.Fatalf("with nothing open the task is done: %s", done.Status)
	}
	if err := s.SetTaskClosed(o.ID, task.ID, true, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if list := must(firstOf(s.AgentTasks(o.ID, scout, PageIn{}))); len(list) != 0 {
		t.Fatalf("a closed task is still offered to agents: %+v", list)
	}
	if _, err := s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Brandtech", Task: task.ID}); err == nil {
		t.Fatal("work was accepted for a closed task")
	}
	if err := s.AskTask(o.ID, task.ID, "One more", "Ada Lovelace"); err == nil {
		t.Fatal("a request was added to a closed task")
	}
}

func TestTasksStayInTheirOrganisation(t *testing.T) {
	s := tempStore(t)
	a, b := mustOrg(t, s, "alpha"), mustOrg(t, s, "beta")
	scout := Actor{Name: "agent:scout", KeyID: 1}
	task := must(s.CreateTask(a.ID, "Yachts", "Yacht builders.", "Ada Lovelace"))
	if list := must(firstOf(s.AgentTasks(b.ID, scout, PageIn{}))); len(list) != 0 {
		t.Fatalf("another organisation's tasks are listed: %+v", list)
	}
	if _, err := s.AgentTask(b.ID, scout, task.ID, PageIn{}); err == nil {
		t.Fatal("another organisation's task can be read")
	}
	if _, err := s.AgentAddCompany(b.ID, scout, AddCompanyIn{Name: "Corvane", Task: task.ID}); err == nil {
		t.Fatal("work was filed against another organisation's task")
	}
	if _, err := s.AgentNote(b.ID, scout, NoteIn{Task: task.ID, Kind: "next", Text: "Try the trade register."}); err == nil {
		t.Fatal("a note was left on another organisation's task")
	}
	if err := s.AskTask(b.ID, task.ID, "More", "Grace Hopper"); err == nil {
		t.Fatal("a request was added to another organisation's task")
	}
	request := must(s.AgentTask(a.ID, scout, task.ID, PageIn{})).Open[0].ID
	if err := s.AgentFinish(b.ID, scout, FinishIn{Request: request}); err == nil {
		t.Fatal("another organisation's request was finished")
	}
	if err := s.SetTaskClosed(b.ID, task.ID, true, "Grace Hopper"); err == nil {
		t.Fatal("another organisation's task was closed")
	}
	note := must(s.AgentNote(a.ID, scout, NoteIn{Task: task.ID, Kind: "next", Text: "Try the trade register."}))
	if err := s.DeleteNote(b.ID, note.ID); err == nil {
		t.Fatal("another organisation's note was deleted")
	}
}

func TestNotesAreShortAndAboutSomething(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout, second := Actor{Name: "agent:scout", KeyID: 1}, Actor{Name: "agent:second", KeyID: 2}
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor Yachts"}))
	for why, in := range map[string]NoteIn{
		"about nothing":   {Kind: "next", Text: "Try the trade register."},
		"of no kind":      {Company: "Larkmoor Yachts", Kind: "fact", Text: "Founded in 1990."},
		"empty":           {Company: "Larkmoor Yachts", Kind: "next", Text: "  "},
		"too long":        {Company: "Larkmoor Yachts", Kind: "next", Text: strings.Repeat("é", maxNote+1)},
		"unknown company": {Company: "Nobody Ltd", Kind: "next", Text: "Try the trade register."},
	} {
		if _, err := s.AgentNote(o.ID, scout, in); err == nil {
			t.Fatalf("a note %s was accepted", why)
		}
	}
	for i := range 4 {
		must(s.AgentNote(o.ID, scout, NoteIn{Company: "Larkmoor Yachts", Kind: "dead_end", Text: "Nothing on page " + strconv.Itoa(i+1) + " of the press archive."}))
	}
	got := must(s.AgentCompany(o.ID, second, "Larkmoor Yachts", PageIn{}))
	if len(got.Notes) != 3 || !strings.Contains(got.Notes[0].Text, "page 4") || got.Notes[0].ByYou || len(got.Facts) != 0 {
		t.Fatalf("get_company should carry the latest three notes, apart from facts: %+v", got.Notes)
	}
	if h := got.History[0]; h.Action != "note:dead_end" || h.Value != "" {
		t.Fatalf("a note is in the history without its text: %+v", h)
	}
	if _, err := s.CreateTask(o.ID, "", strings.Repeat("x", maxBrief+1), "Ada Lovelace"); err == nil {
		t.Fatal("an overlong brief was accepted")
	}
}

func TestErasingAPersonTakesTheNotesAroundThem(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := Actor{Name: "agent:scout", KeyID: 1}
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders.", "Ada Lovelace"))
	other := must(s.CreateTask(o.ID, "Aviation", "Aircraft makers.", "Ada Lovelace"))
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor Yachts", Task: task.ID}))
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Ostrel Aviation", Task: other.ID}))
	hannah := must(s.AgentAddContact(o, scout, AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", SourceURL: "https://larkmoor.example/team", Task: task.ID}))
	for _, in := range []NoteIn{
		{Company: "Larkmoor Yachts", Kind: "next", Text: "Hannah Pryce may have moved to Corvane, check."},
		{Task: task.ID, Kind: "dead_end", Text: "Could not confirm H. Pryce's role anywhere current."},
		{Task: other.ID, Company: "Ostrel Aviation", Kind: "skipped", Text: "No configurator."},
	} {
		must(s.AgentNote(o.ID, scout, in))
	}
	if err := s.ErasePerson(o.ID, hannah.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	var left []TaskNote
	s.in(o.ID).Find(&left)
	if len(left) != 1 || left[0].Text != "No configurator." {
		t.Fatalf("notes about the erased person's company and its task should be gone, others kept: %+v", left)
	}
	for _, shown := range []any{must(s.AgentTask(o.ID, scout, task.ID, PageIn{})), must(s.AgentCompany(o.ID, scout, "Larkmoor Yachts", PageIn{})), must(s.ExportOrg(o.ID, "Ada Lovelace"))} {
		if b, _ := json.Marshal(shown); strings.Contains(string(b), "Pryce") {
			t.Fatalf("an erased person is still named: %s", b)
		}
	}
	if out := must(s.ExportOrg(o.ID, "Ada Lovelace")); len(out.Tasks) != 2 || len(out.TaskRequests) != 2 || len(out.TaskNotes) != 1 {
		t.Fatalf("the organisation's export should hold its tasks: %d %d %d", len(out.Tasks), len(out.TaskRequests), len(out.TaskNotes))
	}
}

func TestExistingHistorySurvivesTheTaskColumn(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := Actor{Name: "agent:scout", KeyID: 4}
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor Yachts"}))
	// A database from before tasks, with history already in it.
	if err := s.db.Migrator().DropColumn(&Event{}, "task_id"); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Migrator().DropTable(&Task{}, &TaskRequest{}, &TaskNote{}); err != nil {
		t.Fatal(err)
	}
	if err := s.db.AutoMigrate(models...); err != nil {
		t.Fatalf("adding tasks to a database with rows: %v", err)
	}
	var old Event
	if err := s.in(o.ID).First(&old).Error; err != nil || old.Action != "add:company" || old.KeyID != 4 || old.TaskID != 0 {
		t.Fatalf("an existing event after the upgrade: %+v %v", old, err)
	}
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders.", "Ada Lovelace"))
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor Yachts", Task: task.ID}))
	if got := must(s.AgentTask(o.ID, scout, task.ID, PageIn{})); len(got.Companies) != 1 {
		t.Fatalf("a company from before the upgrade should join a task: %+v", got.Companies)
	}
}

func TestRunsSplitOnAQuietHalfHour(t *testing.T) {
	at := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	one, two := int64(1), int64(2)
	event := func(mins int, user string, key int64, action string, company *int64, task int64) Event {
		return Event{At: at.Add(time.Duration(mins) * time.Minute), User: user, KeyID: key, Action: action, CompanyID: company, TaskID: task}
	}
	runs := runsOf([]Event{
		event(0, "agent:scout", 1, "task:pickup", nil, 7),
		event(5, "agent:scout", 1, "add:company", &one, 7),
		event(10, "Ada Lovelace", 0, "company:qualified", &one, 0),
		event(20, "agent:scout", 1, "add:contact", &one, 7),
		event(25, "agent:scout", 9, "add:company", &two, 0),
		event(49, "agent:scout", 1, "draft:email", &two, 0),
		event(79, "agent:scout", 1, "add:fact", &two, 0),
	})
	if len(runs) != 3 {
		t.Fatalf("want two runs for key 1 and one for its namesake, got %+v", runs)
	}
	latest, first, namesake := runs[0], runs[1], runs[2]
	if latest.KeyID != 1 || latest.Changes != 1 || !latest.Start.Equal(at.Add(79*time.Minute)) {
		t.Fatalf("thirty quiet minutes should start a new run: %+v", latest)
	}
	if namesake.KeyID != 9 || namesake.Changes != 1 {
		t.Fatalf("two keys with one name are two agents: %+v", namesake)
	}
	if first.Changes != 3 || first.Companies != 2 || first.People != 1 || first.Drafts != 1 || len(first.Tasks) != 1 || first.Tasks[0] != 7 ||
		!first.Start.Equal(at) || !first.End.Equal(at.Add(49*time.Minute)) {
		t.Fatalf("the first run: %+v", first)
	}
	if !latest.Going(at.Add(100*time.Minute)) || latest.Going(at.Add(110*time.Minute)) {
		t.Fatal("a run is going until half an hour after its last change")
	}
}

func TestTasksOverMCPAndThePages(t *testing.T) {
	s, o, srv, client := signedIn(t)
	key, _, _ := s.CreateKey(o.ID, "claude", []string{"read", "research:write"}, "test")
	reader, _, _ := s.CreateKey(o.ID, "reader", []string{"read"}, "test")
	connect := func(secret string) *mcp.ClientSession {
		t.Helper()
		transport := &mcp.StreamableClientTransport{Endpoint: srv.URL + "/o/alpha/mcp", HTTPClient: &http.Client{
			Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				r.Header.Set("Authorization", "Bearer "+secret)
				return http.DefaultTransport.RoundTrip(r)
			})}}
		session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), transport, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { session.Close() })
		return session
	}
	ctx := context.Background()

	code, body := call(t, client, "POST", srv.URL+"/o/alpha/ui/task", "application/json", map[string]any{"title": "", "brief": "Yacht builders with a <b>configurator</b>."})
	if code != http.StatusOK || !strings.Contains(body, `"go":"/o/alpha/tasks/1"`) {
		t.Fatalf("create a task from the page: %d %s", code, body)
	}
	session := connect(key)
	tool := func(name string, args map[string]any) string {
		t.Helper()
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("%s: %v %+v", name, err, res)
		}
		return fmt.Sprint(res.StructuredContent)
	}
	if list := tool("list_tasks", map[string]any{}); !strings.Contains(list, "status:waiting") || !strings.Contains(list, "Yacht builders") {
		t.Fatalf("list_tasks: %s", list)
	}
	prompt, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: "work_task", Arguments: map[string]string{"id": "1"}})
	if err != nil || len(prompt.Messages) != 1 || !strings.Contains(prompt.Messages[0].Content.(*mcp.TextContent).Text, "get_task with id 1") {
		t.Fatalf("the work_task prompt: %v %+v", err, prompt)
	}
	if got := tool("get_task", map[string]any{"id": 1}); !strings.Contains(got, "<b>configurator</b>") {
		t.Fatalf("get_task: %s", got)
	}
	tool("add_company", map[string]any{"name": "Larkmoor Yachts", "task": 1,
		"facts": []map[string]string{{"field": "configurator", "value": "Day Cruiser 45 builder", "source_url": "https://larkmoor.example/build"}}})
	tool("task_note", map[string]any{"task": 1, "company": "Larkmoor Yachts", "kind": "next", "text": "Look for <i>who</i> runs marketing."})
	if res, _ := session.CallTool(ctx, &mcp.CallToolParams{Name: "add_company", Arguments: map[string]any{"name": "Corvane", "task": 99}}); res == nil || !res.IsError {
		t.Fatal("work for a task that doesn't exist should be a tool error")
	}
	reading := connect(reader)
	tools, err := reading.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := reading.CallTool(ctx, &mcp.CallToolParams{Name: "get_task", Arguments: map[string]any{"id": 1}}); err != nil || res.IsError {
		t.Fatalf("a read-only key reading a task: %v %+v", err, res)
	}
	var byReader int64
	if s.in(o.ID).Model(&Event{}).Where(`"user" = ?`, "agent:reader").Count(&byReader); byReader != 0 {
		t.Fatalf("a read-only key reading a task was recorded as picking it up: %d", byReader)
	}
	var names []string
	for _, tl := range tools.Tools {
		names = append(names, tl.Name)
	}
	if all := " " + strings.Join(names, " ") + " "; !strings.Contains(all, " get_task ") || strings.Contains(all, " task_note ") || strings.Contains(all, " finish_request ") {
		t.Fatalf("a read-only key should read tasks and not write to them: %v", names)
	}

	_, list := call(t, client, "GET", srv.URL+"/o/alpha/tasks", "", nil)
	if !strings.Contains(list, "Being worked on") || !strings.Contains(list, "1 company, 0 people") || !strings.Contains(list, `href="/o/alpha/tasks/1"`) {
		t.Fatalf("the Tasks page should show the task, its status and what was found: %s", list)
	}
	_, page := call(t, client, "GET", srv.URL+"/o/alpha/tasks/1", "", nil)
	for _, want := range []string{"&lt;b&gt;configurator&lt;/b&gt;", "Larkmoor Yachts", "Look for &lt;i&gt;who&lt;/i&gt; runs marketing.", "agent · claude", "Copy prompt", "Ask for more"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the task page is missing %q", want)
		}
	}
	if code, _ := call(t, client, "POST", srv.URL+"/o/alpha/ui/task/ask", "application/json", map[string]any{"task": 1, "text": "Now some in Switzerland."}); code != http.StatusOK {
		t.Fatalf("ask for more: %d", code)
	}
	if got := tool("get_task", map[string]any{"id": 1}); !strings.Contains(got, "Now some in Switzerland.") || !strings.Contains(got, "asked_by:Ada Lovelace") {
		t.Fatalf("a request made on the page should reach the agent: %s", got)
	}
	tool("finish_request", map[string]any{"request": 2})
	if _, page = call(t, client, "GET", srv.URL+"/o/alpha/tasks/1", "", nil); !strings.Contains(page, "done, says agent · claude") {
		t.Fatal("the task page should say which agent finished the request")
	}
	if code, _ := call(t, client, "POST", srv.URL+"/o/alpha/ui/task/request/reopen", "application/json", map[string]any{"id": 2}); code != http.StatusOK {
		t.Fatalf("reopen a request: %d", code)
	}
	if code, _ := call(t, client, "POST", srv.URL+"/o/alpha/ui/note/delete", "application/json", map[string]any{"id": 1}); code != http.StatusOK {
		t.Fatalf("delete a note: %d", code)
	}
	if _, page = call(t, client, "GET", srv.URL+"/o/alpha/tasks/1", "", nil); strings.Contains(page, "runs marketing") || strings.Contains(page, "done, says") {
		t.Fatal("a deleted note or a reopened request still shows as before")
	}
	_, agents := call(t, client, "GET", srv.URL+"/o/alpha/agents", "", nil)
	if !strings.Contains(agents, "Recent agent work") || !strings.Contains(agents, `href="/o/alpha/tasks/1"`) {
		t.Fatal("the Agents page should show the run and the task it was for")
	}
	if code, _ := call(t, client, "POST", srv.URL+"/o/alpha/ui/task/close", "application/json", map[string]any{"id": 1}); code != http.StatusOK {
		t.Fatalf("close the task: %d", code)
	}
	if _, list = call(t, client, "GET", srv.URL+"/o/alpha/tasks", "", nil); strings.Contains(list, `href="/o/alpha/tasks/1"`) {
		t.Fatal("a closed task is still in the open list")
	}
	if code, _ := call(t, client, "GET", srv.URL+"/o/alpha/tasks/404", "", nil); code != http.StatusNotFound {
		t.Fatalf("a task that isn't there: %d", code)
	}
}
