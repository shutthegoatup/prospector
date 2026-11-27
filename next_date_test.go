package main

import "testing"

// A contact's next date is compared as text with today's, so it has to be a real day.
func TestAContactsNextDateIsADay(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	ines := must(s.QuickAddPerson(o, QuickAdd{Name: "Ines Moreau", Company: "Belair Kitchens"}, "Ada Lovelace"))
	for _, bad := range []string{"next tuesday", "2026-13-01", "1/12/2026"} {
		if err := s.Update(o.ID, ines.ID, "next_date", bad, "Ada Lovelace"); err == nil {
			t.Errorf("%q was taken as a next date", bad)
		}
	}
	for _, good := range []string{"2026-12-01", ""} {
		if err := s.Update(o.ID, ines.ID, "next_date", good, "Ada Lovelace"); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
}
