package main

import "testing"

func TestRestoreDoesntHandOutADroppedPersonsID(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	if _, err := s.Import(o.ID, readFixtures(t)); err != nil {
		t.Fatal(err)
	}
	hannah := must(s.FindOne(o.ID, "Hannah"))
	before := must(s.SnapshotText(o.ID))
	nia := must(s.QuickAddPerson(o, QuickAdd{Name: "Nia Okafor", Company: deref(hannah.Maker)}, "ada"))
	if err := s.Update(o.ID, nia.ID, "call_notes", "asked for a callback in spring", "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restore(o.ID, before); err != nil {
		t.Fatal(err)
	}
	omar := must(s.QuickAddPerson(o, QuickAdd{Name: "Omar Haddad", Company: deref(hannah.Maker)}, "ada"))
	var inherited int64
	s.in(o.ID).Model(&Event{}).Where("contact_id = ? and action <> ?", omar.ID, "add:person").Count(&inherited)
	if omar.ID == nia.ID || inherited != 0 {
		t.Fatalf("the next person added got id %d, which was Nia's (%d), and %d lines of her history", omar.ID, nia.ID, inherited)
	}
}
