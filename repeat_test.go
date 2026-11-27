package main

import (
	"strings"
	"testing"
)

func TestRepeatedAgentWorkAddsNothing(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout, second := Actor{Name: "agent:scout", KeyID: 1}, Actor{Name: "agent:second", KeyID: 2}
	builder := FactIn{Field: "configurator", Value: "Day Cruiser 45 builder", SourceURL: "https://larkmoor.example/build"}

	first, err := s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor Yachts", Domain: "larkmoor.example", Facts: []FactIn{builder}})
	if err != nil || first.Existed {
		t.Fatalf("add company: %+v %v", first, err)
	}
	again, err := s.AgentAddCompany(o.ID, second, AddCompanyIn{Name: "Larkmoor Yachts Ltd", Domain: "https://www.Larkmoor.example/about", Facts: []FactIn{
		{Field: "Configurator", Value: " day cruiser 45 builder ", SourceURL: builder.SourceURL},
		{Field: "press", Value: "Boat of the year", SourceURL: "https://larkmoor.example/news"}}})
	if err != nil || !again.Existed || again.ID != first.ID || again.Name != "Larkmoor Yachts" {
		t.Fatalf("the same site under another name should be the same company: %+v %v", again, err)
	}
	if hits := mustSearch(t, s, o.ID, "larkmoor"); len(hits) != 1 {
		t.Fatalf("one company became %d: %+v", len(hits), hits)
	}
	if facts, _ := s.Facts(o.ID, first.ID); len(facts) != 2 {
		t.Fatalf("a repeated fact was stored again, or a new one dropped: %+v", facts)
	}
	repeat, err := s.AgentAddFact(o.ID, second, AddFactIn{Company: "Larkmoor Yachts", FactIn: builder})
	if err != nil || !repeat.Existed || repeat.ID == 0 || repeat.Actor != "agent:scout" {
		t.Fatalf("a repeated fact should come back as the one already there: %+v %v", repeat, err)
	}
	elsewhere := builder
	elsewhere.SourceURL = "https://boats.example/review"
	if f, err := s.AgentAddFact(o.ID, second, AddFactIn{Company: "Larkmoor Yachts", FactIn: elsewhere}); err != nil || f.Existed {
		t.Fatalf("the same claim from another page is new evidence: %+v %v", f, err)
	}

	hannah, err := s.AgentAddContact(o, scout, AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", Title: "Head of Marketing",
		Linkedin: "https://www.linkedin.com/in/hannah-pryce/", SourceURL: "https://larkmoor.example/team"})
	if err != nil || hannah.Existed {
		t.Fatalf("add contact: %+v %v", hannah, err)
	}
	jonas, err := s.AgentAddContact(o, scout, AddContactIn{Company: "Larkmoor Yachts", Name: "Jonas Weber", Email: "jonas@larkmoor.example", SourceURL: "https://larkmoor.example/team"})
	if err != nil {
		t.Fatal(err)
	}
	for why, in := range map[string]AddContactIn{
		"name":    {Name: "hannah pryce", Title: "CMO"},
		"profile": {Name: "H. Pryce", Linkedin: "https://uk.linkedin.com/in/hannah-pryce?trk=people"},
	} {
		in.Company, in.SourceURL = "Larkmoor Yachts", "https://larkmoor.example/about"
		got, err := s.AgentAddContact(o, second, in)
		if err != nil || !got.Existed || got.ID != hannah.ID || got.Status != ContactSuggested {
			t.Fatalf("the same person by %s was added again: %+v %v", why, got, err)
		}
	}
	if got, err := s.AgentAddContact(o, second, AddContactIn{Company: "Larkmoor Yachts", Name: "J Weber", Email: "Jonas@Larkmoor.example",
		SourceURL: "https://larkmoor.example/about"}); err != nil || !got.Existed || got.ID != jonas.ID {
		t.Fatalf("the same person by address was added again: %+v %v", got, err)
	}
	if got, err := s.AgentAddContact(o, second, AddContactIn{Company: "Larkmoor Yachts", Name: "Grace Hopper", SourceURL: "https://larkmoor.example/team"}); err != nil || got.Existed {
		t.Fatalf("someone new was taken for someone known: %+v %v", got, err)
	}
	if people, _ := s.People(o.ID, first.ID); len(people) != 3 {
		t.Fatalf("want Hannah, Jonas and Grace, got %d people", len(people))
	}

	var events []Event
	s.InOrg(o.ID).Order("id").Find(&events)
	var did []string
	for _, e := range events {
		did = append(did, e.Action)
		if want := map[string]int64{"agent:scout": 1, "agent:second": 2}[e.User]; e.KeyID != want {
			t.Fatalf("%s by %s is recorded against key %d", e.Action, e.User, e.KeyID)
		}
	}
	if got := strings.Join(did, " "); got != "add:company add:facts add:fact add:contact add:contact add:contact" {
		t.Fatalf("work that changed nothing shouldn't be in the history: %s", got)
	}
	if v := deref(events[1].Value); v != "Larkmoor Yachts, 1 facts" {
		t.Fatalf("the history should count the facts that were new: %q", v)
	}
}

