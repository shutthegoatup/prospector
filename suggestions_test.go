package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/research"
)

func TestNewDetailsForSomeoneKnownWaitForAPerson(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	other := mustOrg(t, s, "beta")
	scout, second := Actor{Name: "agent:scout", KeyID: 1}, Actor{Name: "agent:second", KeyID: 2}
	team := "https://larkmoor.example/team"
	co := must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Larkmoor Yachts", Domain: "larkmoor.example"}))
	hannah := must(s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce",
		Linkedin: "https://www.linkedin.com/in/hannah-pryce/", Phone: "+44 20 7946 0000", SourceURL: team}))
	if hannah.Suggested != nil {
		t.Fatalf("someone new has nothing waiting: %+v", hannah)
	}
	found := prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", Email: "hannah.pryce@larkmoor.example", EmailStatus: "guess",
		Phone: "+44 (0)20 7946 0111", Linkedin: "https://uk.linkedin.com/in/hannah-pryce?trk=x", SourceURL: "https://larkmoor.example/contact"}
	got := must(s.AgentAddContact(o, second, found))
	if !got.Existed || got.ID != hannah.ID || strings.Join(got.Suggested, " ") != "email phone" {
		t.Fatalf("an email and a different number should wait, the profile she already has should not: %+v", got)
	}
	person := func() Contact {
		t.Helper()
		var c Contact
		if err := s.InOrg(o.ID).First(&c, hannah.ID).Error; err != nil {
			t.Fatal(err)
		}
		return c
	}
	if c := person(); c.Email != nil || deref(c.Phone) != "+44 20 7946 0000" {
		t.Fatalf("a suggestion changed her before anyone accepted it: email %v, phone %q", c.Email, deref(c.Phone))
	}

	again := must(s.AgentAddContact(o, scout, found))
	if strings.Join(again.Suggested, " ") != "email phone" {
		t.Fatalf("a repeat should say what is still waiting: %+v", again)
	}
	people := must(s.People(o.ID, co.ID))
	pending := must(s.Pending(o.ID, people))[hannah.ID]
	if len(pending) != 2 || pending[0].Field != "email" || pending[0].By != "agent:second" || pending[0].SourceURL != found.SourceURL ||
		pending[1].Current != "+44 20 7946 0000" {
		t.Fatalf("want one waiting email and one waiting number, each once: %+v", pending)
	}
	if theirs := must(s.Pending(other.ID, people)); len(theirs) != 0 {
		t.Fatalf("another organisation sees these suggestions: %+v", theirs)
	}

	// The company's page and Research's details to check read what waits from the API.
	page := "/o/alpha/api/v1/companies/" + strconv.FormatInt(co.ID, 10) + "/overview"
	for _, at := range []string{page, "/o/alpha/api/v1/research?filter=details"} {
		if _, body := call(t, client, "GET", srv.URL+at, "", nil); !strings.Contains(body, `"value":"hannah.pryce@larkmoor.example"`) ||
			!strings.Contains(body, `"email_status":"guess"`) || !strings.Contains(body, `"current":"+44 20 7946 0000"`) ||
			!strings.Contains(body, fmt.Sprintf(`"field":"email","id":%d,`, pending[0].ID)) {
			t.Fatalf("%s doesn't offer the suggestions: %s", at, body)
		}
	}
	decide := func(id int64, decision string) (int, string) {
		return call(t, client, "PATCH", srv.URL+"/o/alpha/api/v1/suggestions/"+strconv.FormatInt(id, 10), "application/json", map[string]any{"decision": decision})
	}
	if err := s.DecideSuggestion(other.ID, pending[0].ID, prospecting.SuggestionAccepted, "mallory"); err == nil {
		t.Fatal("another organisation settled this one's suggestion")
	}
	if code, body := decide(pending[0].ID, prospecting.SuggestionAccepted); code != http.StatusNoContent {
		t.Fatalf("accept: %d %s", code, body)
	}
	if code, body := decide(pending[1].ID, prospecting.SuggestionDismissed); code != http.StatusNoContent {
		t.Fatalf("dismiss: %d %s", code, body)
	}
	if code, _ := decide(pending[0].ID, prospecting.SuggestionDismissed); code != http.StatusConflict {
		t.Fatalf("a settled suggestion was settled again: %d", code)
	}
	if c := person(); deref(c.Email) != "hannah.pryce@larkmoor.example" || c.EmailStatus != "guess" || deref(c.Phone) != "+44 20 7946 0000" {
		t.Fatalf("accepted email %q (%s), dismissed number left her with %q", deref(c.Email), c.EmailStatus, deref(c.Phone))
	}
	if _, body := call(t, client, "GET", srv.URL+page, "", nil); !strings.Contains(body, `"pending":[]`) || strings.Contains(body, `"pending":[{`) {
		t.Fatal("settled suggestions are still offered")
	}
	if rows, _, _ := s.Research(o.ID, "details", 1); len(rows) != 0 {
		t.Fatalf("nothing is left to check, but Research lists %d", len(rows))
	}

	after := must(s.AgentAddContact(o, scout, found))
	if len(after.Suggested) != 0 {
		t.Fatalf("a dismissed number or an accepted email was raised again: %+v", after)
	}
	var n int64
	s.DB.Model(&Suggestion{}).Where("org_id = ?", o.ID).Count(&n)
	if n != 2 {
		t.Fatalf("want the two suggestions, got %d rows", n)
	}

	var events []Event
	s.InOrg(o.ID).Where("contact_id = ?", hannah.ID).Order("id").Find(&events)
	var did []string
	for _, e := range events {
		did = append(did, e.Action)
		if (strings.HasPrefix(e.Action, "suggest:") || strings.HasPrefix(e.Action, "dismiss:")) && e.Value != nil {
			t.Fatalf("%s put the detail in the history: %q", e.Action, *e.Value)
		}
	}
	if got := strings.Join(did, " "); got != "add:contact suggest:email suggest:phone set:email dismiss:phone" {
		t.Fatalf("history: %s", got)
	}
	if events[1].KeyID != 2 || events[3].User != "Ada Lovelace" {
		t.Fatalf("the suggestion is the agent's and the change is the person's: %+v %+v", events[1], events[3])
	}
}

