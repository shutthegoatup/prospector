package main

import (
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
	nia := must(s.QuickAddPerson(o, QuickAdd{Name: "Nia Okafor", Company: deref(hannah.Maker), Linkedin: "https://www.linkedin.com/in/nia-okafor-9z8y7x/"}, "ada"))
	must(s.AgentAddContact(o, "agent:scout", AddContactIn{Company: strconv.FormatInt(*hannah.CompanyID, 10), Name: "Jonas Lindqvist", Title: "CMO", SourceURL: "https://larkmoor.example/team"}))

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
