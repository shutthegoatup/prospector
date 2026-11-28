package main

import (
	"testing"

	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/store"
)

func TestRestoreDoesntHandOutADroppedPersonsID(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	if _, err := s.Import(o.ID, readFixtures(t)); err != nil {
		t.Fatal(err)
	}
	hannah := must(s.FindOne(o.ID, "Hannah"))
	before := must(s.SnapshotText(o.ID))
	nia := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Nia Okafor", Company: store.Deref(hannah.Maker)}, "ada"))
	if err := s.Update(o.ID, nia.ID, "call_notes", "asked for a callback in spring", "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restore(o.ID, before); err != nil {
		t.Fatal(err)
	}
	omar := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Omar Haddad", Company: store.Deref(hannah.Maker)}, "ada"))
	var inherited int64
	s.InOrg(o.ID).Model(&store.Event{}).Where("contact_id = ? and action <> ?", omar.ID, "add:person").Count(&inherited)
	if omar.ID == nia.ID || inherited != 0 {
		t.Fatalf("the next person added got id %d, which was Nia's (%d), and %d lines of her history", omar.ID, nia.ID, inherited)
	}
}
