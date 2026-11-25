package main

import (
	"strings"
	"testing"
)

func TestRestoreKeepsHistoryWithItsPerson(t *testing.T) {
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
	hannah, jonas := must(s.FindOne(o.ID, "Hannah")), must(s.FindOne(o.ID, "Jonas"))
	do(s.Update(o.ID, hannah.ID, "phone", "+44 20 7946 0000", "ada"))
	do(s.Update(o.ID, jonas.ID, "tz", "Europe/Berlin", "ada"))
	before := must(s.SnapshotText(o.ID))

	nia := must(s.QuickAddPerson(o, QuickAdd{Name: "Nia Okafor", Company: deref(hannah.Maker), Email: "nia.okafor@larkmoor.example"}, "ada"))
	do(s.Update(o.ID, nia.ID, "call_notes", "asked for a callback in spring", "ada"))
	do(s.Mark(o.ID, nia.ID, "replied", "2020-01-02", "ada"))
	do(s.db.Create(&Fact{OrgID: o.ID, CompanyID: *nia.CompanyID, ContactID: &nia.ID, Field: "role", Value: "Head of Digital", SourceURL: "https://larkmoor.example/team"}).Error)
	do(s.db.Create(&Enquiry{OrgID: o.ID, Name: "Nia Okafor", Email: "nia.okafor@larkmoor.example", Message: "Could you call me?", ContactID: &nia.ID}).Error)

	if _, err := s.Restore(o.ID, before); err != nil {
		t.Fatal(err)
	}
	history := func(id int64) map[string]bool {
		var events []Event
		do(s.in(o.ID).Where("contact_id = ?", id).Find(&events).Error)
		out := map[string]bool{}
		for _, e := range events {
			out[e.Action] = true
		}
		return out
	}
	if h := history(hannah.ID); !h["set:phone"] {
		t.Fatalf("the restore lost the history of someone it kept: %v", h)
	}
	if h := history(nia.ID); len(h) != 0 {
		t.Fatalf("history is still filed under someone the restore dropped, for whoever gets her id next: %v", h)
	}
	var events []Event
	do(s.in(o.ID).Find(&events).Error)
	replied := false
	for _, e := range events {
		if v := strings.ToLower(deref(e.Value)); strings.Contains(v, "callback") || strings.Contains(v, "nia") {
			t.Errorf("the history still says %q about someone who is gone", deref(e.Value))
		}
		replied = replied || (e.Action == "mark:replied" && deref(e.Value) == "2020-01-02")
	}
	if !replied {
		t.Fatal("the history lost that a reply happened, which the counts are built from")
	}
	var facts int64
	s.in(o.ID).Model(&Fact{}).Where("contact_id = ?", nia.ID).Count(&facts)
	if facts != 0 {
		t.Fatal("a fact about someone the restore dropped is still filed under her id")
	}
	var enquiry Enquiry
	do(s.in(o.ID).Where("email = ?", "nia.okafor@larkmoor.example").First(&enquiry).Error)
	if enquiry.ContactID != nil || enquiry.Message == "" {
		t.Fatalf("what she wrote to us should stay, matched to nobody: %+v", enquiry)
	}

	if _, err := s.Restore(o.ID, "not a snapshot at all"); err == nil {
		t.Fatal("a file with no contacts in it replaced everyone")
	}
	if h := history(hannah.ID); !h["set:phone"] || must(s.FindOne(o.ID, "Hannah")) == nil {
		t.Fatal("a refused restore still changed something")
	}

	// A snapshot from another host can number the same people differently.
	do(s.db.Create(&Suggestion{OrgID: o.ID, ContactID: hannah.ID, Field: "phone", Value: "+44 20 7946 0111", By: "agent:scout"}).Error)
	waitsFor := func() (id int64) {
		s.db.Model(&Suggestion{}).Where("org_id = ?", o.ID).Limit(1).Pluck("contact_id", &id)
		return id
	}
	var rows []Contact
	do(s.in(o.ID).Find(&rows).Error)
	for i := range rows {
		switch rows[i].ID {
		case hannah.ID:
			rows[i].ID = jonas.ID
		case jonas.ID:
			rows[i].ID = hannah.ID
		}
	}
	if _, err := s.replaceContacts(o.ID, rows); err != nil {
		t.Fatal(err)
	}
	if now := must(s.FindOne(o.ID, "Hannah")); now.ID != jonas.ID {
		t.Fatalf("the test didn't trade their ids: Hannah is %d", now.ID)
	}
	if h := history(jonas.ID); !h["set:phone"] || h["set:tz"] {
		t.Fatalf("Hannah's history didn't follow her to her new id: %v", h)
	}
	if h := history(hannah.ID); !h["set:tz"] || h["set:phone"] {
		t.Fatalf("Jonas's history didn't follow him to his new id: %v", h)
	}
	if waitsFor() != jonas.ID {
		t.Fatalf("what was suggested for Hannah didn't follow her to her new id: it waits for %d", waitsFor())
	}

	do(s.in(o.ID).Find(&rows).Error)
	for i := range rows {
		if rows[i].ID == jonas.ID {
			rows[i].Key, rows[i].Person = "omar-haddad", str("Omar Haddad")
		}
	}
	if _, err := s.replaceContacts(o.ID, rows); err != nil {
		t.Fatal(err)
	}
	if h := history(jonas.ID); len(h) != 0 {
		t.Fatalf("a stranger who arrived under Hannah's id inherited her history: %v", h)
	}
	if waitsFor() != 0 {
		t.Fatal("a stranger who arrived under Hannah's id inherited what was suggested for her")
	}
}
