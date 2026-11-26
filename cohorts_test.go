package main

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/api"
)

func rule(field, value string) CohortRule { return CohortRule{Field: field, Value: value} }

func addCohort(t *testing.T, s *Store, org int64, name string, rules ...CohortRule) *Cohort {
	t.Helper()
	return must(s.AddCohort(org, CohortChange{Name: &name, Rules: &rules}, "Ada Lovelace"))
}

func agentCompany(t *testing.T, s *Store, org int64, by, name, segment string) int64 {
	t.Helper()
	return must(s.AgentAddCompany(org, Actor{Name: by}, AddCompanyIn{Name: name, Segment: segment})).ID
}

// inCohort says which cohort a company is in, and why.
func inCohort(t *testing.T, s *Store, org, company int64) (int64, string) {
	t.Helper()
	c := companyRow(t, s, org, company)
	return c.CohortID, c.CohortWhy
}

func cohortMoves(t *testing.T, s *Store, org, company int64) []string {
	t.Helper()
	items := must(s.historyAt(org, company, "company:cohort", 0, 100))
	var out []string
	for _, h := range items {
		out = append(out, h.Actor+": "+h.Value)
	}
	return out
}

func TestCompaniesFallIntoTheFirstCohortWhoseRuleTheyMeet(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	brightwater := agentCompany(t, s, o.ID, "agent:scout", "Brightwater Studio", "CGI studio for carmakers")
	larkmoor := agentCompany(t, s, o.ID, "agent:scout", "Larkmoor Yachts", "Boatbuilder")
	helm := agentCompany(t, s, o.ID, "agent:scout", "Helm & Hull Creative", "Creative agency for manufacturers")
	blank := agentCompany(t, s, o.ID, "agent:scout", "Quillon Works", "")

	for _, id := range []int64{brightwater, larkmoor, helm, blank} {
		if c := companyRow(t, s, o.ID, id); c.CohortID != 0 || c.CohortWhy != "" || len(cohortMoves(t, s, o.ID, id)) > 0 {
			t.Fatalf("with no cohorts a company is in one: %+v", c)
		}
	}

	studios := addCohort(t, s, o.ID, "CGI studios", rule("segment", "studio"))
	if !studios.CatchAll {
		t.Fatal("the first cohort should be the catch-all")
	}
	makers := addCohort(t, s, o.ID, "Manufacturers", rule("segment", "carmaker, manufacturer, OEM, boatbuilder"))
	agencies := addCohort(t, s, o.ID, "Agencies", rule("segment", "agency"))

	var order []string
	for _, c := range must(s.Cohorts(o.ID)) {
		order = append(order, c.Name)
	}
	if strings.Join(order, ", ") != "Manufacturers, Agencies, CGI studios" {
		t.Fatalf("new cohorts go before the catch-all, which stays last: %v", order)
	}
	for _, c := range []struct {
		company, want int64
		why           string
	}{
		{brightwater, studios.ID, `segment "studio"`},
		{larkmoor, makers.ID, `segment "boatbuilder"`},
		{helm, agencies.ID, `segment "agency"`},
		{blank, studios.ID, ""},
	} {
		if got, why := inCohort(t, s, o.ID, c.company); got != c.want || why != c.why {
			t.Errorf("company %d: in %d because %q, want %d because %q", c.company, got, why, c.want, c.why)
		}
	}
	if moves := cohortMoves(t, s, o.ID, larkmoor); len(moves) == 0 || moves[0] != `rules: CGI studios → Manufacturers · segment "boatbuilder"` {
		t.Fatalf("Larkmoor's history of its cohorts: %v", moves)
	}
	if moves := cohortMoves(t, s, o.ID, blank); len(moves) != 1 || !strings.HasSuffix(moves[0], "no rule matched") {
		t.Fatalf("a company the catch-all took should say no rule did: %v", moves)
	}

	// A company two rules take goes to whichever cohort comes first.
	both := agentCompany(t, s, o.ID, "agent:scout", "Ferris OEM agency", "OEM agency")
	if got, _ := inCohort(t, s, o.ID, both); got != makers.ID {
		t.Fatalf("the first cohort in order should take it: %d", got)
	}
	if err := s.MoveCohort(o.ID, agencies.ID, 0, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if got, _ := inCohort(t, s, o.ID, both); got != agencies.ID {
		t.Fatalf("moving a cohort up the order should place its companies again: %d", got)
	}
	if err := s.MoveCohort(o.ID, studios.ID, 0, "Ada Lovelace"); err == nil {
		t.Fatal("the catch-all was moved off the end")
	}
	if err := s.MoveCohort(o.ID, agencies.ID, 9, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if last := must(s.Cohorts(o.ID)); last[len(last)-1].ID != studios.ID {
		t.Fatal("moving a cohort far down put it after the catch-all")
	}

	// The catch-all can be handed on, but there is always one.
	if _, err := s.ChangeCohort(o.ID, studios.ID, CohortChange{CatchAll: new(false)}, "Ada Lovelace"); err == nil {
		t.Fatal("the only catch-all was unmade")
	}
	must(s.ChangeCohort(o.ID, makers.ID, CohortChange{CatchAll: new(true)}, "Ada Lovelace"))
	cohorts := must(s.Cohorts(o.ID))
	if last := cohorts[len(cohorts)-1]; last.ID != makers.ID || !last.CatchAll || slices.ContainsFunc(cohorts[:len(cohorts)-1], func(c Cohort) bool { return c.CatchAll }) {
		t.Fatalf("making a cohort the catch-all should move it last, the only one: %+v", cohorts)
	}
	if got, why := inCohort(t, s, o.ID, blank); got != makers.ID || why != "" {
		t.Fatalf("what no rule takes should follow the catch-all: %d %q", got, why)
	}
	if _, err := s.AddCohort(o.ID, CohortChange{Name: new("agencies")}, "Ada Lovelace"); err == nil {
		t.Fatal("two cohorts with one name")
	}
	if _, err := s.AddCohort(o.ID, CohortChange{Name: new("Pink"), Colour: new("#000000")}, "Ada Lovelace"); err == nil {
		t.Fatal("a colour off the palette")
	}
	if _, err := s.AddCohort(o.ID, CohortChange{Name: new("Odd"), Rules: &[]CohortRule{rule("size", "big")}}, "Ada Lovelace"); err == nil {
		t.Fatal("a rule on a field there is none of")
	}
}

func TestACompanyMovedByHandStaysUntilPutBackToItsRules(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	larkmoor := agentCompany(t, s, o.ID, "agent:scout", "Larkmoor Yachts", "Boatbuilder")
	studios := addCohort(t, s, o.ID, "CGI studios")
	makers := addCohort(t, s, o.ID, "Manufacturers", rule("segment", "boatbuilder"))

	if err := s.SetCompanyCohort(o.ID, larkmoor, studios.ID, "Sam Okafor"); err != nil {
		t.Fatal(err)
	}
	c := companyRow(t, s, o.ID, larkmoor)
	if c.CohortID != studios.ID || !c.CohortByHand || c.CohortWhy != "" {
		t.Fatalf("moved by hand: %+v", c)
	}
	// Neither a change to the rules nor to the company moves it now.
	must(s.ChangeCohort(o.ID, makers.ID, CohortChange{Rules: &[]CohortRule{rule("segment", "boatbuilder, yacht")}}, "Ada Lovelace"))
	must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, AddCompanyIn{Name: "Larkmoor Yachts", Facts: []FactIn{{Field: "press", Value: "new yard", SourceURL: "https://larkmoor.example/news"}}}))
	if _, err := s.PlaceCompanies(o.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := inCohort(t, s, o.ID, larkmoor); got != studios.ID {
		t.Fatalf("a company moved by hand was moved by its rules: %d", got)
	}
	if moves := cohortMoves(t, s, o.ID, larkmoor); moves[0] != "Sam Okafor: Manufacturers → CGI studios · by hand" {
		t.Fatalf("the history should say who moved it by hand: %v", moves)
	}
	p := must(s.PreviewCohort(o.ID, makers.ID, CohortChange{Rules: &[]CohortRule{rule("segment", "boat")}}))
	if len(p.Kept) != 1 || p.Kept[0].Company != "Larkmoor Yachts" || len(p.In) != 0 {
		t.Fatalf("the preview should say who stays because they were moved by hand: %+v", p)
	}

	if err := s.CompanyByRule(o.ID, larkmoor, "Sam Okafor"); err != nil {
		t.Fatal(err)
	}
	c = companyRow(t, s, o.ID, larkmoor)
	if c.CohortID != makers.ID || c.CohortByHand || c.CohortWhy != `segment "boatbuilder"` {
		t.Fatalf("back to by rule: %+v", c)
	}
	if moves := cohortMoves(t, s, o.ID, larkmoor); moves[0] != `Sam Okafor: CGI studios → Manufacturers · segment "boatbuilder"` {
		t.Fatalf("putting it back to its rules: %v", moves)
	}

	beta := mustOrg(t, s, "beta")
	theirs := addCohort(t, s, beta.ID, "Theirs")
	if err := s.SetCompanyCohort(o.ID, larkmoor, theirs.ID, "Sam Okafor"); err == nil {
		t.Fatal("a company was put in another organisation's cohort")
	}
}

// Each thing a rule reads moves a company when it changes, in the same write.
func TestEachChangeARuleReadsPlacesTheCompanyAgain(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	roles := must(s.AddOffering(o.ID, Offering{Name: "Contract roles"}, "Ada Lovelace"))
	everyone := addCohort(t, s, o.ID, "Everyone else")
	big := addCohort(t, s, o.ID, "Big deals", rule("deal_value", "1000000"))
	proposals := addCohort(t, s, o.ID, "In proposal", rule("conversation_stage", "proposal"))
	renewals := addCohort(t, s, o.ID, "Renewing", rule("deal_kind", "renewal"))
	paying := addCohort(t, s, o.ID, "Paying", rule("pays_a_year", "500000"))
	monthly := addCohort(t, s, o.ID, "Monthly", rule("billed", "monthly"))
	contract := addCohort(t, s, o.ID, "Hiring contractors", rule("offering", itoa(roles.ID)))
	german := addCohort(t, s, o.ID, "Germany", rule("country", "de"))
	found := addCohort(t, s, o.ID, "Found by finder", rule("found_by", "agent:finder"))
	at := func(id, want int64) {
		t.Helper()
		if got, why := inCohort(t, s, o.ID, id); got != want {
			t.Fatalf("company %d is in %d (%q), want %d", id, got, why, want)
		}
	}

	hannah := repliedPerson(t, s, o, "Hannah Pryce")
	lark := *hannah.CompanyID
	at(lark, everyone.ID)
	if err := s.Update(o.ID, hannah.ID, "deal_amount", "12,000", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	at(lark, big.ID)
	if _, why := inCohort(t, s, o.ID, lark); why != "open deal of at least £10,000" {
		t.Fatalf("why a deal put it there: %q", why)
	}
	if err := s.Update(o.ID, hannah.ID, "stage", "proposal", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	at(lark, big.ID)
	if err := s.Update(o.ID, hannah.ID, "deal_amount", "5,000", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	at(lark, proposals.ID)
	if err := s.Update(o.ID, hannah.ID, "stage", "lost", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	at(lark, everyone.ID)

	// A deal in another currency is one no amount can be weighed against.
	if err := s.Update(o.ID, hannah.ID, "stage", "meeting", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(o.ID, hannah.ID, "deal_amount", "20,000", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	at(lark, big.ID)
	o.Settings.Currency = "EUR"
	if err := s.ChangeSettings(o, Actor{Name: "Ada Lovelace"}, []string{"currency"}); err != nil {
		t.Fatal(err)
	}
	at(lark, everyone.ID)
	if p := must(s.PreviewCohort(o.ID, big.ID, CohortChange{})); p.OtherCurrency != 1 {
		t.Fatalf("the preview should count the deal it can't weigh: %+v", p)
	}
	o.Settings.Currency = "GBP"
	if err := s.ChangeSettings(o, Actor{Name: "Ada Lovelace"}, []string{"currency"}); err != nil {
		t.Fatal(err)
	}
	at(lark, big.ID)

	priya := must(s.QuickAddPerson(o, QuickAdd{Name: "Priya Shah", Company: "Northwind Payments"}, "Ada Lovelace"))
	northwind := *priya.CompanyID
	if err := s.Mark(o.ID, priya.ID, "replied", today(), "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	deal := must(s.StartDeal(o.ID, priya.ID, "renewal", "Ada Lovelace"))
	at(northwind, renewals.ID)
	if err := s.RemoveDeal(o.ID, deal.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	at(northwind, everyone.ID)

	yearly := must(s.AddSubscription(o.ID, SubscriptionIn{Company: northwind, Name: "Platform", Amount: "6,000", Per: PerYear, Start: today(), TermMonths: 12}, "Ada Lovelace"))
	at(northwind, paying.ID)
	if err := s.ChangeSubscription(o.ID, yearly.ID, "amount", "4,000", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	at(northwind, everyone.ID)
	if err := s.ChangeSubscription(o.ID, yearly.ID, "per", PerMonth, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	at(northwind, paying.ID)
	if err := s.ChangeSubscription(o.ID, yearly.ID, "amount", "10", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	at(northwind, monthly.ID)
	if err := s.RemoveSubscription(o.ID, yearly.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	at(northwind, everyone.ID)

	quillon := agentCompany(t, s, o.ID, "agent:scout", "Quillon", "Payments")
	suggested := must(s.AgentAddContact(o, Actor{Name: "agent:scout"}, AddContactIn{Company: itoa(quillon), Name: "Bram Osei", SourceURL: "https://quillon.example/team"}))
	at(quillon, everyone.ID)
	if err := s.PickContactFor(o.ID, o, suggested.ID, &roles.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	at(quillon, contract.ID)
	if _, why := inCohort(t, s, o.ID, quillon); why != `offering "Contract roles"` {
		t.Fatalf("why an offering put it there: %q", why)
	}
	if err := s.RemoveOffering(o.ID, roles.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	at(quillon, everyone.ID)
	if c := must(s.cohort(o.ID, contract.ID)); len(c.Rules) != 0 {
		t.Fatalf("a rule still names the offering removed: %+v", c.Rules)
	}

	must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, AddCompanyIn{Name: "Eisenwerk", Country: "DE"}))
	at(must(s.companyRef(o.ID, "Eisenwerk")).ID, german.ID)
	at(agentCompany(t, s, o.ID, "agent:finder", "Ferris Aero", "Aircraft maker"), found.ID)

	// Erasing the person whose deal put it there places the company again.
	if err := s.ErasePerson(o.ID, hannah.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	at(lark, everyone.ID)
}

// What changes with time, such as a subscription ending, nothing writes: the sweep
// catches it.
func TestTheSweepPlacesWhatTimeHasChanged(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	beta := mustOrg(t, s, "beta")
	everyone := addCohort(t, s, o.ID, "Everyone else")
	monthly := addCohort(t, s, o.ID, "Monthly", rule("billed", "monthly"))
	addCohort(t, s, beta.ID, "Theirs")
	company := agentCompany(t, s, o.ID, "agent:scout", "Northwind Payments", "Payments")
	sub := must(s.AddSubscription(o.ID, SubscriptionIn{Company: company, Name: "Platform", Amount: "100", Per: PerMonth, Start: "2025-01-01"}, "Ada Lovelace"))
	if got, _ := inCohort(t, s, o.ID, company); got != monthly.ID {
		t.Fatalf("a live monthly subscription: %d", got)
	}
	// As if the day it ended has since come and gone.
	if err := s.db.Model(&Subscription{}).Where("id = ?", sub.ID).Update("ended_on", time.Now().AddDate(0, 0, -1).Format(dayLayout)).Error; err != nil {
		t.Fatal(err)
	}
	if got, _ := inCohort(t, s, o.ID, company); got != monthly.ID {
		t.Fatal("the test moved it already")
	}
	s.placeEverywhere()
	if got, _ := inCohort(t, s, o.ID, company); got != everyone.ID {
		t.Fatalf("the sweep left a company whose subscription ended: %d", got)
	}
	if n := must(s.PlaceCompanies(o.ID)); n != 0 {
		t.Fatalf("a second sweep moved %d", n)
	}
	if moves := cohortMoves(t, s, o.ID, company); len(moves) != 3 {
		t.Fatalf("each move is in its history, and nothing else: %v", moves)
	}
}

func TestAPreviewSaysWhatSavingDoes(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	brightwater := agentCompany(t, s, o.ID, "agent:scout", "Brightwater Studio", "CGI studio for carmakers")
	larkmoor := agentCompany(t, s, o.ID, "agent:scout", "Larkmoor Yachts", "Boatbuilder")
	ferris := agentCompany(t, s, o.ID, "agent:scout", "Ferris Aero", "Aircraft maker")
	helm := agentCompany(t, s, o.ID, "agent:scout", "Helm & Hull", "Agency")
	studios := addCohort(t, s, o.ID, "CGI studios")
	makers := addCohort(t, s, o.ID, "Manufacturers", rule("segment", "boatbuilder, studio"))
	if err := s.SetCompanyCohort(o.ID, helm, studios.ID, "Sam Okafor"); err != nil {
		t.Fatal(err)
	}
	where := func() map[int64]int64 {
		out := map[int64]int64{}
		for _, id := range []int64{brightwater, larkmoor, ferris, helm} {
			out[id], _ = inCohort(t, s, o.ID, id)
		}
		return out
	}
	before := where()
	events := func() int64 {
		var n int64
		s.in(o.ID).Model(&Event{}).Count(&n)
		return n
	}
	logged := events()

	change := CohortChange{Name: new("Makers"), Rules: &[]CohortRule{rule("segment", "boatbuilder, aircraft maker, agency")}}
	p := must(s.PreviewCohort(o.ID, makers.ID, change))
	names := func(rows []CohortMoveRow) (out []string) {
		for _, r := range rows {
			out = append(out, r.Company+" "+r.FromName+"→"+r.ToName)
		}
		return out
	}
	if in, out, kept := names(p.In), names(p.Out), names(p.Kept); strings.Join(in, "; ") != "Ferris Aero CGI studios→Makers" ||
		strings.Join(out, "; ") != "Brightwater Studio Makers→CGI studios" || strings.Join(kept, "; ") != "Helm & Hull CGI studios→Makers" {
		t.Fatalf("the preview: in %v, out %v, kept %v", in, out, kept)
	}
	if now := where(); !mapsEqual(now, before) || events() != logged || must(s.cohort(o.ID, makers.ID)).Name != "Manufacturers" {
		t.Fatalf("a preview changed something: %v %v", before, now)
	}

	must(s.ChangeCohort(o.ID, makers.ID, change, "Ada Lovelace"))
	after := where()
	for id, was := range before {
		moved := after[id] != was
		previewed := slices.ContainsFunc(append(p.In, p.Out...), func(r CohortMoveRow) bool { return r.CompanyID == id })
		if moved != previewed {
			t.Errorf("company %d: moved %v, previewed %v", id, moved, previewed)
		}
	}

	// A cohort not yet saved previews too, and saving it does the same.
	fresh := CohortChange{Name: new("Agencies"), Rules: &[]CohortRule{rule("segment", "agency, studio")}}
	p = must(s.PreviewCohort(o.ID, 0, fresh))
	if len(p.In) != 1 || p.In[0].Company != "Brightwater Studio" || p.In[0].To != 0 || len(p.Kept) != 0 || p.Cohort.ID != 0 {
		t.Fatalf("previewing a new cohort: %+v", p)
	}
	if len(must(s.Cohorts(o.ID))) != 2 {
		t.Fatal("previewing a new cohort added it")
	}
	agencies := must(s.AddCohort(o.ID, fresh, "Ada Lovelace"))
	if got, _ := inCohort(t, s, o.ID, brightwater); got != agencies.ID {
		t.Fatalf("saving didn't do what the preview said: %d", got)
	}
}

func TestARestorePlacesCompaniesAgain(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	everyone := addCohort(t, s, o.ID, "Everyone else")
	proposals := addCohort(t, s, o.ID, "In proposal", rule("conversation_stage", "proposal"))
	hannah := repliedPerson(t, s, o, "Hannah Pryce")
	if err := s.Update(o.ID, hannah.ID, "stage", "proposal", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	snapshot := must(s.SnapshotText(o.ID))
	if err := s.Update(o.ID, hannah.ID, "stage", "lost", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if got, _ := inCohort(t, s, o.ID, *hannah.CompanyID); got != everyone.ID {
		t.Fatalf("a lost conversation: %d", got)
	}
	must(s.Restore(o.ID, snapshot))
	if got, _ := inCohort(t, s, o.ID, *hannah.CompanyID); got != proposals.ID {
		t.Fatalf("restoring the conversation at proposal should place its company again: %d", got)
	}
}

func mapsEqual(a, b map[int64]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestRemovingACohortPlacesItsCompaniesAgain(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	larkmoor := agentCompany(t, s, o.ID, "agent:scout", "Larkmoor Yachts", "Boatbuilder")
	helm := agentCompany(t, s, o.ID, "agent:scout", "Helm & Hull", "Agency")
	studios := addCohort(t, s, o.ID, "CGI studios")
	makers := addCohort(t, s, o.ID, "Manufacturers", rule("segment", "boatbuilder"))
	agencies := addCohort(t, s, o.ID, "Agencies", rule("segment", "agency"))
	if err := s.SetCompanyCohort(o.ID, larkmoor, agencies.ID, "Sam Okafor"); err != nil {
		t.Fatal(err)
	}

	if err := s.RemoveCohort(o.ID, agencies.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if c := companyRow(t, s, o.ID, larkmoor); c.CohortID != makers.ID || c.CohortByHand {
		t.Fatalf("a company put by hand in a cohort removed should go where its rules say: %+v", c)
	}
	if got, why := inCohort(t, s, o.ID, helm); got != studios.ID || why != "" {
		t.Fatalf("a company whose cohort was removed: %d %q", got, why)
	}
	if moves := cohortMoves(t, s, o.ID, helm); moves[0] != "rules: Agencies → CGI studios · no rule matched" {
		t.Fatalf("the history should name the cohort removed: %v", moves)
	}
	var rules int64
	s.in(o.ID).Model(&CohortRule{}).Where("cohort_id = ?", agencies.ID).Count(&rules)
	if rules != 0 {
		t.Fatal("a removed cohort's rules stayed")
	}

	if err := s.RemoveCohort(o.ID, studios.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	left := must(s.Cohorts(o.ID))
	if len(left) != 1 || !left[0].CatchAll {
		t.Fatalf("removing the catch-all should make the one left it: %+v", left)
	}
	if got, _ := inCohort(t, s, o.ID, helm); got != makers.ID {
		t.Fatalf("the new catch-all should take what no rule does: %d", got)
	}

	if err := s.RemoveCohort(o.ID, makers.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{larkmoor, helm} {
		if c := companyRow(t, s, o.ID, id); c.CohortID != 0 || c.CohortWhy != "" || c.CohortByHand {
			t.Fatalf("with no cohorts left a company is still in one: %+v", c)
		}
	}
	if moves := cohortMoves(t, s, o.ID, helm); !strings.HasPrefix(moves[0], "rules: Manufacturers → no cohort") {
		t.Fatalf("the last move: %v", moves)
	}
}

// A database from before cohorts, with companies in it, comes up with every company in
// none, and nothing placed until a cohort is added.
func TestExistingCompaniesAreInNoCohortAfterTheUpgrade(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	larkmoor := agentCompany(t, s, o.ID, "agent:scout", "Larkmoor Yachts", "Boatbuilder")
	for _, column := range []string{"cohort_id", "cohort_by_hand", "cohort_why"} {
		if err := s.db.Migrator().DropColumn(&Company{}, column); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.db.AutoMigrate(&Company{}); err != nil {
		t.Fatalf("adding the columns to a table with rows: %v", err)
	}
	if c := companyRow(t, s, o.ID, larkmoor); c.CohortID != 0 || c.CohortByHand || c.CohortWhy != "" || c.Segment != "Boatbuilder" {
		t.Fatalf("an existing company after the upgrade: %+v", c)
	}
	if n := must(s.PlaceCompanies(o.ID)); n != 0 || len(cohortMoves(t, s, o.ID, larkmoor)) != 0 {
		t.Fatal("placing with no cohorts moved something")
	}
	if rows := must(s.Cohorts(o.ID)); len(rows) != 0 {
		t.Fatalf("cohorts out of nowhere: %+v", rows)
	}
	makers := addCohort(t, s, o.ID, "Manufacturers", rule("segment", "boatbuilder"))
	if got, _ := inCohort(t, s, o.ID, larkmoor); got != makers.ID {
		t.Fatalf("adding a cohort should place the companies already here: %d", got)
	}
}

func TestCohortsThroughTheAPI(t *testing.T) {
	s, o, srv, ada := signedIn(t)
	key := func(scopes ...string) string {
		secret, _, err := s.CreateKey(o.ID, "robot", scopes, "Ada Lovelace")
		if err != nil {
			t.Fatal(err)
		}
		return secret
	}
	owner, reviewer, reader := key("settings:write"), key("research:review", "settings:write"), key("read", "research:write")
	base := srv.URL + "/o/alpha/api/v1"
	larkmoor := agentCompany(t, s, o.ID, "agent:scout", "Larkmoor Yachts", "Boatbuilder")

	code, body := asKey(t, "POST", base+"/cohorts", owner, map[string]any{"name": "CGI studios", "who_they_are": "Studios making renders for others"})
	var studios api.Cohort
	if json.Unmarshal([]byte(body), &studios); code != http.StatusCreated || !studios.CatchAll || studios.Colour != CohortColours[0] {
		t.Fatalf("adding a cohort: %d %s", code, body)
	}
	code, body = asKey(t, "POST", base+"/cohorts", owner, map[string]any{"name": "Manufacturers", "who_they_are": "Makers of boats and cars with options to configure",
		"rules": []map[string]any{{"field": "segment", "value": "boatbuilder, carmaker"}}})
	var makers api.Cohort
	if json.Unmarshal([]byte(body), &makers); code != http.StatusCreated || makers.Companies != 1 || makers.Colour == studios.Colour || len(makers.Rules) != 1 || makers.Rules[0].Op != "any_of" {
		t.Fatalf("adding a cohort with a rule: %d %s", code, body)
	}
	if code, body := asKey(t, "POST", base+"/cohorts", reader, map[string]any{"name": "Agencies"}); code != http.StatusForbidden {
		t.Fatalf("a key without settings:write added a cohort: %d %s", code, body)
	}
	code, body = asKey(t, "POST", base+"/cohorts/preview", reader, map[string]any{"cohort": makers.Id, "change": map[string]any{"rules": []any{}}})
	var p api.CohortPreview
	if json.Unmarshal([]byte(body), &p); code != http.StatusOK || len(p.MovingOut) != 1 || p.MovingOut[0].Company != "Larkmoor Yachts" || p.MovingOut[0].To.Name != "CGI studios" {
		t.Fatalf("previewing: %d %s", code, body)
	}

	// Agents read a company's cohort; only a person moves it by hand.
	put := base + "/companies/" + itoa(larkmoor) + "/cohort"
	if code, body := asKey(t, "PUT", put, reviewer, map[string]any{"cohort": studios.Id}); code != http.StatusForbidden || !strings.Contains(body, "no key can move a company") {
		t.Fatalf("a key moved a company by hand: %d %s", code, body)
	}
	if code, body := call(t, ada, "PUT", put, "application/json", map[string]any{"cohort": studios.Id}); code != http.StatusNoContent {
		t.Fatalf("a member moving a company by hand: %d %s", code, body)
	}
	code, body = asKey(t, "GET", base+"/companies/"+itoa(larkmoor)+"/overview", reader, nil)
	var c api.CompanyOverview
	if json.Unmarshal([]byte(body), &c); code != http.StatusOK || c.Cohort.Name != "CGI studios" || !c.CohortByHand || c.Cohort.Colour != studios.Colour {
		t.Fatalf("the company's cohort: %d %s", code, body)
	}
	code, body = asKey(t, "GET", base+"/companies/"+itoa(larkmoor)+"/history?action=company:cohort", reader, nil)
	var h struct {
		Items []api.HistoryEntry
		Total int
	}
	if json.Unmarshal([]byte(body), &h); code != http.StatusOK || h.Total != 3 || h.Items[0].Actor != "Ada Lovelace" || !strings.Contains(h.Items[0].Text, "by hand") {
		t.Fatalf("the company's moves: %d %s", code, body)
	}
	if code, body := call(t, ada, "PUT", put, "application/json", map[string]any{"by_rule": true}); code != http.StatusNoContent {
		t.Fatalf("back to by rule: %d %s", code, body)
	}
	if code, body := call(t, ada, "PUT", put, "application/json", map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("neither a cohort nor by rule: %d %s", code, body)
	}
	code, body = asKey(t, "GET", base+"/companies", reader, nil)
	var list struct{ Items []api.CompanyRow }
	if json.Unmarshal([]byte(body), &list); code != http.StatusOK || len(list.Items) != 1 || list.Items[0].Cohort.Id != makers.Id ||
		list.Items[0].CohortWhy != `segment "boatbuilder"` || list.Items[0].CohortByHand {
		t.Fatalf("the companies list: %d %s", code, body)
	}

	code, body = asKey(t, "PATCH", base+"/cohorts/"+itoa(makers.Id), owner, map[string]any{"name": "Makers"})
	if json.Unmarshal([]byte(body), &makers); code != http.StatusOK || makers.Name != "Makers" || len(makers.Rules) != 1 {
		t.Fatalf("renaming keeps the rules: %d %s", code, body)
	}
	if code, body := asKey(t, "PUT", base+"/cohorts/"+itoa(studios.Id)+"/position", owner, map[string]any{"position": 0}); code != http.StatusBadRequest {
		t.Fatalf("moving the catch-all: %d %s", code, body)
	}

	// Research agents are told what each cohort is, so the segments they write match.
	k := must(s.KeyFor(reader))
	if g := agentInstructions(o, k, must(s.Cohorts(o.ID))); !strings.Contains(g, "- Makers: Makers of boats and cars") || !strings.Contains(g, "Start each company's segment with what the company is") {
		t.Fatalf("the agent's guidance: %s", g)
	}

	grace := mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	_ = grace
	asGrace := signInAs(t, srv, "grace@example.com", "correct horse battery")
	if code, _ := call(t, asGrace, "POST", base+"/cohorts", "application/json", map[string]any{"name": "Agencies"}); code != http.StatusForbidden {
		t.Fatalf("a member who isn't an owner added a cohort: %d", code)
	}

	if code, _ := asKey(t, "DELETE", base+"/cohorts/"+itoa(makers.Id), owner, nil); code != http.StatusNoContent {
		t.Fatalf("removing a cohort: %d", code)
	}
	ex := must(s.ExportOrg(o.ID, "Ada Lovelace"))
	if len(ex.Cohorts) != 1 || ex.Cohorts[0].Name != "CGI studios" || ex.Companies[0].CohortID != studios.Id {
		t.Fatalf("the export: %+v %+v", ex.Cohorts, ex.Companies)
	}
}
