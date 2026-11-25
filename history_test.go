package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestHistoryKeepsNoPersonalDetails(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	do := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Import(o.ID, readFixtures(t)); err != nil {
		t.Fatal(err)
	}
	hannah := must(s.FindOne(o.ID, "Hannah"))
	for field, value := range map[string]string{
		"phone": "+44 20 7946 0000", "email": "h.pryce@larkmoor.example", "linkedin": "https://www.linkedin.com/in/hannah-pryce-4a2b1c/",
		"call_notes": "prefers mornings, has a dog called Biscuit", "person": "Hannah Pryce-Lindqvist", "title": "Head of Marketing",
		"note": "Hi Hannah, saw the Larkmoor relaunch", "next_step": "ring her mobile on Tuesday", "lost_reason": "",
	} {
		do(s.Update(o.ID, hannah.ID, field, value, "ada"))
	}
	do(s.Mark(o.ID, hannah.ID, "replied", "2020-01-02", "ada"))
	do(s.Update(o.ID, hannah.ID, "stage", "meeting", "ada"))
	do(s.Update(o.ID, hannah.ID, "next_date", "2020-02-03", "ada"))
	do(s.Update(o.ID, hannah.ID, "lost_reason", "went with her agency's tool", "ada"))
	nia := must(s.QuickAddPerson(o, QuickAdd{Name: "Nia Okafor", Company: deref(hannah.Maker), Linkedin: "https://www.linkedin.com/in/nia-okafor-9z8y7x/", Note: "Hi Nia, saw the relaunch"}, "ada"))
	must(s.AgentAddContact(o, Actor{Name: "agent:scout", KeyID: 1}, AddContactIn{Company: strconv.FormatInt(*hannah.CompanyID, 10), Name: "Jonas Lindqvist", Title: "CMO", SourceURL: "https://larkmoor.example/team"}))

	var events []Event
	do(s.in(o.ID).Find(&events).Error)
	said := map[string]string{}
	for _, e := range events {
		v := strings.ToLower(deref(e.Value))
		for _, detail := range []string{"7946", "pryce", "biscuit", "lindqvist", "marketing", "relaunch", "tuesday", "agency", "nia-okafor"} {
			if strings.Contains(v, detail) {
				t.Errorf("the history copied %q under %s", deref(e.Value), e.Action)
			}
		}
		said[e.Action] = deref(e.Value)
	}
	for _, action := range []string{"set:phone", "set:email", "set:call_notes", "set:person", "set:next_step", "add:person", "add:contact"} {
		if _, ok := said[action]; !ok {
			t.Errorf("the history lost that %s happened", action)
		}
	}
	if said["set:stage"] != "meeting" || said["set:next_date"] != "2020-02-03" || said["mark:replied"] != "2020-01-02" {
		t.Fatalf("the history lost where the conversation stands: %v", said)
	}

	history := must(s.History(o.ID, *hannah.CompanyID, 50))
	lines := map[string]bool{}
	for _, h := range history {
		lines[historyText(h)] = true
	}
	for _, want := range []string{"set phone · Hannah Pryce-Lindqvist", "set stage · Hannah Pryce-Lindqvist: meeting", "added · " + displayName(*nia), "suggested a contact · Jonas Lindqvist"} {
		if !lines[want] {
			t.Errorf("the company's history has no line %q: %v", want, lines)
		}
	}
}

