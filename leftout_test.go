package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/sales"
)

// A search that comes to companies already here says so, by how each stood when it
// came to it, and leaves them out of what it found new.
func TestASearchLeavesOutWhatWasAlreadyHere(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	scout := Actor{Name: "agent:scout", KeyID: 1}
	today := time.Now().Format(dayLayout)

	rejected := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Corvane Marine", Domain: "corvane.example"}))
	if err := s.SetCompanyStage(o.ID, rejected.ID, CompanyRejected, "", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	customer := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Belair Kitchens", Domain: "belair.example"}))
	if err := s.SetCompanyStage(o.ID, customer.ID, CompanyQualified, "", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	must(s.AddSubscription(o.ID, sales.SubscriptionIn{Company: customer.ID, Name: "Support", Amount: "1200", Per: PerYear, Start: today, TermMonths: 12, Then: sales.ThenRenews}, "Ada Lovelace"))
	talking := must(s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	if err := s.SetCompanyStage(o.ID, *talking.CompanyID, CompanyQualified, "", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if err := s.Mark(o.ID, talking.ID, "email", today, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	waiting := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Halvard Marine", Domain: "halvard.example"}))

	code, body := call(t, ada, "POST", srv.URL+"/o/alpha/api/v1/tasks", "application/json", map[string]any{"title": "", "brief": "Boat makers.", "want": 5})
	var task api.TaskStarted
	if code != http.StatusCreated || json.Unmarshal([]byte(body), &task) != nil {
		t.Fatalf("starting the search: %d %s", code, body)
	}
	scout.Task = task.Id
	for _, name := range []string{"Corvane Marine", "Belair Kitchens", "Larkmoor Yachts", "Halvard Marine"} {
		must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: name, Task: task.Id}))
	}
	// Found again: it doesn't count twice.
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Corvane Marine", Task: task.Id, Facts: []FactIn{{Field: "size", Value: "40 staff", SourceURL: "https://corvane.example/about"}}}))
	// New to the search, rejected during it: not something that was here before.
	fresh := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Dunmere Tourers", Domain: "dunmere.example", Task: task.Id}))
	if err := s.SetCompanyStage(o.ID, fresh.ID, CompanyRejected, "", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	// Waiting for review when found, then qualified: the search moved it on, it wasn't held.
	if err := s.SetCompanyStage(o.ID, waiting.ID, CompanyQualified, "", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}

	left := leftOutOf(t, ada, srv.URL, task.Id)
	if left.Body.Rejected != 1 || left.Body.Customer != 1 || left.Body.OpenDeal != 1 || left.Body.Qualified != 0 || len(left.Body.Items) != 3 {
		t.Fatalf("left out should be Corvane rejected, Belair a customer, Larkmoor talking: %+v", left)
	}
	for _, c := range left.Body.Items {
		if c.Id == fresh.ID || c.Id == waiting.ID {
			t.Errorf("%s was moved on by this search, not held before it", c.Name)
		}
	}

	_, list := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/tasks", "", nil)
	var tasks api.ListTasks200JSONResponse
	if json.Unmarshal([]byte(list), &tasks.Body) != nil || len(tasks.Body.Items) != 1 || tasks.Body.Items[0].LeftOut != 3 || tasks.Body.Items[0].Found.Companies != 5 {
		t.Errorf("the search should have found 5, 3 of them left out: %s", list)
	}

	// Put up for review again, it leaves the panel and waits in review.
	if code, body := call(t, ada, "PATCH", fmt.Sprintf("%s/o/alpha/api/v1/companies/%d", srv.URL, rejected.ID), "application/json",
		map[string]any{"stage": "review", "offering": left.Body.Offering}); code/100 != 2 {
		t.Fatalf("reviewing Corvane again: %d %s", code, body)
	}
	if again := leftOutOf(t, ada, srv.URL, task.Id); again.Body.Rejected != 0 || len(again.Body.Items) != 2 {
		t.Errorf("Corvane is up for review again, so not left out: %+v", again)
	}
	_, queue := call(t, ada, "GET", fmt.Sprintf("%s/o/alpha/api/v1/research?task=%d", srv.URL, task.Id), "", nil)
	for _, name := range []string{"Belair Kitchens", "Larkmoor Yachts"} {
		if strings.Contains(queue, name) {
			t.Errorf("%s is left out and in review at once: %s", name, queue)
		}
	}
	if !strings.Contains(queue, "Corvane Marine") {
		t.Errorf("Corvane should wait in this search's review: %s", queue)
	}
	// Qualified after another look, it is someone's decision now, not something left out.
	if err := s.SetCompanyStage(o.ID, rejected.ID, CompanyQualified, "", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if again := leftOutOf(t, ada, srv.URL, task.Id); again.Body.Rejected != 0 || len(again.Body.Items) != 2 {
		t.Errorf("Corvane, qualified after review, came back as rejected before: %+v", again)
	}
}

// An agent can reach a company first through a person or a fact; it is judged the same
// as one it adds, so a customer reached that way isn't a new find either.
func TestACompanyReachedThroughAPersonIsHeldToo(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	scout := Actor{Name: "agent:scout", KeyID: 1}
	belair := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Belair Kitchens", Domain: "belair.example"}))
	if err := s.SetCompanyStage(o.ID, belair.ID, CompanyQualified, "", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	must(s.AddSubscription(o.ID, sales.SubscriptionIn{Company: belair.ID, Name: "Support", Amount: "1200", Per: PerYear, Start: time.Now().Format(dayLayout), TermMonths: 12, Then: sales.ThenRenews}, "Ada Lovelace"))
	quiet := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Fennholt Campers", Domain: "fennholt.example"}))
	if err := s.SetCompanyStage(o.ID, quiet.ID, CompanyQualified, "", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}

	task := must(s.CreateTask(o.ID, "", "Kitchen makers.", "Ada Lovelace"))
	must(s.AgentAddContact(o, scout, AddContactIn{Company: "Belair Kitchens", Name: "Mara Quist", SourceURL: "https://belair.example/team", Task: task.ID}))
	must(s.AgentAddFact(o.ID, scout, AddFactIn{Company: "Fennholt Campers", FactIn: FactIn{Field: "size", Value: "12 staff", SourceURL: "https://fennholt.example/about"}, Task: task.ID}))
	// Added afterwards, it is already this task's: nothing changes.
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Belair Kitchens", Task: task.ID}))

	left := leftOutOf(t, ada, srv.URL, task.ID)
	if left.Body.Customer != 1 || left.Body.Qualified != 1 || len(left.Body.Items) != 2 {
		t.Errorf("Belair reached through a person is a customer, Fennholt through a fact qualified already: %+v", left)
	}
}

// A rejection for another offering says nothing about this one, so a company rejected
// for the organisation's own waits in an offering's search like any other.
func TestARejectionForAnotherOfferingIsntLeftOut(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	scout := Actor{Name: "agent:scout", KeyID: 1}
	cto := must(s.AddOffering(o.ID, Offering{Name: "Interim CTO"}, "Ada"))
	known := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Harbour Analytics", Domain: "harbour.example"}))
	if err := s.SetCompanyStage(o.ID, known.ID, CompanyRejected, "", "Ada"); err != nil {
		t.Fatal(err)
	}
	task := must(s.CreateTaskFor(o.ID, cto.ID, "", "Scale-ups.", "Ada Lovelace"))
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Harbour Analytics", Task: task.ID}))
	if left := leftOutOf(t, ada, srv.URL, task.ID); len(left.Body.Items) != 0 || left.Body.Offering != cto.ID {
		t.Errorf("rejected for the organisation's own, Harbour should wait in this offering's review: %+v", left)
	}
}

// People an agent is refused for a search are counted by why, and nothing else: the
// count outlives an erasure and goes out in exports.
func TestASearchCountsThePeopleItWasRefused(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	scout := Actor{Name: "agent:scout", KeyID: 1}
	gone := must(s.QuickAddPerson(o, QuickAdd{Name: "Ines Ward", Company: "Larkmoor Yachts"}, "Ada Lovelace"))
	if err := s.ErasePerson(o.ID, gone.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	o.Settings.NeverContact = "theo@larkmoor.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	task := must(s.CreateTask(o.ID, "", "Boat makers.", "Ada Lovelace"))
	never := AddContactIn{Company: "Larkmoor Yachts", Name: "Theo Marsh", Email: "theo@larkmoor.example", SourceURL: "https://larkmoor.example/team"}
	erasedOne := AddContactIn{Company: "Larkmoor Yachts", Name: "Ines Ward", SourceURL: "https://larkmoor.example/team"}

	// Outside a search, refusals are as they were and nothing is counted.
	_, outside := s.AgentAddContact(o, scout, never)
	if outside == nil {
		t.Fatal("Theo is on the never-contact list")
	}
	var events int64
	s.DB.Model(&Event{}).Where("org_id = ? and action = ?", o.ID, actionWithheld).Count(&events)
	if events != 0 {
		t.Fatalf("a refusal outside a search was counted: %d", events)
	}

	never.Task, erasedOne.Task = task.ID, task.ID
	for range 2 {
		if _, err := s.AgentAddContact(o, scout, never); err == nil || err.Error() != outside.Error() {
			t.Fatalf("for a search, Theo's refusal should be the same: %v", err)
		}
	}
	if _, err := s.AgentAddContact(o, scout, erasedOne); err == nil {
		t.Fatal("Ines was erased")
	}
	left := leftOutOf(t, ada, srv.URL, task.ID)
	if left.Body.NeverContact != 2 || left.Body.Erased != 1 {
		t.Errorf("Theo refused twice and Ines once: %+v", left)
	}

	var counted []Event
	s.DB.Where("org_id = ? and action = ?", o.ID, actionWithheld).Find(&counted)
	for _, e := range counted {
		if e.ContactID != 0 || e.CompanyID != nil || e.Value == nil || (*e.Value != withheldNeverContact && *e.Value != withheldErased) {
			t.Errorf("a refusal kept more than why: %+v", e)
		}
	}
}

func leftOutOf(t *testing.T, c *http.Client, base string, task int64) api.ListLeftOut200JSONResponse {
	t.Helper()
	code, body := call(t, c, "GET", fmt.Sprintf("%s/o/alpha/api/v1/tasks/%d/left-out", base, task), "", nil)
	var out api.ListLeftOut200JSONResponse
	if code != http.StatusOK || json.Unmarshal([]byte(body), &out.Body) != nil {
		t.Fatalf("left out: %d %s", code, body)
	}
	return out
}
