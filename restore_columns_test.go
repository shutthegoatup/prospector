package main

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/privacy"
)

// A column added to Contact has to be carried by the snapshot, or be named here as
// one a restore works out again.
func TestASnapshotCarriesEveryContactColumn(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	rebuilt := []string{"org_id", "company_id"}
	stmt := &gorm.Statement{DB: s.DB}
	if err := stmt.Parse(&Contact{}); err != nil {
		t.Fatal(err)
	}
	for _, f := range stmt.Schema.Fields {
		if !slices.Contains(privacy.SnapshotColumns, f.DBName) && !slices.Contains(rebuilt, f.DBName) {
			t.Errorf("contacts.%s is in no snapshot, so a restore would empty it: add it to laterColumns", f.DBName)
		}
	}
	if len(privacy.LaterColumns) != len((&Contact{}).Later()) {
		t.Fatal("laterColumns and Contact.later() have drifted apart")
	}
}

func suggestedAndWorked(t *testing.T, s *Store, o *Org) (suggested, worked int64) {
	t.Helper()
	agent := Actor{Name: "agent:scout", KeyID: 1}
	co := must(s.AgentAddCompany(o.ID, agent, AddCompanyIn{Name: "Larkmoor", Domain: "larkmoor.example"}))
	add := func(name, email string) int64 {
		return must(s.AgentAddContact(o, agent, AddContactIn{Company: co.Name, Name: name, Title: "Head of Digital",
			Email: email, EmailStatus: "guess", SourceURL: "https://larkmoor.example/team"})).ID
	}
	suggested, worked = add("Ada Vale", "ada.vale@larkmoor.example"), add("Bram Osei", "bram.osei@larkmoor.example")
	for _, err := range []error{
		s.PickContact(o.ID, o, worked, "Nia"),
		s.AgentDraft(o.ID, agent, DraftIn{ContactID: worked, Kind: "email", Subject: "Hello", Text: "A few words."}),
		s.Mark(o.ID, worked, "replied", "2020-01-02", "Nia"),
		s.Update(o.ID, worked, "stage", Stages[1].Key, "Nia"),
		s.Update(o.ID, worked, "next_step", "send the proposal", "Nia"),
		s.Update(o.ID, worked, "next_date", "2030-01-02", "Nia"),
		s.Update(o.ID, worked, "lost_reason", "no budget this year", "Nia"),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return suggested, worked
}

func contactsOf(t *testing.T, s *Store, org int64) []Contact {
	t.Helper()
	var rows []Contact
	if err := s.InOrg(org).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestRestoreBringsBackEveryColumn(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	suggested, _ := suggestedAndWorked(t, s, o)
	before := contactsOf(t, s, o.ID)
	for _, c := range before {
		for i, f := range c.Later() {
			// Between them the two hold something in every column: only one still waiting has a status.
			if (c.ID == suggested) == (privacy.LaterColumns[i] == "status") && deref(privacy.LaterValue(f)) == "" {
				t.Fatalf("the test leaves %s empty for contact %d, so it wouldn't notice it lost", privacy.LaterColumns[i], c.ID)
			}
		}
	}
	snapshot := must(s.SnapshotText(o.ID))
	if _, err := s.Restore(o.ID, snapshot); err != nil {
		t.Fatal(err)
	}
	after := contactsOf(t, s, o.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("a restore of its own snapshot changed the contacts:\nbefore %+v\nafter  %+v", before, after)
	}
	if again := must(s.SnapshotText(o.ID)); again != snapshot {
		t.Fatal("the snapshot of a restored snapshot differs from it")
	}
	if c := must(s.FindOne(o.ID, "Ada Vale")); c.Status != ContactSuggested || c.Open() {
		t.Fatalf("someone an agent suggested came back as picked: status %q", c.Status)
	}
}

// A snapshot written before the later columns were carried says nothing about them,
// so it mustn't clear them for someone who is still here.
func TestAnOlderSnapshotLeavesLaterColumnsAlone(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	suggestedAndWorked(t, s, o)
	before := contactsOf(t, s, o.ID)
	older := ""
	for _, c := range before {
		older += fmt.Sprintf("insert into contacts (id, key, file, maker, person, email) values (%d, %s, 'leads.md', %s, %s, %s);\n",
			c.ID, privacy.SqlQuote(&c.Key), privacy.SqlQuote(c.Maker), privacy.SqlQuote(c.Person), privacy.SqlQuote(c.Email))
	}
	rows, absent, err := privacy.ParseInserts(older)
	if err != nil || len(rows) != len(before) || !slices.Equal(absent, privacy.LaterColumns) {
		t.Fatalf("an older snapshot should name none of the later columns: %d rows, absent %v, %v", len(rows), absent, err)
	}
	if _, err := s.Restore(o.ID, older); err != nil {
		t.Fatal(err)
	}
	for i, c := range contactsOf(t, s, o.ID) {
		for j, f := range c.Later() {
			if got, want := deref(privacy.LaterValue(f)), deref(privacy.LaterValue(before[i].Later()[j])); got != want {
				t.Errorf("%s: %s was %q and is now %q", c.Key, privacy.LaterColumns[j], want, got)
			}
		}
	}
}
