package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/research"
)

// researchQueue is what the Research page lists, read as it reads it.
type researchQueue struct {
	Items  []api.ResearchCompany `json:"items"`
	Ranked bool                  `json:"ranked"`
	body   string
}

func readResearch(t *testing.T, client *http.Client, u string) researchQueue {
	t.Helper()
	code, body := call(t, client, "GET", u, "", nil)
	var q researchQueue
	if code != http.StatusOK || json.Unmarshal([]byte(body), &q) != nil {
		t.Fatalf("GET %s: %d %s", u, code, body)
	}
	q.body = body
	return q
}

func (q researchQueue) names() []string {
	var out []string
	for _, c := range q.Items {
		out = append(out, c.Name)
	}
	return out
}

const criteriaLookFor = "configurator (must): a product configurator on their site\npayments: takes payment online\ncrm: which CRM they run"

func TestEachCriterionIsMetNotMetOrNotFound(t *testing.T) {
	t.Parallel()
	assets := must(OrgSettings{LookFor: criteriaLookFor}.Assets())
	if len(assets) != 3 || !assets[0].Must || assets[0].Field != "configurator" || assets[1].Must {
		t.Fatalf("a (must) after the name marks a must: %+v", assets)
	}

	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	o.Settings.LookFor = criteriaLookFor
	bot := Actor{Name: "agent:bot"}
	out, err := s.AgentAddCompany(o.ID, bot, research.AddCompanyIn{Name: "Tidewater Skiffs", Domain: "tidewater.example", Facts: []research.FactIn{
		{Field: "configurator", Value: "Choose your hull colour on every model page", SourceURL: "https://tidewater.example/models"},
		{Field: "payments", Value: "Order through a dealer; no online payment", SourceURL: "https://tidewater.example/buy", Answer: "no"},
		{Field: "crm", Value: "Looked at 6 pages and 2 job adverts", Answer: "unknown"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	found := prospecting.AssetsFound(o.Settings, must(s.Facts(o.ID, out.ID)))
	if found[0].State != prospecting.AssetMet || found[1].State != prospecting.AssetNotMet || found[2].State != prospecting.AssetNotFound {
		t.Fatalf("the answers: %+v", found)
	}
	if got := prospecting.FitOf(found).Text(); got != "1 of 3 · 1 not found" {
		t.Fatalf("the fit: %q", got)
	}

	// A later answer stands for the criterion: the agent found the CRM after all.
	if _, err := s.AgentAddFact(o.ID, bot, research.AddFactIn{Company: "Tidewater Skiffs", FactIn: research.FactIn{Field: "crm",
		Value: "An advert asks for experience with a named CRM", SourceURL: "https://jobs.example/tidewater/42", Inferred: true}}); err != nil {
		t.Fatal(err)
	}
	found = prospecting.AssetsFound(o.Settings, must(s.Facts(o.ID, out.ID)))
	if found[2].State != prospecting.AssetMet || !found[2].Fact.Inferred {
		t.Fatalf("the newer answer should stand, marked inferred: %+v", found[2])
	}
	if others := research.OtherFacts(o.Settings, must(s.Facts(o.ID, out.ID))); len(others) != 1 || others[0].Answer != prospecting.AnswerUnknown {
		t.Fatalf("the answer it replaced should still be kept among the other facts: %+v", others)
	}

	// Said again after a different answer, an earlier answer stands once more.
	payments := func(f research.FactIn) string {
		t.Helper()
		if _, err := s.AgentAddFact(o.ID, bot, research.AddFactIn{Company: "Tidewater Skiffs", FactIn: f}); err != nil {
			t.Fatal(err)
		}
		return prospecting.AssetsFound(o.Settings, must(s.Facts(o.ID, out.ID)))[1].State
	}
	paid := research.FactIn{Field: "payments", Value: "Card deposit at checkout", SourceURL: "https://tidewater.example/checkout"}
	if payments(paid) != prospecting.AssetMet {
		t.Fatal("a yes after a no should stand")
	}
	if payments(research.FactIn{Field: "Payments", Value: "Order through a dealer; no online payment", SourceURL: "https://tidewater.example/buy", Answer: "no"}) != prospecting.AssetNotMet {
		t.Fatal("the no, said again after a yes, should stand again")
	}
	if payments(paid) != prospecting.AssetMet {
		t.Fatal("the yes, said again word for word after a no, should stand again")
	}
	if payments(paid) != prospecting.AssetMet || len(must(s.Facts(o.ID, out.ID))) != 7 {
		t.Fatalf("the latest answer repeated should add nothing: %d facts", len(must(s.Facts(o.ID, out.ID))))
	}

	for _, bad := range []research.FactIn{
		{Field: "payments", Value: "No online payment", Answer: "no"},
		{Field: "payments", Value: "Maybe", SourceURL: "https://tidewater.example", Answer: "perhaps"},
		{Field: "crm", Value: "Looked", SourceURL: "javascript:alert(1)", Answer: "unknown"},
	} {
		if _, err := s.AgentAddFact(o.ID, bot, research.AddFactIn{Company: "Tidewater Skiffs", FactIn: bad}); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}

	k := &APIKey{Scopes: "research:write"}
	if g := agentInstructions(o, k); !strings.Contains(g, "- configurator (must): a product configurator") || !strings.Contains(g, "unknown when you looked and couldn't tell") {
		t.Fatalf("agents aren't told to answer each criterion: %q", g)
	}
}

func TestASearchRanksItsCompaniesByFit(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	if code, body := call(t, client, "PATCH", srv.URL+"/o/alpha/api/v1/settings", "application/json",
		map[string]any{"name": "Alpha", "color": "#56c5f4", "look_for": criteriaLookFor}); code != http.StatusOK {
		t.Fatalf("saving what we look for: %d %s", code, body)
	}
	task := must(s.CreateTask(o.ID, "Makers", "Boat makers with a configurator.", "Ada Lovelace"))
	bot := Actor{Name: "agent:bot"}
	yes := func(field, site string) research.FactIn {
		return research.FactIn{Field: field, Value: "Has " + field, SourceURL: "https://" + site + "/" + field}
	}
	no := func(field, site string) research.FactIn {
		return research.FactIn{Field: field, Value: "No " + field, SourceURL: "https://" + site + "/" + field, Answer: "no"}
	}
	// Added best fit first, so newest first would show them the other way round.
	for _, c := range []struct {
		name  string
		facts []research.FactIn
	}{
		{"Corran Craft", []research.FactIn{yes("configurator", "corran.example"), yes("payments", "corran.example")}},
		{"Brine Boats", []research.FactIn{yes("configurator", "brine.example")}},
		{"Anchor Works", []research.FactIn{no("configurator", "anchor.example"), yes("payments", "anchor.example"), yes("crm", "anchor.example")}},
	} {
		if _, err := s.AgentAddCompany(o.ID, bot, research.AddCompanyIn{Name: c.name, Domain: strings.ToLower(strings.Fields(c.name)[0]) + ".example",
			Facts: c.facts, Task: task.ID}); err != nil {
			t.Fatal(err)
		}
	}

	ranked := readResearch(t, client, fmt.Sprintf("%s/o/alpha/api/v1/research?task=%d", srv.URL, task.ID))
	if got := strings.Join(ranked.names(), ", "); got != "Corran Craft, Brine Boats, Anchor Works" || !ranked.Ranked {
		t.Fatalf("a search's companies should come best fit first, one missing a must last: %s (ranked %v)", got, ranked.Ranked)
	}
	for _, want := range []string{`"text":"2 of 3 · 1 unchecked"`, `"text":"2 of 3 · misses a must"`} {
		if !strings.Contains(ranked.body, want) {
			t.Errorf("the ranked queue is missing %s", want)
		}
	}
	anchor := ranked.Items[2].Criteria[0]
	if anchor.Field != "configurator" || !anchor.Must || anchor.State != api.CriterionStateNotMet {
		t.Errorf("the must Anchor Works misses: %+v", anchor)
	}
	all := readResearch(t, client, srv.URL+"/o/alpha/api/v1/research")
	if got := strings.Join(all.names(), ", "); got != "Anchor Works, Brine Boats, Corran Craft" || all.Ranked {
		t.Errorf("the whole queue should stay newest first: %s (ranked %v)", got, all.Ranked)
	}
}
