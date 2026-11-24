package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestNewDetailsForSomeoneKnownWaitForAPerson(t *testing.T) {
	s, o, srv, client := signedIn(t)
	other := mustOrg(t, s, "beta")
	scout, second := Actor{Name: "agent:scout", KeyID: 1}, Actor{Name: "agent:second", KeyID: 2}
	team := "https://larkmoor.example/team"
	co := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor Yachts", Domain: "larkmoor.example"}))
	hannah := must(s.AgentAddContact(o, scout, AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce",
		Linkedin: "https://www.linkedin.com/in/hannah-pryce/", Phone: "+44 20 7946 0000", SourceURL: team}))
	if hannah.Suggested != nil {
		t.Fatalf("someone new has nothing waiting: %+v", hannah)
	}
	found := AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", Email: "hannah.pryce@larkmoor.example", EmailStatus: "guess",
		Phone: "+44 (0)20 7946 0111", Linkedin: "https://uk.linkedin.com/in/hannah-pryce?trk=x", SourceURL: "https://larkmoor.example/contact"}
	got := must(s.AgentAddContact(o, second, found))
	if !got.Existed || got.ID != hannah.ID || strings.Join(got.Suggested, " ") != "email phone" {
		t.Fatalf("an email and a different number should wait, the profile she already has should not: %+v", got)
	}
	person := func() Contact {
		t.Helper()
		var c Contact
		if err := s.in(o.ID).First(&c, hannah.ID).Error; err != nil {
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

	page := "/o/alpha/companies/" + strconv.FormatInt(co.ID, 10)
	for _, at := range []string{page, "/o/alpha/research?f=details"} {
		if _, body := call(t, client, "GET", srv.URL+at, "", nil); !strings.Contains(body, "hannah.pryce@larkmoor.example") ||
			!strings.Contains(body, "a guess") || !strings.Contains(body, "in place of +44 20 7946 0000") || !strings.Contains(body, `data-post="ui/suggestion"`) {
			t.Fatalf("%s doesn't offer the suggestions", at)
		}
	}
	decide := func(id int64, decision string) (int, string) {
		return call(t, client, "POST", srv.URL+"/o/alpha/ui/suggestion", "application/json", map[string]any{"id": id, "decision": decision})
	}
	if err := s.DecideSuggestion(other.ID, pending[0].ID, SuggestionAccepted, "mallory"); err == nil {
		t.Fatal("another organisation settled this one's suggestion")
	}
	if code, body := decide(pending[0].ID, SuggestionAccepted); code != http.StatusOK {
		t.Fatalf("accept: %d %s", code, body)
	}
	if code, body := decide(pending[1].ID, SuggestionDismissed); code != http.StatusOK {
		t.Fatalf("dismiss: %d %s", code, body)
	}
	if code, _ := decide(pending[0].ID, SuggestionDismissed); code != http.StatusBadRequest {
		t.Fatalf("a settled suggestion was settled again: %d", code)
	}
	if c := person(); deref(c.Email) != "hannah.pryce@larkmoor.example" || c.EmailStatus != "guess" || deref(c.Phone) != "+44 20 7946 0000" {
		t.Fatalf("accepted email %q (%s), dismissed number left her with %q", deref(c.Email), c.EmailStatus, deref(c.Phone))
	}
	if _, body := call(t, client, "GET", srv.URL+page, "", nil); strings.Contains(body, `data-post="ui/suggestion"`) {
		t.Fatal("settled suggestions are still offered")
	}
	if rows := must(s.Research(o.ID, "details")); len(rows) != 0 {
		t.Fatalf("nothing is left to check, but Research lists %d", len(rows))
	}

	after := must(s.AgentAddContact(o, scout, found))
	if len(after.Suggested) != 0 {
		t.Fatalf("a dismissed number or an accepted email was raised again: %+v", after)
	}
	var n int64
	s.db.Model(&Suggestion{}).Where("org_id = ?", o.ID).Count(&n)
	if n != 2 {
		t.Fatalf("want the two suggestions, got %d rows", n)
	}

	var events []Event
	s.in(o.ID).Where("contact_id = ?", hannah.ID).Order("id").Find(&events)
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
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := Actor{Name: "agent:scout", KeyID: 1}
	in := AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", SourceURL: "https://larkmoor.example/team"}
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor Yachts"}))
	hannah := must(s.AgentAddContact(o, scout, in))
	must(s.AgentAddContact(o, scout, AddContactIn{Company: "Larkmoor Yachts", Name: "Jonas Weber", SourceURL: in.SourceURL}))

	in.Email = "hannah.pryce@larkmoor.example"
	if got := must(s.AgentAddContact(o, scout, in)); len(got.Suggested) != 1 {
		t.Fatalf("want her email waiting: %+v", got)
	}
	var sg Suggestion
	if err := s.db.Where("contact_id = ?", hannah.ID).First(&sg).Error; err != nil {
		t.Fatal(err)
	}
	o.Settings.NeverContact = "larkmoor.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	if err := s.DecideSuggestion(o.ID, sg.ID, SuggestionAccepted, "ada"); err == nil {
		t.Fatal("accepting put an address on the never-contact list onto a person")
	}
	if err := s.db.First(&sg, sg.ID).Error; err != nil || sg.Decision != "" {
		t.Fatalf("a refused accept should leave the suggestion waiting: %+v %v", sg, err)
	}
	if _, err := s.AgentAddContact(o, scout, AddContactIn{Company: "Larkmoor Yachts", Name: "Jonas Weber", Email: "jonas@larkmoor.example", SourceURL: in.SourceURL}); err == nil {
		t.Fatal("an address on the never-contact list was taken as a suggestion")
	}

	if err := s.ErasePerson(o.ID, hannah.ID, "ada"); err != nil {
		t.Fatal(err)
	}
	var left []Suggestion
	s.db.Find(&left)
	if len(left) != 0 {
		t.Fatalf("erasing her left what was suggested for her: %+v", left)
	}
	o.Settings.NeverContact = ""
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	// Under a colleague's name, her erased address still can't come back.
	if _, err := s.AgentAddContact(o, scout, AddContactIn{Company: "Larkmoor Yachts", Name: "Jonas Weber", Email: in.Email, SourceURL: in.SourceURL}); err == nil {
		t.Fatal("an erased person's address was taken as a suggestion for someone else")
	}
}

func TestSuggestionsAreInBothExports(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := Actor{Name: "agent:scout", KeyID: 1}
	in := AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", SourceURL: "https://larkmoor.example/team"}
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor Yachts"}))
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