func TestASuggestionCantGetPastTheNeverContactListOrAnErasure(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := Actor{Name: "agent:scout", KeyID: 1}
	in := prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", SourceURL: "https://larkmoor.example/team"}
	must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Larkmoor Yachts"}))
	hannah := must(s.AgentAddContact(o, scout, in))
	must(s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Jonas Weber", SourceURL: in.SourceURL}))

	in.Email = "hannah.pryce@larkmoor.example"
	if got := must(s.AgentAddContact(o, scout, in)); len(got.Suggested) != 1 {
		t.Fatalf("want her email waiting: %+v", got)
	}
	var sg Suggestion
	if err := s.DB.Where("contact_id = ?", hannah.ID).First(&sg).Error; err != nil {
		t.Fatal(err)
	}
	o.Settings.NeverContact = "larkmoor.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	if err := s.DecideSuggestion(o.ID, sg.ID, prospecting.SuggestionAccepted, "ada"); err == nil {
		t.Fatal("accepting put an address on the never-contact list onto a person")
	}
	if err := s.DB.First(&sg, sg.ID).Error; err != nil || sg.Decision != "" {
		t.Fatalf("a refused accept should leave the suggestion waiting: %+v %v", sg, err)
	}
	if _, err := s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Jonas Weber", Email: "jonas@larkmoor.example", SourceURL: in.SourceURL}); err == nil {
		t.Fatal("an address on the never-contact list was taken as a suggestion")
	}

	if err := s.ErasePerson(o.ID, hannah.ID, "ada"); err != nil {
		t.Fatal(err)
	}
	var left []Suggestion
	s.DB.Find(&left)
	if len(left) != 0 {
		t.Fatalf("erasing her left what was suggested for her: %+v", left)
	}
	o.Settings.NeverContact = ""
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	// Under a colleague's name, her erased address still can't come back.
	if _, err := s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Jonas Weber", Email: in.Email, SourceURL: in.SourceURL}); err == nil {
		t.Fatal("an erased person's address was taken as a suggestion for someone else")
	}
}