func TestAgentsSeeWhatHappenedAtACompany(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout, second := Actor{Name: "agent:scout", KeyID: 1}, Actor{Name: "agent:second", KeyID: 2}
	added, err := s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor Yachts",
		Facts: []FactIn{{Field: "configurator", Value: "Day Cruiser 45 builder", SourceURL: "https://larkmoor.example/build"}}})
	if err != nil {
		t.Fatal(err)
	}
	hannah, err := s.AgentAddContact(o, scout, AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", SourceURL: "https://larkmoor.example/team"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Corvane"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PickContact(o.ID, o, hannah.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if err := s.AgentMark(o.ID, scout, MarkIn{ContactID: hannah.ID, Step: "linkedin", Date: "2026-11-20", Note: "Ada said she sent it from her phone"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(o.ID, hannah.ID, "next_step", "Send the brochure", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}

	theirs, err := s.AgentCompany(o.ID, second, "Larkmoor Yachts")
	if err != nil {
		t.Fatal(err)
	}
	if l := theirs.LastAgent; l == nil || l.Actor != "agent:scout" || l.ByYou || l.At.IsZero() {
		t.Fatalf("the second agent should see the first was here: %+v", l)
	}
	var did []string
	for _, h := range theirs.History {
		did = append(did, h.Action+"/"+h.Actor+"/"+h.Person+"/"+h.Value)
		if h.ByYou {
			t.Fatalf("nothing here was the second agent's: %+v", h)
		}
	}
	want := "set:next_step/Ada Lovelace/Hannah Pryce/ mark:linkedin/agent:scout/Hannah Pryce/2026-11-20 " +
		"pick/Ada Lovelace/Hannah Pryce/ company:qualified/Ada Lovelace// add:contact/agent:scout/Hannah Pryce/ add:company/agent:scout//"
	if got := strings.Join(did, " "); got != want {
		t.Fatalf("history, newest first:\n got %s\nwant %s", got, want)
	}
	if p := theirs.People[0]; p.NextStep != "Send the brochure" {
		t.Fatalf("the person's next step isn't shown: %+v", p)
	}
	shown, _ := json.Marshal(theirs)
	if strings.Contains(string(shown), "from her phone") || strings.Contains(string(shown), "Send the brochure\",\"person") {
		t.Fatalf("free text from the history reached an agent: %s", shown)
	}

	mine, err := s.AgentCompany(o.ID, scout, "Larkmoor Yachts")
	if err != nil || !mine.LastAgent.ByYou || !mine.History[len(mine.History)-1].ByYou || mine.History[0].ByYou {
		t.Fatalf("the first agent should recognise its own work and not Ada's: %+v %v", mine, err)
	}
	// A key recreated under the same name is a different agent.
	if again, _ := s.AgentCompany(o.ID, Actor{Name: "agent:scout", KeyID: 9}, "Larkmoor Yachts"); again.LastAgent.ByYou {
		t.Fatal("a new key took an old key's work for its own because they share a name")
	}

	for _, hit := range must(firstOf(s.AgentSearch(o.ID, second, "larkmoor"))) {
		if hit.Kind == "company" && (hit.ID != added.ID || hit.LastAgent == nil || hit.LastAgent.Actor != "agent:scout" || hit.LastAgent.ByYou) {
			t.Fatalf("search should say who was last at the company: %+v", hit)
		}
		if hit.Kind == "contact" && hit.LastAgent != nil {
			t.Fatalf("last_agent is about companies: %+v", hit)
		}
	}
	if err := s.SetCompanyStage(o.ID, added.ID, CompanyRejected, "", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	after := must(s.AgentCompany(o.ID, second, "Larkmoor Yachts"))
	if after.LastAgent.Actor != "agent:scout" || after.History[0].Action != "company:rejected" {
		t.Fatalf("a person's decision belongs in the history but isn't an agent's visit: %+v", after)
	}
	beta := mustOrg(t, s, "beta")
	if _, err := s.QuickAddPerson(beta, QuickAdd{Linkedin: "https://www.linkedin.com/in/jonas-weber", Name: "Jonas Weber", Company: "Larkmoor Yachts"}, "Grace Hopper"); err != nil {
		t.Fatal(err)
	}
	if other := must(s.AgentCompany(beta.ID, second, "Larkmoor Yachts")); other.LastAgent != nil || len(other.History) != 1 {
		t.Fatalf("another organisation's namesake shows this one's agents: %+v", other)
	}

	if err := s.ErasePerson(o.ID, hannah.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	gone, _ := json.Marshal(must(s.AgentCompany(o.ID, second, "Larkmoor Yachts")))
	if strings.Contains(string(gone), "Hannah") || strings.Contains(string(gone), "Pryce") || strings.Contains(string(gone), "brochure") {
		t.Fatalf("an erased person is still in what agents are shown: %s", gone)
	}
}
