package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// The task reads moved into the spec answer exactly what the store says, which is what
// the MCP tools answer too: a field the spec lacks would be lost on the way through.
func TestTheSpecsTaskReadsSayEverythingTheStoreDoes(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	key := func(name string) (string, Actor) {
		secret, k, err := s.CreateKey(o.ID, name, []string{"read", "research:write"}, "Ada Lovelace")
		if err != nil {
			t.Fatal(err)
		}
		return secret, Actor{Name: "agent:" + name, KeyID: k.ID}
	}
	scoutKey, scout := key("scout")
	otherKey, _ := key("other")

	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders with a configurator.", "Ada Lovelace"))
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor Yachts", Task: task.ID,
		Facts: []FactIn{{Field: "configurator", Value: "Day cruiser builder", SourceURL: "https://larkmoor.example/build"}}}))
	must(s.AgentAddContact(o, scout, AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", Title: "Head of Marketing",
		SourceURL: "https://larkmoor.example/team", Task: task.ID}))
	must(s.AgentNote(o.ID, scout, NoteIn{Task: task.ID, Company: "Larkmoor Yachts", Kind: "next", Text: "Look at their dealer list."}))
	must(s.AskTask(o.ID, task.ID, "Now some in Dorset.", "Ada Lovelace"))
	finished := must(s.AskTask(o.ID, task.ID, "And one in Kent.", "Ada Lovelace"))
	if err := s.SetRequestDone(o.ID, finished.ID, true, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}

	one := fmt.Sprintf("%s/o/alpha/api/v1/tasks/%d", srv.URL, task.ID)
	// Another agent takes the first open request, so the scout sees it held by someone else.
	if code, body := asKey(t, "GET", one, otherKey, nil); code != http.StatusOK || !strings.Contains(body, `"claimed_request"`) {
		t.Fatalf("another agent reading the task: %d %s", code, body)
	}
	code, body := asKey(t, "GET", one, scoutKey, nil)
	if code != http.StatusOK {
		t.Fatalf("the scout reading the task: %d %s", code, body)
	}
	direct := must(s.AgentTask(o.ID, scout, task.ID, PageIn{}))
	sameJSON(t, "the task", body, direct)
	for _, want := range []string{`"held_by"`, `"held_until"`, `"yours":true`, `"done_by"`, `"added_since"`, `"by_you":true`, `"title":"Head of Marketing"`, `"last_agent":"agent:other"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the task should carry %s: %s", want, body)
		}
	}

	code, body = asKey(t, "GET", srv.URL+"/o/alpha/api/v1/tasks", scoutKey, nil)
	if code != http.StatusOK {
		t.Fatalf("the list: %d %s", code, body)
	}
	var list struct{ Items json.RawMessage }
	json.Unmarshal([]byte(body), &list)
	briefs, _, err := s.AgentTasks(o.ID, scout, PageIn{})
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, "the list", string(list.Items), briefs)

	// A person reading the task claims nothing, and isn't anyone's agent.
	var claims int64
	s.db.Model(&RequestClaim{}).Count(&claims)
	code, body = call(t, ada, "GET", one, "", nil)
	var after int64
	s.db.Model(&RequestClaim{}).Count(&after)
	if code != http.StatusOK || strings.Contains(body, `"claimed_request"`) || strings.Contains(body, `"yours"`) || after != claims {
		t.Fatalf("a person reading the task: %d, claims %d then %d: %s", code, claims, after, body)
	}
}

// A person adds notes and finishes requests through the same operations agents use, as
// sales work; a read-only key can do neither.
func TestPeopleInSalesLeaveNotesAndFinishRequests(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	grace := mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	asGrace := signInAs(t, srv, "grace@example.com", "correct horse battery")
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders with a configurator.", "Ada Lovelace"))
	first := must(s.AskTask(o.ID, task.ID, "Now some in Dorset.", "Ada Lovelace"))
	second := must(s.AskTask(o.ID, task.ID, "And one in Kent.", "Ada Lovelace"))
	base := srv.URL + "/o/alpha/api/v1/"
	note := map[string]any{"task": task.ID, "kind": "next", "text": "Look at their dealer list."}
	finish := func(id int64) map[string]any { return map[string]any{"request": id} }
	areas := func(to ...string) {
		t.Helper()
		if code, body := call(t, ada, "PATCH", srv.URL+"/o/alpha/api/v1/members/"+itoa(grace.ID), "application/json", map[string]any{"areas": append([]string{}, to...)}); code != http.StatusOK {
			t.Fatalf("set areas %v: %d %s", to, code, body)
		}
	}

	areas("support")
	for what, body := range map[string]any{"notes": note, "requests/finish": finish(first.ID)} {
		if code, out := call(t, asGrace, "POST", base+what, "application/json", body); code != http.StatusForbidden || !strings.Contains(out, "Team list") {
			t.Errorf("support-only %s: %d %s", what, code, out)
		}
	}

	areas("sales")
	code, out := call(t, asGrace, "POST", base+"notes", "application/json", note)
	if code != http.StatusOK || !strings.Contains(out, `"actor":"Grace Hopper"`) {
		t.Fatalf("a person in sales leaving a note: %d %s", code, out)
	}
	if code, out := call(t, asGrace, "POST", base+"requests/finish", "application/json", finish(first.ID)); code != http.StatusOK || !strings.Contains(out, `"ok":true`) {
		t.Fatalf("a person in sales finishing a request: %d %s", code, out)
	}
	if r := must(s.RequestByID(o.ID, first.ID)); r.DoneAt == nil || r.DoneBy != "Grace Hopper" {
		t.Fatalf("the request should be done, by Grace: %+v", r)
	}

	reader, _, err := s.CreateKey(o.ID, "reader", []string{"read"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	for what, body := range map[string]any{"notes": note, "requests/finish": finish(second.ID)} {
		if code, out := asKey(t, "POST", base+what, reader, body); code != http.StatusForbidden || !strings.Contains(out, "research:write") {
			t.Errorf("a read-only key on %s: %d %s", what, code, out)
		}
	}
}

// sameJSON fails unless body is what v marshals to, field for field.
func sameJSON(t *testing.T, what, body string, v any) {
	t.Helper()
	raw, _ := json.Marshal(v)
	var got, want any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("%s isn't JSON: %v", what, err)
	}
	json.Unmarshal(raw, &want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s through the spec differs from the store's:\n got %s\nwant %s", what, body, raw)
	}
}
