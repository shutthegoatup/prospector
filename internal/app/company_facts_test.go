package app

import (
	"slices"
	"testing"

	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/store"
)

// An agent that names a known company with another domain, country or segment changes
// nothing on file; what it said is kept as facts beside it, once, and the history says
// so. A field blank on file, or one differing only in capitals, isn't said otherwise.
func TestWhatAnAgentSaysOtherwiseIsKeptAsFacts(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := store.Actor{Name: "agent:scout", KeyID: 1}
	first := must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Larkmoor Yachts", Domain: "larkmoor.example", Country: "GB"}))
	must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Larkmoor Yachts", Domain: "LARKMOOR.example", Country: "gb"}))
	again := research.AddCompanyIn{Name: "Larkmoor Yachts", Domain: "https://www.larkmoor-yachts.example/", Country: "US", Segment: "boatbuilding"}
	out := must(s.AgentAddCompany(o.ID, scout, again))
	must(s.AgentAddCompany(o.ID, scout, again))
	if !out.Existed || out.ID != first.ID {
		t.Fatalf("saying it again made another company: %+v", out)
	}

	var c store.Company
	if err := s.DB.First(&c, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if c.Domain != "larkmoor.example" || c.Country != "GB" || c.Segment != "" {
		t.Errorf("the company on file changed: %q %q %q", c.Domain, c.Country, c.Segment)
	}
	var facts []store.Fact
	if err := s.DB.Where("org_id = ? and company_id = ?", o.ID, c.ID).Order("id").Find(&facts).Error; err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range facts {
		got = append(got, f.Field+"="+f.Value+" by "+f.Actor)
	}
	want := []string{"other_domain=larkmoor-yachts.example by agent:scout", "other_country=US by agent:scout"}
	if !slices.Equal(got, want) {
		t.Errorf("facts:\n got %q\nwant %q", got, want)
	}
	var said int64
	if err := s.DB.Model(&store.Event{}).Where("org_id = ? and company_id = ? and action = ? and value = ?",
		o.ID, c.ID, store.ActAddFacts.String(), "Larkmoor Yachts, 2 facts").Count(&said).Error; err != nil || said != 1 {
		t.Errorf("the history said it %d times (%v)", said, err)
	}
}
