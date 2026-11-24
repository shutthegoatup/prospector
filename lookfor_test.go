package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestLookForAssets(t *testing.T) {
	got, err := OrgSettings{LookFor: "Careers page: a jobs page on their site\n\nats\ncareers-page: again"}.Assets()
	if err != nil || len(got) != 2 || got[0] != (Asset{"careers_page", "a jobs page on their site"}) || got[1] != (Asset{"ats", ""}) {
		t.Fatalf("assets: %+v %v", got, err)
	}
	if _, err := (OrgSettings{LookFor: ": no name"}).Assets(); err == nil {
		t.Fatal("an asset without a name was accepted")
	}

	s, o, srv, client := signedIn(t)
	save := func(lookFor string) (int, string) {
		return call(t, client, "POST", srv.URL+"/o/alpha/ui/settings", "application/json",
			map[string]any{"name": "Alpha", "color": "#56c5f4", "look_for": lookFor})
	}
	if code, body := save(": a jobs page"); code != http.StatusBadRequest || !strings.Contains(body, "needs a name") {
		t.Fatalf("an unnamed asset was saved: %d %s", code, body)
	}
	if code, body := save("careers_page: a jobs page on their site\nats: the applicant tracking system they use"); code != http.StatusOK {
		t.Fatalf("saving what we look for: %d %s", code, body)
	}
	o, _ = s.OrgBySlug("alpha")
	k := &APIKey{Scopes: "research:write"}
	if g := agentInstructions(o, k); !strings.Contains(g, "- careers_page: a jobs page on their site") || !strings.Contains(g, "- ats: the applicant tracking system") {
		t.Fatalf("agents aren't told what to look for: %q", g)
	}

	out, err := s.AgentAddCompany(o.ID, Actor{Name: "agent:bot"}, AddCompanyIn{Name: "Brightwell Staffing", Domain: "brightwell.example",
		Facts: []FactIn{{Field: "Careers page", Value: "Lists 14 open roles", SourceURL: "https://brightwell.example/jobs"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, page := call(t, client, "GET", srv.URL+"/o/alpha/research?f=review", "", nil)
	if !strings.Contains(page, "careers page") {
		t.Errorf("Research doesn't show the careers page the company has")
	}
	if !strings.Contains(page, "no ats found") {
		t.Errorf("Research doesn't show the tracking system wasn't found")
	}
	if strings.Contains(page, "onfigurator") {
		t.Errorf("Research still talks about configurators")
	}
	if _, page := call(t, client, "GET", fmt.Sprintf("%s/o/alpha/companies/%d", srv.URL, out.ID), "", nil); !strings.Contains(page, "no ats found") {
		t.Errorf("the company page doesn't show what's missing")
	}
}

func TestOlderAgentsConfiguratorBecomesFacts(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	out, err := s.AgentAddCompany(o.ID, Actor{Name: "agent:bot"}, AddCompanyIn{Name: "Larkmoor Yachts",
		ConfiguratorURL: "https://larkmoor.example/build", Platform: "Kickflip", BuiltBy: "Studio Vell"})
	if err != nil {
		t.Fatal(err)
	}
	facts := s.companyFacts(t, o.ID, out.ID)
	if facts["configurator"] != "https://larkmoor.example/build" || facts["platform"] != "Kickflip" || facts["built_by"] != "Studio Vell" {
		t.Fatalf("facts: %v", facts)
	}
	if _, err := s.AgentAddCompany(o.ID, Actor{Name: "agent:bot"}, AddCompanyIn{Name: "Corvane", Platform: "Kickflip"}); err == nil {
		t.Fatal("a platform with nothing to cite was accepted")
	}
}

// TestConfiguratorColumnsMove starts from a database written before assets were the
// organisation's to name, with the configurator columns filled in.
func TestConfiguratorColumnsMove(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "old.sqlite")
	s, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	renders := mustOrg(t, s, "renders")
	named := mustOrg(t, s, "named")
	named.Settings.LookFor = "builder"
	quiet := mustOrg(t, s, "quiet")
	for _, err := range []error{s.SaveOrg(named),
		s.db.Exec(`alter table companies add column configurator_url text`).Error,
		s.db.Exec(`alter table companies add column platform text`).Error,
		s.db.Exec(`alter table companies add column built_by text`).Error} {
		if err != nil {
			t.Fatal(err)
		}
	}
	add := func(org int64, name, domain, url, platform, builtBy string) int64 {
		c := Company{OrgID: org, Name: name, Domain: domain, Stage: CompanyQualified}
		if err := s.db.Create(&c).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.db.Exec(`update companies set configurator_url = ?, platform = ?, built_by = ? where id = ?`, url, platform, builtBy, c.ID).Error; err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	larkmoor := add(renders.ID, "Larkmoor Yachts", "larkmoor.example", "https://larkmoor.example/build", "Kickflip", "Studio Vell")
	ostrel := add(renders.ID, "Ostrel Aviation", "ostrel.example", "", "Threekit", "")
	add(named.ID, "Fjordline Saunas", "fjordline.example", "https://fjordline.example/configure", "", "")
	add(quiet.ID, "Corvane", "corvane.example", "", "", "")
	closeStore(s)

	for i := range 2 {
		if s, err = Open(dsn); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			closeStore(s)
		}
	}
	t.Cleanup(func() { closeStore(s) })
	facts := s.companyFacts(t, renders.ID, larkmoor)
	if len(facts) != 3 || facts["configurator"] != "https://larkmoor.example/build" || facts["platform"] != "Kickflip" || facts["built_by"] != "Studio Vell" {
		t.Fatalf("Larkmoor's facts: %v", facts)
	}
	var cited Fact
	s.db.Where("company_id = ? and field = 'platform'", ostrel).First(&cited)
	if cited.Value != "Threekit" || cited.SourceURL != "https://ostrel.example" {
		t.Fatalf("a platform without a configurator link: %+v", cited)
	}
	var total, left int64
	s.db.Model(&Fact{}).Count(&total)
	s.db.Raw(`select count(*) from companies where configurator_url <> '' or platform <> '' or built_by <> ''`).Scan(&left)
	if total != 6 || left != 0 {
		t.Fatalf("%d facts after starting twice, %d companies still holding the old columns", total, left)
	}
	want := map[string]string{"renders": "configurator: a product configurator or builder on their site", "named": "builder", "quiet": ""}
	for slug, lookFor := range want {
		if o, _ := s.OrgBySlug(slug); o.Settings.LookFor != lookFor {
			t.Errorf("%s looks for %q, want %q", slug, o.Settings.LookFor, lookFor)
		}
	}
}

func (s *Store) companyFacts(t *testing.T, org, company int64) map[string]string {
	t.Helper()
	var facts []Fact
	if err := s.in(org).Where("company_id = ?", company).Find(&facts).Error; err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range facts {
		out[f.Field] = f.Value
	}
	return out
}