func TestASharedSiteNamesNoCompany(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := Actor{Name: "agent:scout", KeyID: 1}
	add := func(name string, facts ...FactIn) *AddCompanyOut {
		t.Helper()
		out, err := s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: name, Domain: "brandtech.example", Facts: facts})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	first := add("Larkmoor Yachts")
	// A group's second brand on the same site, as an import files it.
	second, err := s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Corvane"})
	if err != nil || second.Existed {
		t.Fatalf("add company: %+v %v", second, err)
	}
	if err := s.InOrg(o.ID).Model(&Company{}).Where("id = ?", second.ID).Update("domain", "brandtech.example").Error; err != nil {
		t.Fatal(err)
	}
	if again := add("larkmoor yachts"); again.ID != first.ID {
		t.Fatalf("a known name should win over the site: %+v", again)
	}
	fact := FactIn{Field: "press", Value: "Österreichs Boot des Jahres", SourceURL: "https://brandtech.example/news"}
	if got := add("corvane", fact); !got.Existed || got.ID != second.ID {
		t.Fatalf("facts for the second brand went to %+v, not company %d", got, second.ID)
	}
	if third := add("Ostrel Aviation"); third.Existed {
		t.Fatalf("a site two companies share picked one of them for a third: %+v", third)
	}
	shouted := fact
	shouted.Value = "ÖSTERREICHS BOOT DES JAHRES"
	if got, err := s.AgentAddFact(o.ID, scout, AddFactIn{Company: "Corvane", FactIn: shouted}); err != nil || !got.Existed {
		t.Fatalf("the same fact in capitals was stored again: %+v %v", got, err)
	}
}

func TestANoteTypedWhenAddingSomeoneIsTheirs(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	c, err := s.QuickAddPerson(o, QuickAdd{Linkedin: "https://www.linkedin.com/in/hannah-pryce", Name: "Hannah Pryce",
		Company: "Larkmoor Yachts", Note: "Hannah, we met at the boat show."}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AgentDraft(o.ID, Actor{Name: "agent:scout", KeyID: 1}, DraftIn{ContactID: c.ID, Kind: "note", Text: "Hi Hannah."}); err == nil {
		t.Fatal("an agent replaced the note a person typed when adding them")
	}
	templated, err := s.QuickAddPerson(o, QuickAdd{Linkedin: "https://www.linkedin.com/in/jonas-weber", Name: "Jonas Weber", Company: "Larkmoor Yachts"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AgentDraft(o.ID, Actor{Name: "agent:scout", KeyID: 1}, DraftIn{ContactID: templated.ID, Kind: "note", Text: "Hi Jonas."}); err != nil {
		t.Fatalf("the template note is there to be improved on: %v", err)
	}
}

func TestAgentsLeaveAPersonsWordsAlone(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout, second := Actor{Name: "agent:scout", KeyID: 1}, Actor{Name: "agent:second", KeyID: 2}
	if _, err := s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor Yachts"}); err != nil {
		t.Fatal(err)
	}
	c, err := s.AgentAddContact(o, scout, AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", SourceURL: "https://larkmoor.example/team"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PickContact(o.ID, o, c.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	note := func() string {
		var row Contact
		s.InOrg(o.ID).First(&row, c.ID)
		return deref(row.Note)
	}
	draft := func(who Actor, kind, text string) error {
		return s.AgentDraft(o.ID, who, DraftIn{ContactID: c.ID, Kind: kind, Text: text})
	}
	if err := draft(scout, "note", "Hi Hannah, saw the Day Cruiser builder."); err != nil {
		t.Fatalf("the template note is there to be improved on: %v", err)
	}
	if err := draft(second, "note", "Hannah, your Day Cruiser builder caught my eye."); err != nil {
		t.Fatalf("one agent may redraft another's text: %v", err)
	}
	if err := s.Update(o.ID, c.ID, "note", "Hannah, we met at the boat show.", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	err = draft(scout, "note", "Hi Hannah, a better note.")
	if err == nil || !strings.Contains(err.Error(), "Ada Lovelace") || note() != "Hannah, we met at the boat show." {
		t.Fatalf("an agent overwrote a person's edit: %v, note %q", err, note())
	}
	if err := draft(scout, "email", "Hannah, following up on the builder."); err != nil {
		t.Fatalf("her edit to the note shouldn't close the email: %v", err)
	}
	if err := s.Update(o.ID, c.ID, "note", "", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if err := draft(scout, "note", "Hi Hannah, a fresh note."); err != nil {
		t.Fatalf("a note a person cleared is open again: %v", err)
	}
	if err := s.Mark(o.ID, c.ID, "linkedin", today(), "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if err := draft(scout, "note", "Hi Hannah, too late."); err == nil || note() != "Hi Hannah, a fresh note." {
		t.Fatalf("a note that has gone out was rewritten: %v, note %q", err, note())
	}
	if err := draft(scout, "message", "Thanks for connecting, Hannah."); err != nil {
		t.Fatalf("the message after accepting is still to write: %v", err)
	}
}
