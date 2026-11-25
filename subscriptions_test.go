package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMonthsAreAddedWithoutSpillingIntoTheNext(t *testing.T) {
	for _, c := range []struct {
		from string
		n    int
		want string
	}{
		{"2026-01-31", 1, "2026-02-28"}, {"2026-01-31", 2, "2026-03-31"}, {"2024-01-31", 1, "2024-02-29"},
		{"2024-02-29", 12, "2025-02-28"}, {"2024-02-29", 48, "2028-02-29"}, {"2026-01-12", 12, "2027-01-12"},
		{"2026-11-30", 3, "2027-02-28"}, {"2026-05-15", 0, "2026-05-15"},
	} {
		if got := addMonths(mustDay(c.from), c.n).Format(dayLayout); got != c.want {
			t.Errorf("%s plus %d months is %s, want %s", c.from, c.n, got, c.want)
		}
	}
}

func TestWhereASubscriptionStands(t *testing.T) {
	day := func(s string) *string { return &s }
	yearly := Subscription{Start: "2026-01-12", TermMonths: 12, Then: ThenRenews, NoticeDays: 60, Per: PerYear}
	withNotice := func(on string) Subscription { s := yearly; s.NoticeOn = day(on); return s }
	endedEarly := yearly
	endedEarly.EndedOn = day("2026-06-30")
	monthly := Subscription{Start: "2026-01-31", Per: PerMonth}
	longNotice := Subscription{Start: "2026-01-01", Per: PerMonth, NoticeDays: 90}
	longNoticeGiven := longNotice
	longNoticeGiven.NoticeOn = day("2026-01-10")
	leap := Subscription{Start: "2024-02-29", TermMonths: 12, Then: ThenRenews, NoticeDays: 30, Per: PerYear}
	fixed := Subscription{Start: "2026-01-01", TermMonths: 24, Then: ThenEnds, Per: PerYear}
	rolling := Subscription{Start: "2026-01-01", TermMonths: 6, Then: ThenRolls, NoticeDays: 30, Per: PerMonth}

	for _, c := range []struct {
		name string
		sub  Subscription
		on   string
		want Standing
	}{
		{"first term, too late to stop at its end", yearly, "2026-11-25", Standing{SubActive, "2027-01-11", "2027-11-13", "2028-01-11", ""}},
		{"its last day", yearly, "2027-01-11", Standing{SubActive, "2027-01-11", "2027-11-13", "2028-01-11", ""}},
		{"the last day to cancel", yearly, "2026-11-13", Standing{SubActive, "2027-01-11", "2026-11-13", "2027-01-11", ""}},
		{"renewed once", yearly, "2027-01-12", Standing{SubActive, "2028-01-11", "2027-11-13", "2028-01-11", ""}},
		{"renewed several times", yearly, "2030-06-01", Standing{SubActive, "2031-01-11", "2030-11-13", "2031-01-11", ""}},
		{"not started", yearly, "2025-12-01", Standing{SubScheduled, "2027-01-11", "2026-11-13", "2027-01-11", ""}},
		{"notice in time", withNotice("2026-11-13"), "2026-11-25", Standing{SubNotice, "2027-01-11", "", "", "2027-01-11"}},
		{"notice in time, after its end", withNotice("2026-11-13"), "2027-01-12", Standing{SubEnded, "2028-01-11", "", "", "2027-01-11"}},
		{"notice a day late runs another term", withNotice("2026-11-14"), "2026-11-25", Standing{SubNotice, "2027-01-11", "", "", "2028-01-11"}},
		{"ended early, before the day", endedEarly, "2026-06-01", Standing{SubEnding, "2027-01-11", "", "", "2026-06-30"}},
		{"ended early, on the day", endedEarly, "2026-06-30", Standing{SubEnding, "2027-01-11", "", "", "2026-06-30"}},
		{"ended early, after", endedEarly, "2026-07-01", Standing{SubEnded, "2027-01-11", "", "", "2026-06-30"}},
		{"monthly from the 31st, in February", monthly, "2026-02-10", Standing{SubActive, "2026-02-27", "2026-02-27", "2026-02-27", ""}},
		{"monthly from the 31st, back on the 31st", monthly, "2026-03-31", Standing{SubActive, "2026-04-29", "2026-04-29", "2026-04-29", ""}},
		{"notice longer than the term", longNotice, "2026-01-10", Standing{SubActive, "2026-01-31", "2026-01-31", "2026-04-30", ""}},
		{"notice longer than the term, months on", longNotice, "2026-11-25", Standing{SubActive, "2026-11-30", "2026-12-01", "2027-02-28", ""}},
		{"notice longer than the term, given", longNoticeGiven, "2026-01-10", Standing{SubNotice, "2026-01-31", "", "", "2026-04-30"}},
		{"from 29 February", leap, "2027-06-01", Standing{SubActive, "2028-02-28", "2028-01-30", "2028-02-28", ""}},
		{"from 29 February, in the next leap year", leap, "2028-02-29", Standing{SubActive, "2029-02-27", "2029-01-29", "2029-02-27", ""}},
		{"a fixed term", fixed, "2026-06-01", Standing{SubActive, "2027-12-31", "", "", "2027-12-31"}},
		{"a fixed term, over", fixed, "2028-01-01", Standing{SubEnded, "2027-12-31", "", "", "2027-12-31"}},
		{"committed then rolling, in the commitment", rolling, "2026-03-01", Standing{SubActive, "2026-06-30", "2026-06-01", "2026-06-30", ""}},
		{"committed then rolling, after it", rolling, "2026-07-15", Standing{SubActive, "2026-07-31", "2026-08-02", "2026-08-31", ""}},
		{"no start date", Subscription{}, "2026-01-01", Standing{Status: SubScheduled}},
	} {
		if got := c.sub.On(c.on); got != c.want {
			t.Errorf("%s, on %s:\n got %+v\nwant %+v", c.name, c.on, got, c.want)
		}
	}
	if st := yearly.On("2026-11-25"); !st.Missed() || st.Attention() != "2027-01-11" {
		t.Errorf("too late to stop this term, the next thing to know is that it renews: %+v", st)
	}
	if st := yearly.On("2026-10-01"); st.Missed() || st.Attention() != "2026-11-13" {
		t.Errorf("before the day to cancel by, that is what needs attention: %+v", st)
	}
	if st := withNotice("2026-11-13").On("2026-11-25"); st.Attention() != "2027-01-11" {
		t.Errorf("with notice given, the day it stops is what needs attention: %+v", st)
	}
}

