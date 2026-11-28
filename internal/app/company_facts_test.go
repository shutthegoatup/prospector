package app

import (
	"slices"
	"testing"

	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/store"
)

// An agent that names a known company with another domain, country or segment changes
// nothing on file; what it said is kept as facts beside it, once.
func TestWhatAnAgentSaysOtherwiseIsKeptAsFacts(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := store.Actor{Name: "agent:scout", KeyID: 1}
	first := must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Larkmoor Yachts", Domain: "larkmoor.example", Country: "GB", Segment: "marine"}))
	again := research.AddCompanyIn{Name: "Larkmoor Yachts", Domain: "https://www.larkmoor-yachts.example/", Country: "gb", Segment: "boatbuilding"}
	out := must(s.AgentAddCompany(o.ID, scout, again))
	must(s.AgentAddCompany(o.ID, scout, again))
	if !out.Existed || out.ID != first.ID {
		t.Fatalf("the second saying made another company: %+v", out)
	}

	var c store.Company
	if err := s.DB.First(&c, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if c.Domain != "larkmoor.example" || c.Country != "GB" || c.Segment != "marine" {
		t.Errorf("the company on file changed: %s %s %s", c.Domain, c.Country, c.Segment)
	}
	var facts []store.Fact
	if err := s.DB.Where("org_id = ? and company_id = ?", o.ID, c.ID).Order("id").Find(&facts).Error; err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range facts {
		got = append(got, f.Field+"="+f.Value+" by "+f.Actor)
	}
	// The country differs only in its capitals, so it isn't another answer.
	want := []string{"domain=larkmoor-yachts.example by agent:scout", "segment=boatbuilding by agent:scout"}
	if !slices.Equal(got, want) {
		t.Errorf("facts:\n got %q\nwant %q", got, want)
	}
}
