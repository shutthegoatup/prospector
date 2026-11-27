package main

import "testing"

// TestTheDetailsCountIsTheDetailsList: the count is worked out without reading what
// the list shows, so it is held to the list. A suggestion the person has since been
// given some other way leaves nothing to check in either.
func TestTheDetailsCountIsTheDetailsList(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout, second := Actor{Name: "agent:scout", KeyID: 1}, Actor{Name: "agent:second", KeyID: 2}
	for _, name := range []string{"Larkmoor Yachts", "Windrush Marine", "Quay Boats"} {
		must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: name}))
		must(s.AgentAddContact(o, scout, AddContactIn{Company: name, Name: "Hannah " + name, Phone: "+44 20 7946 0000", SourceURL: "https://example.example/team"}))
		// Another number for someone already here waits for a person to check.
		must(s.AgentAddContact(o, second, AddContactIn{Company: name, Name: "Hannah " + name, Phone: "+44 20 7946 0111", SourceURL: "https://example.example/contact"}))
	}
	agrees := func(want int) {
		t.Helper()
		rows := must(s.detailsToCheck(o.ID, 0, 0))
		if n := must(s.detailsCount(o.ID, 0, 0)); n != len(rows) || n != want || s.ResearchCounts(o.ID)["details"] != want {
			t.Fatalf("%d companies listed, %d counted, %d in the counts; want %d", len(rows), n, s.ResearchCounts(o.ID)["details"], want)
		}
	}
	agrees(3)

	// One is given the suggested number by hand: nothing is left to check there.
	c := must(s.FindOne(o.ID, "Hannah Quay Boats"))
	if err := s.Update(o.ID, c.ID, "phone", "+44 20 7946 0111", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	agrees(2)
}