func TestASubscriptionIsRecordedAgainstACompany(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	person := repliedPerson(t, s, o, "Hannah Pryce")
	company := *person.CompanyID
	ok := SubscriptionIn{Company: company, Name: "Standard plan, 2 sites", Amount: "14,000", Start: "2026-01-12", TermMonths: 12, NoticeDays: 60}
	for why, bad := range map[string]SubscriptionIn{
		"no name":        {Company: company, Start: "2026-01-12"},
		"no start":       {Company: company, Name: "Plan"},
		"a bad price":    {Company: company, Name: "Plan", Start: "2026-01-12", Amount: "plenty"},
		"a bad period":   {Company: company, Name: "Plan", Start: "2026-01-12", Per: "week"},
		"a bad ending":   {Company: company, Name: "Plan", Start: "2026-01-12", Then: "explodes"},
		"a 50-year term": {Company: company, Name: "Plan", Start: "2026-01-12", TermMonths: 600},
		"no company":     {Company: company + 99, Name: "Plan", Start: "2026-01-12"},
	} {
		if _, err := s.AddSubscription(o.ID, bad, "Ada Lovelace"); err == nil {
			t.Errorf("a subscription with %s was recorded", why)
		}
	}
	sub := must(s.AddSubscription(o.ID, ok, "Ada Lovelace"))
	if sub.Amount != 1400000 || sub.Currency != "GBP" || sub.Per != PerYear || sub.Then != ThenRenews {
		t.Fatalf("the subscription recorded: %+v", sub)
	}
	second := must(s.AddSubscription(o.ID, SubscriptionIn{Company: company, Name: "Support hours", Amount: "300", Per: PerMonth, Start: "2026-03-01"}, "Ada Lovelace"))

	rows := must(s.Subscriptions(o.ID, 0, "2026-10-01"))
	if len(rows) != 2 || rows[0].ID != second.ID || rows[0].Company != "Larkmoor Yachts" || rows[1].CancelBy != "2026-11-13" {
		t.Fatalf("the list on 1 October, soonest first: %+v", rows)
	}
	if got := contracted(rows); got != "£17,600" {
		t.Errorf("contracted for the year: %q, want the yearly one and twelve of the monthly", got)
	}
	if only := must(s.Subscriptions(o.ID, company+99, "2026-10-01")); len(only) != 0 {
		t.Errorf("another company's list: %+v", only)
	}

	change := func(field, value string) error {
		return s.ChangeSubscription(o.ID, sub.ID, field, value, "Ada Lovelace")
	}
	for field, value := range map[string]string{"start": "last week", "term_months": "many", "notice_days": "-1", "per": "week", "then": "explodes",
		"amount": "plenty", "name": "", "notice_on": "2025-01-01", "colour": "red"} {
		if err := change(field, value); err == nil {
			t.Errorf("a subscription's %s was set to %q", field, value)
		}
	}
	for _, step := range [][2]string{{"name", "Standard plan, 3 sites"}, {"amount", "£18,000"}, {"notice_days", "30"}, {"notice_on", "2026-11-25"}} {
		if err := change(step[0], step[1]); err != nil {
			t.Fatalf("setting a subscription's %s: %v", step[0], err)
		}
	}
	rows = must(s.Subscriptions(o.ID, company, "2026-11-26"))
	if got := rows[len(rows)-1]; got.Name != "Standard plan, 3 sites" || got.Price() != "£18,000 a year" || got.Status != SubNotice || got.Ends != "2027-01-11" {
		t.Errorf("after the changes: %+v", got)
	}

	if err := change("start", "2026-12-01"); err == nil {
		t.Error("the start was moved to after notice was given")
	}
	if err := s.ChangeSubscription(o.ID, second.ID, "then", ThenEnds, "Ada Lovelace"); err == nil {
		t.Error("a subscription with nothing committed was told to end at the end of its term")
	}
	if second.Then != ThenRolls {
		t.Errorf("a subscription with nothing committed was recorded as %q", second.Then)
	}

	// The history says what was agreed and when, never the name or the price.
	var said []Event
	if err := s.db.Where("org_id = ? and company_id = ? and action like ?", o.ID, company, "sub:%").Order("id").Find(&said).Error; err != nil {
		t.Fatal(err)
	}
	if len(said) != 6 || deref(said[0].Value) != "2026-01-12" || said[2].Value != nil || said[3].Value != nil || deref(said[5].Value) != "2026-11-25" || said[0].ContactID != 0 {
		t.Errorf("the history of the subscriptions: %+v", said)
	}

	other := mustOrg(t, s, "beta")
	if err := s.ChangeSubscription(other.ID, sub.ID, "name", "Theirs now", "Eve"); err == nil {
		t.Error("another organisation changed the subscription")
	}
	if err := s.RemoveSubscription(other.ID, sub.ID, "Eve"); err == nil {
		t.Error("another organisation removed the subscription")
	}
	if _, err := s.AddSubscription(other.ID, ok, "Eve"); err == nil {
		t.Error("another organisation recorded a subscription against the first one's company")
	}
	if theirs := must(s.Subscriptions(other.ID, 0, "2026-10-01")); len(theirs) != 0 {
		t.Errorf("another organisation sees subscriptions: %+v", theirs)
	}

	// It is the company's, so it outlasts the person, and it is in the organisation's export.
	if err := s.ErasePerson(o.ID, person.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if export := must(s.ExportOrg(o.ID, "Ada Lovelace")); len(export.Subs) != 2 {
		t.Errorf("the export after the person was erased holds %d subscriptions", len(export.Subs))
	}
	if err := s.RemoveSubscription(o.ID, second.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if left := must(s.Subscriptions(o.ID, 0, "2026-10-01")); len(left) != 1 {
		t.Errorf("after removing one: %+v", left)
	}
}

func TestSubscriptionsAreRecordedAndChangedFromThePages(t *testing.T) {
	s, o, srv, client := signedIn(t)
	person := repliedPerson(t, s, o, "Hannah Pryce")
	company := *person.CompanyID
	post := func(path string, body map[string]any) (int, string) {
		return call(t, client, "POST", srv.URL+"/o/alpha/ui/"+path, "application/json", body)
	}
	if code, body := post("subscription", map[string]any{"company": company, "name": "Standard plan", "amount": "14,000", "per": "year",
		"start": "2026-01-12", "term_months": 12, "then": "renews", "notice_days": 60}); code != http.StatusOK {
		t.Fatalf("recording a subscription: %d %s", code, body)
	}
	if code, _ := post("subscription", map[string]any{"company": company, "name": "", "start": "2026-01-12"}); code == http.StatusOK {
		t.Error("a subscription with no name was recorded")
	}
	sub := must(s.Subscriptions(o.ID, 0, today()))[0]
	if code, body := post("subscription/change", map[string]any{"id": sub.ID, "field": "notice_days", "value": "30"}); code != http.StatusOK {
		t.Fatalf("changing a subscription: %d %s", code, body)
	}
	for _, path := range []string{"/o/alpha/subscriptions", "/o/alpha/companies/" + itoa(company)} {
		_, page := call(t, client, "GET", srv.URL+path, "", nil)
		for _, want := range []string{"Standard plan", "£14,000 a year", "12 months, then renews for the same term", `data-sub="` + itoa(sub.ID) + `"`} {
			if !strings.Contains(page, want) {
				t.Errorf("%s doesn't show %q", path, want)
			}
		}
	}
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/subscriptions", "", nil); !strings.Contains(page, "Larkmoor Yachts") || !strings.Contains(page, "a year at today") {
		t.Error("the subscriptions page doesn't name the company or say what is contracted")
	}
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/companies/"+itoa(company), "", nil); !strings.Contains(page, "recorded a subscription: ") || !strings.Contains(page, `data-form="ui/subscription"`) {
		t.Error("the company's page lacks the history line or the form")
	}
	if code, body := post("subscription/remove", map[string]any{"id": sub.ID}); code != http.StatusOK {
		t.Fatalf("removing a subscription: %d %s", code, body)
	}
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/subscriptions", "", nil); !strings.Contains(page, "No subscriptions yet") {
		t.Error("the page after the only subscription was removed")
	}
}

// A database written before subscriptions existed has companies and no such table.
func TestADatabaseFromBeforeSubscriptionsOpens(t *testing.T) {
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
	person := repliedPerson(t, old, o, "Hannah Pryce")
	if err := old.db.Migrator().DropTable(&Subscription{}); err != nil {
		t.Fatal(err)
	}
	s := must(Open(dsn))
	if rows := must(s.Subscriptions(o.ID, 0, "2026-01-01")); len(rows) != 0 {
		t.Fatalf("subscriptions nobody recorded: %+v", rows)
	}
	if _, err := s.AddSubscription(o.ID, SubscriptionIn{Company: *person.CompanyID, Name: "Plan", Start: "2026-01-01"}, "Ada Lovelace"); err != nil {
		t.Errorf("the first subscription after the upgrade: %v", err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
