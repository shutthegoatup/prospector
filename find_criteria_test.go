package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const criteriaLookFor = "configurator (must): a product configurator on their site\npayments: takes payment online\ncrm: which CRM they run"

func TestEachCriterionIsMetNotMetOrNotFound(t *testing.T) {
	assets := must(OrgSettings{LookFor: criteriaLookFor}.Assets())
	if len(assets) != 3 || !assets[0].Must || assets[0].Field != "configurator" || assets[1].Must {
		t.Fatalf("a (must) after the name marks a must: %+v", assets)
	}

	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	o.Settings.LookFor = criteriaLookFor
	bot := Actor{Name: "agent:bot"}
	out, err := s.AgentAddCompany(o.ID, bot, AddCompanyIn{Name: "Tidewater Skiffs", Domain: "tidewater.example", Facts: []FactIn{
		{Field: "configurator", Value: "Choose your hull colour on every model page", SourceURL: "https://tidewater.example/models"},
		{Field: "payments", Value: "Order through a dealer; no online payment", SourceURL: "https://tidewater.example/buy", Answer: "no"},
		{Field: "crm", Value: "Looked at 6 pages and 2 job adverts", Answer: "unknown"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	found := assetsFound(o.Settings, must(s.Facts(o.ID, out.ID)))
	if found[0].State != assetMet || found[1].State != assetNotMet || found[2].State != assetNotFound {
		t.Fatalf("the answers: %+v", found)
	}
	if got := fitOf(found).Text(); got != "1 of 3 · 1 not found" {
		t.Fatalf("the fit: %q", got)
	}

	// A later answer stands for the criterion: the agent found the CRM after all.
	if _, err := s.AgentAddFact(o.ID, bot, AddFactIn{Company: "Tidewater Skiffs", FactIn: FactIn{Field: "crm",
		Value: "An advert asks for experience with a named CRM", SourceURL: "https://jobs.example/tidewater/42", Inferred: true}}); err != nil {
		t.Fatal(err)
	}
	found = assetsFound(o.Settings, must(s.Facts(o.ID, out.ID)))
	if found[2].State != assetMet || !found[2].Fact.Inferred || assetHow(found[2].Fact)[:8] != "inferred" {
		t.Fatalf("the newer answer should stand, marked inferred: %+v", found[2])
	}
	if others := otherFacts(o.Settings, must(s.Facts(o.ID, out.ID))); len(others) != 1 || others[0].Answer != answerUnknown {
		t.Fatalf("the answer it replaced should still be kept among the other facts: %+v", others)
	}

	// Said again after a different answer, an earlier answer stands once more.
	payments := func(f FactIn) string {
		t.Helper()
		if _, err := s.AgentAddFact(o.ID, bot, AddFactIn{Company: "Tidewater Skiffs", FactIn: f}); err != nil {
			t.Fatal(err)
		}
		return assetsFound(o.Settings, must(s.Facts(o.ID, out.ID)))[1].State
	}
	paid := FactIn{Field: "payments", Value: "Card deposit at checkout", SourceURL: "https://tidewater.example/checkout"}
	if payments(paid) != assetMet {
		t.Fatal("a yes after a no should stand")
	}
	if payments(FactIn{Field: "Payments", Value: "Order through a dealer; no online payment", SourceURL: "https://tidewater.example/buy", Answer: "no"}) != assetNotMet {
		t.Fatal("the no, said again after a yes, should stand again")
	}
	if payments(paid) != assetMet {
		t.Fatal("the yes, said again word for word after a no, should stand again")
	}
	if payments(paid) != assetMet || len(must(s.Facts(o.ID, out.ID))) != 7 {
		t.Fatalf("the latest answer repeated should add nothing: %d facts", len(must(s.Facts(o.ID, out.ID))))
	}

	for _, bad := range []FactIn{
		{Field: "payments", Value: "No online payment", Answer: "no"},
		{Field: "payments", Value: "Maybe", SourceURL: "https://tidewater.example", Answer: "perhaps"},
		{Field: "crm", Value: "Looked", SourceURL: "javascript:alert(1)", Answer: "unknown"},
	} {
		if _, err := s.AgentAddFact(o.ID, bot, AddFactIn{Company: "Tidewater Skiffs", FactIn: bad}); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}

	k := &APIKey{Scopes: "research:write"}
	if g := agentInstructions(o, k); !strings.Contains(g, "- configurator (must): a product configurator") || !strings.Contains(g, "unknown when you looked and couldn't tell") {
		t.Fatalf("agents aren't told to answer each criterion: %q", g)
	}
}

func TestASearchRanksItsCompaniesByFit(t *testing.T) {
	s, o, srv, client := signedIn(t)
	if code, body := call(t, client, "PATCH", srv.URL+"/o/alpha/api/v1/settings", "application/json",
		map[string]any{"name": "Alpha", "color": "#56c5f4", "look_for": criteriaLookFor}); code != http.StatusOK {
		t.Fatalf("saving what we look for: %d %s", code, body)
	}
	task := must(s.CreateTask(o.ID, "Makers", "Boat makers with a configurator.", "Ada Lovelace"))
	bot := Actor{Name: "agent:bot"}
	yes := func(field, site string) FactIn {
		return FactIn{Field: field, Value: "Has " + field, SourceURL: "https://" + site + "/" + field}
	}
	no := func(field, site string) FactIn {
		return FactIn{Field: field, Value: "No " + field, SourceURL: "https://" + site + "/" + field, Answer: "no"}
	}
	// Added best fit first, so newest first would show them the other way round.
	for _, c := range []struct {
		name  string
		facts []FactIn
	}{
		{"Corran Craft", []FactIn{yes("configurator", "corran.example"), yes("payments", "corran.example")}},
		{"Brine Boats", []FactIn{yes("configurator", "brine.example")}},
		{"Anchor Works", []FactIn{no("configurator", "anchor.example"), yes("payments", "anchor.example"), yes("crm", "anchor.example")}},
	} {
		if _, err := s.AgentAddCompany(o.ID, bot, AddCompanyIn{Name: c.name, Domain: strings.ToLower(strings.Fields(c.name)[0]) + ".example",
			Facts: c.facts, Task: task.ID}); err != nil {
			t.Fatal(err)
		}
	}

	_, ranked := call(t, client, "GET", fmt.Sprintf("%s/o/alpha/research?task=%d", srv.URL, task.ID), "", nil)
	at := func(page, name string) int { return strings.Index(page, ">"+name+"<") }
	if !(at(ranked, "Corran Craft") < at(ranked, "Brine Boats") && at(ranked, "Brine Boats") < at(ranked, "Anchor Works")) || at(ranked, "Corran Craft") < 0 {
		t.Fatal("a search's companies should come best fit first, one missing a must last")
	}
	for _, want := range []string{"Best fit first", "2 of 3 · 1 unchecked", "2 of 3 · misses a must", "not met", "· must"} {
		if !strings.Contains(ranked, want) {
			t.Errorf("the ranked queue is missing %q", want)
		}
	}
	_, all := call(t, client, "GET", srv.URL+"/o/alpha/research", "", nil)
	if at(all, "Anchor Works") > at(all, "Corran Craft") || at(all, "Anchor Works") < 0 || strings.Contains(all, "Best fit first") {
		t.Error("the whole queue should stay newest first")
	}
}

// A database from before answers has facts, and each still says the company has it.
func TestFactsFromBeforeAnswersStillCount(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "old.sqlite")
	if pg := os.Getenv("PROSPECTOR_TEST_PG"); pg != "" {
		dsn = pg
	}
	old := must(Open(dsn))
	if err := old.db.Migrator().DropTable(models...); err != nil {
		t.Fatal(err)
	}
	if err := old.db.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	o := mustOrg(t, old, "alpha")
	co := Company{OrgID: o.ID, Name: "Tidewater Skiffs", Stage: CompanyReview}
	if err := old.db.Create(&co).Error; err != nil {
		t.Fatal(err)
	}
	if err := old.db.Create(&Fact{OrgID: o.ID, CompanyID: co.ID, Field: "configurator", Value: "A hull picker", SourceURL: "https://tidewater.example"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"answer", "inferred"} {
		if err := old.db.Migrator().DropColumn(&Fact{}, column); err != nil {
			t.Fatal(err)
		}
	}

	s := must(Open(dsn))
	found := assetsFound(OrgSettings{LookFor: criteriaLookFor}, must(s.Facts(o.ID, co.ID)))
	if found[0].State != assetMet || found[0].Fact.Inferred {
		t.Fatalf("an older fact should still count as the company having it: %+v", found[0])
	}
}