func TestSuggestionsAreInBothExports(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := Actor{Name: "agent:scout", KeyID: 1}
	in := prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", SourceURL: "https://larkmoor.example/team"}
	must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Larkmoor Yachts"}))
	hannah := must(s.AgentAddContact(o, scout, in))
	in.Phone = "+44 20 7946 0000"
	must(s.AgentAddContact(o, scout, in))

	for what, export := range map[string]any{"person": must(s.ExportPerson(o.ID, hannah.ID, "ada")), "organisation": must(s.ExportOrg(o.ID, "ada"))} {
		b, err := json.Marshal(export)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"suggested_details":[{`) || !strings.Contains(string(b), "+44 20 7946 0000") {
			t.Fatalf("the %s export leaves out a number held about her: %s", what, b)
		}
	}
}

func TestDetailsSentUnderAnotherNameAreNobodys(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := Actor{Name: "agent:scout", KeyID: 1}
	team := "https://larkmoor.example/team"
	must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Larkmoor Yachts"}))
	jonas := must(s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Jonas Weber", Email: "jonas@larkmoor.example", SourceURL: team}))
	hannah := must(s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", SourceURL: team}))
	profile := "https://www.linkedin.com/in/hannah-pryce-example/"

	// Jonas's address under Hannah's name matches Jonas; her profile and number aren't his.
	mixed := must(s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", Email: "jonas@larkmoor.example",
		Linkedin: profile, Phone: "+44 20 7946 0000", SourceURL: team}))
	var n int64
	s.DB.Model(&Suggestion{}).Count(&n)
	if !mixed.Existed || mixed.ID != jonas.ID || len(mixed.Suggested) != 0 || n != 0 {
		t.Fatalf("details sent under another name were filed for the person the address matched: %+v, %d rows", mixed, n)
	}

	got := must(s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", Linkedin: profile, Phone: "+44 20 7946 0000", SourceURL: team}))
	if got.ID != hannah.ID || strings.Join(got.Suggested, " ") != "phone linkedin" {
		t.Fatalf("want her number and profile waiting: %+v", got)
	}
	var waiting []Suggestion
	s.DB.Where("contact_id = ?", hannah.ID).Order("id").Find(&waiting)
	if err := s.DecideSuggestion(o.ID, waiting[0].ID, prospecting.SuggestionAccepted, "ada"); err != nil {
		t.Fatal(err)
	}
	var c Contact
	s.InOrg(o.ID).First(&c, hannah.ID)
	if deref(c.Phone) != "+44 20 7946 0000" || deref(c.Tz) == "" {
		t.Fatalf("an accepted number should bring its time zone, as a typed one does: %q %q", deref(c.Phone), deref(c.Tz))
	}

	// Typed in by a person meanwhile: nothing is left to offer.
	if err := s.Update(o.ID, hannah.ID, "linkedin", profile, "ada"); err != nil {
		t.Fatal(err)
	}
	people := must(s.People(o.ID, *c.CompanyID))
	if left := must(s.Pending(o.ID, people)); len(left) != 0 {
		t.Fatalf("a profile she now has is still offered: %+v", left)
	}
}

func TestOneOfSeveralAddressesIsAlreadyHeld(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := Actor{Name: "agent:scout", KeyID: 1}
	in := prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", SourceURL: "https://larkmoor.example/team"}
	must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Larkmoor Yachts"}))
	hannah := must(s.AgentAddContact(o, scout, in))
	if err := s.Update(o.ID, hannah.ID, "email", "Hannah@larkmoor.example, h.pryce@larkmoor.example", "ada"); err != nil {
		t.Fatal(err)
	}
	in.Email = "h.pryce@larkmoor.example"
	if got := must(s.AgentAddContact(o, scout, in)); len(got.Suggested) != 0 {
		t.Fatalf("accepting this would drop her other address: %+v", got)
	}
}

func TestErasingSomeoneCoversWhatWasSuggestedForThem(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := Actor{Name: "agent:scout", KeyID: 1}
	team := "https://larkmoor.example/team"
	in := prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", SourceURL: team}
	must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Larkmoor Yachts"}))
	hannah := must(s.AgentAddContact(o, scout, in))
	in.Email, in.Linkedin = "not.hers@larkmoor.example", "https://www.linkedin.com/in/hannah-pryce-example/"
	must(s.AgentAddContact(o, scout, in))
	in.Email = "hannah.pryce@larkmoor.example"
	must(s.AgentAddContact(o, scout, in))
	var waiting []Suggestion
	s.DB.Where("contact_id = ?", hannah.ID).Order("id").Find(&waiting)
	if len(waiting) != 3 || waiting[0].Value != "not.hers@larkmoor.example" {
		t.Fatalf("want two addresses and a profile waiting: %+v", waiting)
	}
	if err := s.DecideSuggestion(o.ID, waiting[0].ID, prospecting.SuggestionDismissed, "ada"); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Create(&Enquiry{OrgID: o.ID, Name: "H", Email: "hannah.pryce@larkmoor.example", Message: "Could you send pricing?"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.ErasePerson(o.ID, hannah.ID, "ada"); err != nil {
		t.Fatal(err)
	}
	var enquiries int64
	s.DB.Model(&Enquiry{}).Count(&enquiries)
	if enquiries != 0 {
		t.Fatal("what she wrote from an address suggested for her outlived her erasure")
	}
	add := func(name, email, link string) error {
		_, err := s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: name, Email: email, Linkedin: link, SourceURL: team})
		return err
	}
	if err := add("H. P.", "", "https://www.linkedin.com/in/hannah-pryce-example/"); err == nil {
		t.Fatal("the profile suggested for her came back under other initials")
	}
	if err := add("Nia Okafor", "not.hers@larkmoor.example", ""); err != nil {
		t.Fatalf("an address dismissed as not hers was banned with her: %v", err)
	}
}

func TestARestoreDropsSuggestionsForPeopleItDrops(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := Actor{Name: "agent:scout", KeyID: 1}
	in := prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", SourceURL: "https://larkmoor.example/team"}
	must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Larkmoor Yachts"}))
	// A restore refuses a file with nobody in it, so someone else is there throughout.
	must(s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Jonas Lindqvist", SourceURL: "https://larkmoor.example/team"}))
	before := must(s.SnapshotText(o.ID))
	must(s.AgentAddContact(o, scout, in))
	in.Phone = "+44 20 7946 0000"
	must(s.AgentAddContact(o, scout, in))
	if _, err := s.Restore(o.ID, before); err != nil {
		t.Fatal(err)
	}
	var n int64
	s.DB.Model(&Suggestion{}).Count(&n)
	if n != 0 {
		t.Fatalf("%d suggestions wait for someone the restore removed", n)
	}
}

// "Now get their phone numbers": the number waits for a person, and the task shows who it was found for.
func TestADetailFoundForATaskWaitsAndCountsTowardsTheTask(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := Actor{Name: "agent:scout", KeyID: 1}
	must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Larkmoor Yachts"}))
	hannah := must(s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", SourceURL: "https://larkmoor.example/team"}))
	task := must(s.CreateTask(o.ID, "Phones", "Find phone numbers for the people at Larkmoor.", "Ada Lovelace"))
	got := must(s.AgentAddContact(o, scout, prospecting.AddContactIn{Task: task.ID, Company: "Larkmoor Yachts", Name: "Hannah Pryce",
		Phone: "+44 20 7946 0000", SourceURL: "https://larkmoor.example/contact"}))
	if !got.Existed || strings.Join(got.Suggested, " ") != "phone" {
		t.Fatalf("the number should wait as a suggestion: %+v", got)
	}
	view := must(s.Task(o.ID, task.ID, time.Now()))
	if len(view.People) != 1 || view.People[0].ID != hannah.ID {
		t.Fatalf("the task should show who the number was found for: %+v", view.People)
	}
}

func TestAnAgentsGuessesDontPileUpOnOnePerson(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := Actor{Name: "agent:scout", KeyID: 1}
	in := prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", SourceURL: "https://larkmoor.example/team"}
	co := must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Larkmoor Yachts"}))
	hannah := must(s.AgentAddContact(o, scout, in))
	for _, guess := range []string{"hannah", "h.pryce", "hpryce", "hannah.pryce", "pryce.h"} {
		in.Email = guess + "@larkmoor.example"
		must(s.AgentAddContact(o, scout, in))
	}
	in.Email, in.Phone = "", strings.Repeat("7", prospecting.MaxDetail+1)
	must(s.AgentAddContact(o, scout, in))
	var waiting int64
	if s.DB.Model(&Suggestion{}).Where("contact_id = ?", hannah.ID).Count(&waiting); waiting != prospecting.MaxWaiting {
		t.Fatalf("want %d addresses waiting and no overlong number, got %d rows", prospecting.MaxWaiting, waiting)
	}

	// Typed in by a person meanwhile, a suggestion leaves nothing to check.
	if rows, _, _ := s.Research(o.ID, "details", 1); len(rows) != 1 || rows[0].ID != co.ID {
		t.Fatalf("the company should be under Details to check: %+v", rows)
	}
	for _, guess := range []string{"hannah", "h.pryce", "hpryce"} {
		if err := s.Update(o.ID, hannah.ID, "email", guess+"@larkmoor.example", "ada"); err != nil {
			t.Fatal(err)
		}
		if guess != "hpryce" {
			var sg Suggestion
			s.DB.Where("contact_id = ? and value = ?", hannah.ID, guess+"@larkmoor.example").First(&sg)
			if err := s.DecideSuggestion(o.ID, sg.ID, prospecting.SuggestionDismissed, "ada"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if rows, _, _ := s.Research(o.ID, "details", 1); len(rows) != 0 {
		t.Fatalf("a company with nothing left to check is still listed: %+v", rows[0].Pending)
	}
}

func TestARestoreDoesntHandASuggestionToWhoeverGotTheId(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := Actor{Name: "agent:scout", KeyID: 1}
	in := prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", SourceURL: "https://larkmoor.example/team"}
	must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Larkmoor Yachts"}))
	hannah := must(s.AgentAddContact(o, scout, in))
	in.Phone = "+44 20 7946 0000"
	must(s.AgentAddContact(o, scout, in))
	snapshot := must(s.SnapshotText(o.ID))
	count := func() (n int64) {
		s.DB.Model(&Suggestion{}).Count(&n)
		return n
	}
	if _, err := s.Restore(o.ID, snapshot); err != nil || count() != 1 {
		t.Fatalf("a restore that brings the same person back keeps what waits for her: %d %v", count(), err)
	}
	// The id is somebody else now; the snapshot gives it back to Hannah.
	if err := s.DB.Model(&Contact{}).Where("id = ?", hannah.ID).Update("key", "somebody-else").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restore(o.ID, snapshot); err != nil || count() != 0 {
		t.Fatalf("a suggestion followed an id to a different person: %d %v", count(), err)
	}
}
