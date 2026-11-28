package app

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/store"
)

func TestLookForAssets(t *testing.T) {
	t.Parallel()
	got, err := store.OrgSettings{LookFor: "Careers page: a jobs page on their site\n\nats\ncareers-page: again"}.Assets()
	if err != nil || len(got) != 2 || got[0] != (store.Asset{Field: "careers_page", Hint: "a jobs page on their site"}) || got[1] != (store.Asset{Field: "ats"}) {
		t.Fatalf("assets: %+v %v", got, err)
	}
	if _, err := (store.OrgSettings{LookFor: ": no name"}).Assets(); err == nil {
		t.Fatal("an asset without a name was accepted")
	}

	s, _, srv, client := signedIn(t)
	save := func(lookFor string) (int, string) {
		return call(t, client, "PATCH", srv.URL+"/o/alpha/api/v1/settings", "application/json",
			map[string]any{"name": "Alpha", "color": "#56c5f4", "look_for": lookFor})
	}
	if code, body := save(": a jobs page"); code != http.StatusBadRequest || !strings.Contains(body, "needs a name") {
		t.Fatalf("an unnamed asset was saved: %d %s", code, body)
	}
	if code, body := save("careers_page: a jobs page on their site\nats: the applicant tracking system they use"); code != http.StatusOK {
		t.Fatalf("saving what we look for: %d %s", code, body)
	}
	o, _ := s.OrgBySlug("alpha")
	k := &store.APIKey{Scopes: "research:write"}
	if g := agentInstructions(o, k); !strings.Contains(g, "- careers_page: a jobs page on their site") || !strings.Contains(g, "- ats: the applicant tracking system") {
		t.Fatalf("agents aren't told what to look for: %q", g)
	}

	out, err := s.AgentAddCompany(o.ID, store.Actor{Name: "agent:bot"}, research.AddCompanyIn{Name: "Brightwell Staffing", Domain: "brightwell.example",
		Facts: []research.FactIn{{Field: "Careers page", Value: "Lists 14 open roles", SourceURL: "https://brightwell.example/jobs"}}})
	if err != nil {
		t.Fatal(err)
	}
	queue := readResearch(t, client, srv.URL+"/o/alpha/api/v1/research?filter=review")
	if len(queue.Items) != 1 || !strings.Contains(queue.body, "Lists 14 open roles") || queue.Items[0].Fit.Text != "1 of 2 · 1 unchecked" {
		t.Errorf("Research doesn't show the careers page the company has, and how it fits: %s", queue.body)
	}
	if c := queue.Items[0].Criteria; len(c) != 2 || c[0].Field != "careers_page" || c[0].State != api.CriterionStateMet || c[1].Field != "ats" || c[1].State != api.CriterionStateUnchecked {
		t.Errorf("Research doesn't show the tracking system wasn't checked: %+v", c)
	}
	if strings.Contains(queue.body, "onfigurator") {
		t.Errorf("Research still talks about configurators")
	}
	_, overview := call(t, client, "GET", fmt.Sprintf("%s/o/alpha/api/v1/companies/%d/overview", srv.URL, out.ID), "", nil)
	if !strings.Contains(overview, `"field":"ats","must":false,"state":"unchecked"`) {
		t.Errorf("the company page doesn't show what's missing: %s", overview)
	}
}

func TestOlderAgentsConfiguratorBecomesFacts(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	out, err := s.AgentAddCompany(o.ID, store.Actor{Name: "agent:bot"}, research.AddCompanyIn{Name: "Larkmoor Yachts",
		ConfiguratorURL: "https://larkmoor.example/build", Platform: "Kickflip", BuiltBy: "Studio Vell"})
	if err != nil {
		t.Fatal(err)
	}
	facts := s.companyFacts(t, o.ID, out.ID)
	if facts["configurator"] != "https://larkmoor.example/build" || facts["platform"] != "Kickflip" || facts["built_by"] != "Studio Vell" {
		t.Fatalf("facts: %v", facts)
	}
	corvane, err := s.AgentAddCompany(o.ID, store.Actor{Name: "agent:bot"}, research.AddCompanyIn{Name: "Corvane", Platform: "Kickflip"})
	if err != nil {
		t.Fatalf("an older agent's platform with no configurator link was refused: %v", err)
	}
	var uncited store.Fact
	s.InOrg(o.ID).Where("company_id = ? and field = 'platform'", corvane.ID).First(&uncited)
	if uncited.Value != "Kickflip" || uncited.SourceURL != "" || uncited.Actor != "agent:bot" {
		t.Fatalf("a platform with nothing to cite: %+v", uncited)
	}
	if o, _ = s.OrgBySlug("alpha"); !strings.HasPrefix(o.Settings.LookFor, "configurator:") {
		t.Fatalf("an organisation given configurators doesn't look for them: %q", o.Settings.LookFor)
	}
}

func (s *Store) companyFacts(t *testing.T, org, company int64) map[string]string {
	t.Helper()
	var facts []store.Fact
	if err := s.InOrg(org).Where("company_id = ?", company).Find(&facts).Error; err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range facts {
		out[f.Field] = f.Value
	}
	return out
}

func TestImportedConfiguratorsShow(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	if _, err := s.Import(o.ID, readFixtures(t)); err != nil {
		t.Fatal(err)
	}
	var facts int64
	s.InOrg(o.ID).Model(&store.Fact{}).Where("field = 'configurator'").Count(&facts)
	if o, _ = s.OrgBySlug("alpha"); facts == 0 || !strings.HasPrefix(o.Settings.LookFor, "configurator:") {
		t.Fatalf("%d configurator facts imported, looking for %q", facts, o.Settings.LookFor)
	}
}
