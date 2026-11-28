package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/sales"
	"github.com/shutthegoatup/prospector/internal/store"
)

func TestMonthsAreAddedWithoutSpillingIntoTheNext(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		from string
		n    int
		want string
	}{
		{"2026-01-31", 1, "2026-02-28"}, {"2026-01-31", 2, "2026-03-31"}, {"2024-01-31", 1, "2024-02-29"},
		{"2024-02-29", 12, "2025-02-28"}, {"2024-02-29", 48, "2028-02-29"}, {"2026-01-12", 12, "2027-01-12"},
		{"2026-11-30", 3, "2027-02-28"}, {"2026-05-15", 0, "2026-05-15"},
	} {
		if got := store.AddMonths(store.MustDay(c.from), c.n).Format(store.DayLayout); got != c.want {
			t.Errorf("%s plus %d months is %s, want %s", c.from, c.n, got, c.want)
		}
	}
}

func TestWhereASubscriptionStands(t *testing.T) {
	t.Parallel()
	day := func(s string) *string { return &s }
	yearly := store.Subscription{Start: "2026-01-12", TermMonths: 12, Then: sales.ThenRenews, NoticeDays: 60, Per: store.PerYear}
	withNotice := func(on string) store.Subscription { s := yearly; s.NoticeOn = day(on); return s }
	endedEarly := yearly
	endedEarly.EndedOn = day("2026-06-30")
	monthly := store.Subscription{Start: "2026-01-31", Per: store.PerMonth}
	longNotice := store.Subscription{Start: "2026-01-01", Per: store.PerMonth, NoticeDays: 90}
	longNoticeGiven := longNotice
	longNoticeGiven.NoticeOn = day("2026-01-10")
	leap := store.Subscription{Start: "2024-02-29", TermMonths: 12, Then: sales.ThenRenews, NoticeDays: 30, Per: store.PerYear}
	fixed := store.Subscription{Start: "2026-01-01", TermMonths: 24, Then: store.ThenEnds, Per: store.PerYear}
	rolling := store.Subscription{Start: "2026-01-01", TermMonths: 6, Then: store.ThenRolls, NoticeDays: 30, Per: store.PerMonth}

	for _, c := range []struct {
		name string
		sub  store.Subscription
		on   string
		want store.Standing
	}{
		{"first term, too late to stop at its end", yearly, "2026-11-25", store.Standing{Status: store.SubActive, TermEnd: "2027-01-11", CancelBy: "2027-11-13", Out: "2028-01-11", Ends: ""}},
		{"its last day", yearly, "2027-01-11", store.Standing{Status: store.SubActive, TermEnd: "2027-01-11", CancelBy: "2027-11-13", Out: "2028-01-11", Ends: ""}},
		{"the last day to cancel", yearly, "2026-11-13", store.Standing{Status: store.SubActive, TermEnd: "2027-01-11", CancelBy: "2026-11-13", Out: "2027-01-11", Ends: ""}},
		{"renewed once", yearly, "2027-01-12", store.Standing{Status: store.SubActive, TermEnd: "2028-01-11", CancelBy: "2027-11-13", Out: "2028-01-11", Ends: ""}},
		{"renewed several times", yearly, "2030-06-01", store.Standing{Status: store.SubActive, TermEnd: "2031-01-11", CancelBy: "2030-11-13", Out: "2031-01-11", Ends: ""}},
		{"not started", yearly, "2025-12-01", store.Standing{Status: store.SubScheduled, TermEnd: "2027-01-11", CancelBy: "2026-11-13", Out: "2027-01-11", Ends: ""}},
		{"notice in time", withNotice("2026-11-13"), "2026-11-25", store.Standing{Status: store.SubNotice, TermEnd: "2027-01-11", CancelBy: "", Out: "", Ends: "2027-01-11"}},
		{"notice in time, after its end", withNotice("2026-11-13"), "2027-01-12", store.Standing{Status: store.SubEnded, TermEnd: "2028-01-11", CancelBy: "", Out: "", Ends: "2027-01-11"}},
		{"notice a day late runs another term", withNotice("2026-11-14"), "2026-11-25", store.Standing{Status: store.SubNotice, TermEnd: "2027-01-11", CancelBy: "", Out: "", Ends: "2028-01-11"}},
		{"ended early, before the day", endedEarly, "2026-06-01", store.Standing{Status: store.SubEnding, TermEnd: "2027-01-11", CancelBy: "", Out: "", Ends: "2026-06-30"}},
		{"ended early, on the day", endedEarly, "2026-06-30", store.Standing{Status: store.SubEnding, TermEnd: "2027-01-11", CancelBy: "", Out: "", Ends: "2026-06-30"}},
		{"ended early, after", endedEarly, "2026-07-01", store.Standing{Status: store.SubEnded, TermEnd: "2027-01-11", CancelBy: "", Out: "", Ends: "2026-06-30"}},
		{"monthly from the 31st, in February", monthly, "2026-02-10", store.Standing{Status: store.SubActive, TermEnd: "2026-02-27", CancelBy: "2026-02-27", Out: "2026-02-27", Ends: ""}},
		{"monthly from the 31st, back on the 31st", monthly, "2026-03-31", store.Standing{Status: store.SubActive, TermEnd: "2026-04-29", CancelBy: "2026-04-29", Out: "2026-04-29", Ends: ""}},
		{"notice longer than the term", longNotice, "2026-01-10", store.Standing{Status: store.SubActive, TermEnd: "2026-01-31", CancelBy: "2026-01-31", Out: "2026-04-30", Ends: ""}},
		{"notice longer than the term, months on", longNotice, "2026-11-25", store.Standing{Status: store.SubActive, TermEnd: "2026-11-30", CancelBy: "2026-12-01", Out: "2027-02-28", Ends: ""}},
		{"notice longer than the term, given", longNoticeGiven, "2026-01-10", store.Standing{Status: store.SubNotice, TermEnd: "2026-01-31", CancelBy: "", Out: "", Ends: "2026-04-30"}},
		{"from 29 February", leap, "2027-06-01", store.Standing{Status: store.SubActive, TermEnd: "2028-02-28", CancelBy: "2028-01-30", Out: "2028-02-28", Ends: ""}},
		{"from 29 February, in the next leap year", leap, "2028-02-29", store.Standing{Status: store.SubActive, TermEnd: "2029-02-27", CancelBy: "2029-01-29", Out: "2029-02-27", Ends: ""}},
		{"a fixed term", fixed, "2026-06-01", store.Standing{Status: store.SubActive, TermEnd: "2027-12-31", CancelBy: "", Out: "", Ends: "2027-12-31"}},
		{"a fixed term, over", fixed, "2028-01-01", store.Standing{Status: store.SubEnded, TermEnd: "2027-12-31", CancelBy: "", Out: "", Ends: "2027-12-31"}},
		{"committed then rolling, in the commitment", rolling, "2026-03-01", store.Standing{Status: store.SubActive, TermEnd: "2026-06-30", CancelBy: "2026-06-01", Out: "2026-06-30", Ends: ""}},
		{"committed then rolling, after it", rolling, "2026-07-15", store.Standing{Status: store.SubActive, TermEnd: "2026-07-31", CancelBy: "2026-08-02", Out: "2026-08-31", Ends: ""}},
		{"no start date", store.Subscription{}, "2026-01-01", store.Standing{Status: store.SubScheduled}},
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
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	person := repliedPerson(t, s, o, "Hannah Pryce")
	company := *person.CompanyID
	ok := sales.SubscriptionIn{Company: company, Name: "Standard plan, 2 sites", Amount: "14,000", Start: "2026-01-12", TermMonths: 12, NoticeDays: 60}
	for why, bad := range map[string]sales.SubscriptionIn{
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
	if sub.Amount != 1400000 || sub.Currency != "GBP" || sub.Per != store.PerYear || sub.Then != sales.ThenRenews {
		t.Fatalf("the subscription recorded: %+v", sub)
	}
	second := must(s.AddSubscription(o.ID, sales.SubscriptionIn{Company: company, Name: "Support hours", Amount: "300", Per: store.PerMonth, Start: "2026-03-01"}, "Ada Lovelace"))

	rows := must(s.Subscriptions(o.ID, 0, "2026-10-01"))
	if len(rows) != 2 || rows[0].ID != second.ID || rows[0].Company != "Larkmoor Yachts" || rows[1].CancelBy != "2026-11-13" {
		t.Fatalf("the list on 1 October, soonest first: %+v", rows)
	}
	if got := sales.Contracted(rows); got != "£17,600" {
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
	if got := rows[len(rows)-1]; got.Name != "Standard plan, 3 sites" || got.Price() != "£18,000 a year" || got.Status != store.SubNotice || got.Ends != "2027-01-11" {
		t.Errorf("after the changes: %+v", got)
	}

	if err := change("start", "2026-12-01"); err == nil {
		t.Error("the start was moved to after notice was given")
	}
	if err := s.ChangeSubscription(o.ID, second.ID, "then", store.ThenEnds, "Ada Lovelace"); err == nil {
		t.Error("a subscription with nothing committed was told to end at the end of its term")
	}
	if second.Then != store.ThenRolls {
		t.Errorf("a subscription with nothing committed was recorded as %q", second.Then)
	}

	// The history says what was agreed and when, never the name or the price.
	var said []store.Event
	if err := s.DB.Where("org_id = ? and company_id = ? and action like ?", o.ID, company, "sub:%").Order("id").Find(&said).Error; err != nil {
		t.Fatal(err)
	}
	if len(said) != 6 || store.Deref(said[0].Value) != "2026-01-12" || said[2].Value != nil || said[3].Value != nil || store.Deref(said[5].Value) != "2026-11-25" || said[0].ContactID != 0 {
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
	t.Parallel()
	s, o, srv, client := signedIn(t)
	person := repliedPerson(t, s, o, "Hannah Pryce")
	company := *person.CompanyID
	subs := srv.URL + "/o/alpha/api/v1/subscriptions"
	if code, body := call(t, client, "POST", subs, "application/json", map[string]any{"company": company, "name": "Standard plan", "amount": "14,000", "per": "year",
		"start": "2026-01-12", "term_months": 12, "then": "renews", "notice_days": 60}); code != http.StatusCreated {
		t.Fatalf("recording a subscription: %d %s", code, body)
	}
	if code, _ := call(t, client, "POST", subs, "application/json", map[string]any{"company": company, "name": "", "start": "2026-01-12"}); code < 300 {
		t.Error("a subscription with no name was recorded")
	}
	sub := must(s.Subscriptions(o.ID, 0, store.Today()))[0]
	if code, body := call(t, client, "PATCH", subs+"/"+itoa(sub.ID), "application/json", map[string]any{"notice_days": 30}); code != http.StatusOK {
		t.Fatalf("changing a subscription: %d %s", code, body)
	}
	// The subscriptions page reads every one; a company's page reads its own.
	for _, query := range []string{"", "?company=" + itoa(company)} {
		list, _ := readSubs(t, client, srv.URL, query)
		if len(list) != 1 || list[0].Id != sub.ID || list[0].Name != "Standard plan" || list[0].Figure != "£14,000" || list[0].Per != "year" ||
			list[0].TermMonths != 12 || list[0].Then != "renews" || list[0].NoticeDays != 30 || list[0].Company != "Larkmoor Yachts" {
			t.Errorf("the subscriptions%s: %+v", query, list)
		}
	}
	if _, contracted := readSubs(t, client, srv.URL, ""); contracted == "" {
		t.Error("the subscriptions don't say what is contracted a year")
	}
	if _, history := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/companies/"+itoa(company)+"/history", "", nil); !strings.Contains(history, "recorded a subscription: ") {
		t.Errorf("the company's history lacks the subscription: %s", history)
	}
	if code, body := call(t, client, "DELETE", subs+"/"+itoa(sub.ID), "", nil); code != http.StatusNoContent {
		t.Fatalf("removing a subscription: %d %s", code, body)
	}
	if list, _ := readSubs(t, client, srv.URL, ""); len(list) != 0 {
		t.Errorf("after the only subscription was removed: %+v", list)
	}
}

// readSubs is the subscriptions as the app reads them, and what they are worth a year.
func readSubs(t *testing.T, client *http.Client, site, query string) ([]api.Subscription, string) {
	t.Helper()
	code, body := call(t, client, "GET", site+"/o/alpha/api/v1/subscriptions"+query, "", nil)
	var out struct {
		Items      []api.Subscription
		Contracted string
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &out) != nil {
		t.Fatalf("the subscriptions: %d %s", code, body)
	}
	return out.Items, out.Contracted
}

// dueSubs is how many subscriptions Today points at.
func dueSubs(t *testing.T, client *http.Client, site string) int {
	t.Helper()
	code, body := call(t, client, "GET", site+"/o/alpha/api/v1/today/summary", "", nil)
	var sum api.TodaySummary
	if code != http.StatusOK || json.Unmarshal([]byte(body), &sum) != nil {
		t.Fatalf("Today's summary: %d %s", code, body)
	}
	return sum.Subscriptions
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestTodayPointsAtSubscriptionsComingDue(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	person := repliedPerson(t, s, o, "Hannah Pryce")
	now := store.MustDay(store.Today())
	on := func(days int) string { return now.AddDate(0, 0, days).Format(store.DayLayout) }
	// A year-long term that began 11 months ago ends in about a month; its day to cancel by is nearer still.
	record := func(start string, notice int) *store.Subscription {
		return must(s.AddSubscription(o.ID, sales.SubscriptionIn{Company: *person.CompanyID, Name: "Plan", Start: start, TermMonths: 12, NoticeDays: notice}, "Ada Lovelace"))
	}
	if dueSubs(t, client, srv.URL) != 0 {
		t.Fatal("Today points at subscriptions when there are none")
	}
	far := record(on(-30), 30)
	if dueSubs(t, client, srv.URL) != 0 {
		t.Error("Today points at a subscription with eleven months to run")
	}
	near := record(now.AddDate(-1, 0, 20).Format(store.DayLayout), 0)
	if dueSubs(t, client, srv.URL) != 1 {
		t.Error("Today says nothing of a subscription whose term ends in under three weeks")
	}
	soon := 0
	list, _ := readSubs(t, client, srv.URL, "")
	for _, sub := range list {
		if sub.Soon {
			soon++
			if sub.Id != near.ID {
				t.Errorf("subscription %d is marked as within 30 days, not the near one", sub.Id)
			}
		}
	}
	if soon != 1 {
		t.Errorf("the list marks %d subscriptions as within 30 days, want the one", soon)
	}
	if err := s.ChangeSubscription(o.ID, near.ID, "ended_on", on(-1), "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if dueSubs(t, client, srv.URL) != 0 {
		t.Error("Today still points at a subscription that has ended")
	}
	for _, c := range []struct {
		st   store.Standing
		want bool
	}{
		{store.Standing{Status: store.SubActive, TermEnd: on(30), CancelBy: on(30), Out: on(30)}, true},
		{store.Standing{Status: store.SubActive, TermEnd: on(31), CancelBy: on(31), Out: on(31)}, false},
		{store.Standing{Status: store.SubActive, TermEnd: on(200), CancelBy: on(0), Out: on(200)}, true},
		{store.Standing{Status: store.SubNotice, TermEnd: on(200), Ends: on(10)}, true},
		{store.Standing{Status: store.SubEnded, TermEnd: on(-5), Ends: on(-5)}, false},
		{store.Standing{Status: store.SubScheduled, TermEnd: on(400), CancelBy: on(370), Out: on(400)}, false},
	} {
		if got := c.st.Within(store.Today()); got != c.want {
			t.Errorf("%+v counts as soon: %v, want %v", c.st, got, c.want)
		}
	}
	_ = far

	// A plan that turns over every month always has a term ending within the month:
	// that is routine, until it has a day it stops.
	monthly := must(s.AddSubscription(o.ID, sales.SubscriptionIn{Company: *person.CompanyID, Name: "Monthly", Per: store.PerMonth, Start: on(-100)}, "Ada Lovelace"))
	rolled := must(s.AddSubscription(o.ID, sales.SubscriptionIn{Company: *person.CompanyID, Name: "Rolled", Per: store.PerMonth, Start: now.AddDate(0, -8, 0).Format(store.DayLayout), TermMonths: 6, Then: store.ThenRolls}, "Ada Lovelace"))
	if dueSubs(t, client, srv.URL) != 0 {
		t.Error("Today points at plans that only turn over monthly")
	}
	if err := s.ChangeSubscription(o.ID, monthly.ID, "notice_on", store.Today(), "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if dueSubs(t, client, srv.URL) != 1 {
		t.Error("Today says nothing of a monthly plan that notice was given on")
	}
	for _, c := range []struct {
		sub  store.Subscription
		on   string
		want int
	}{
		{store.Subscription{Start: "2026-01-01", Per: store.PerMonth}, "2026-06-15", 1},
		{store.Subscription{Start: "2026-01-01", Per: store.PerYear}, "2026-06-15", 12},
		{store.Subscription{Start: "2026-01-01", Per: store.PerMonth, TermMonths: 6, Then: store.ThenRolls}, "2026-06-30", 6},
		{store.Subscription{Start: "2026-01-01", Per: store.PerMonth, TermMonths: 6, Then: store.ThenRolls}, "2026-07-01", 1},
		{store.Subscription{Start: "2026-01-01", Per: store.PerYear, TermMonths: 6, Then: store.ThenRolls}, "2026-07-01", 12},
		{store.Subscription{Start: "2026-01-01", Per: store.PerMonth, TermMonths: 6, Then: sales.ThenRenews}, "2027-03-01", 6},
	} {
		if got := c.sub.MonthsNow(c.on); got != c.want {
			t.Errorf("%+v on %s is in a term of %d months, want %d", c.sub, c.on, got, c.want)
		}
	}
	_ = rolled
}

func TestARenewalDealIsStartedFromASubscription(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	hannah := repliedPerson(t, s, o, "Hannah Pryce")
	quiet := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Tom Ellery", Company: "Larkmoor Yachts"}, "Ada Lovelace"))
	elsewhere := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Sven Dahl", Company: "Nordkap Boats"}, "Ada Lovelace"))
	if err := s.Mark(o.ID, elsewhere.ID, "replied", store.Today(), "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	start := store.MustDay(store.Today()).AddDate(0, -11, 0).Format(store.DayLayout)
	sub := must(s.AddSubscription(o.ID, sales.SubscriptionIn{Company: *hannah.CompanyID, Name: "Standard plan", Amount: "1,500", Per: store.PerMonth, Start: start, TermMonths: 12, NoticeDays: 30}, "Ada Lovelace"))
	ended := must(s.AddSubscription(o.ID, sales.SubscriptionIn{Company: *hannah.CompanyID, Name: "Old plan", Start: "2020-01-01", TermMonths: 12, Then: store.ThenEnds}, "Ada Lovelace"))

	rows := must(s.Subscriptions(o.ID, 0, store.Today()))
	var row sales.SubscriptionRow
	for _, r := range rows {
		if r.ID == sub.ID {
			row = r
		}
	}
	if row.Renewing || len(row.People) != 1 || row.People[0].ID != hannah.ID {
		t.Fatalf("before a renewal, the people it can be with: %+v", row)
	}
	for why, with := range map[string]int64{"someone who has not replied": quiet.ID, "someone at another company": elsewhere.ID, "nobody": 99999} {
		if _, err := s.StartRenewal(o.ID, sub.ID, with, "Ada Lovelace"); err == nil {
			t.Errorf("a renewal was started with %s", why)
		}
	}
	if _, err := s.StartRenewal(o.ID, ended.ID, hannah.ID, "Ada Lovelace"); err == nil {
		t.Error("a renewal was started for a subscription that has ended")
	}
	other := mustOrg(t, s, "beta")
	if _, err := s.StartRenewal(other.ID, sub.ID, hannah.ID, "Eve"); err == nil {
		t.Error("another organisation started a renewal")
	}

	offered := func() (api.Subscription, bool) {
		t.Helper()
		list, _ := readSubs(t, client, srv.URL, "")
		for _, l := range list {
			if l.Id == sub.ID {
				return l, !l.Renewing && l.Status != "ended" && len(l.People) > 0
			}
		}
		t.Fatal("the subscription isn't listed")
		return api.Subscription{}, false
	}
	if l, ok := offered(); !ok || l.People[0].Id != hannah.ID || l.People[0].Name != "Hannah Pryce" {
		t.Errorf("the subscription lacks the offer of a renewal with the person in conversation: %+v", l)
	}
	renew := func(contact int64) (int, string) {
		return call(t, client, "POST", srv.URL+"/o/alpha/api/v1/subscriptions/"+itoa(sub.ID)+"/renewals", "application/json", map[string]any{"contact": contact})
	}
	if code, _ := renew(0); code < 300 {
		t.Error("a renewal was started with nobody chosen")
	}
	if code, body := renew(hannah.ID); code != http.StatusCreated {
		t.Fatalf("starting the renewal: %d %s", code, body)
	}
	deals := must(s.OtherDeals(o.ID))["replied"]
	if len(deals) != 1 || deals[0].Kind != "renewal" || deals[0].SubscriptionID != sub.ID || deals[0].Value() != "£18,000" || store.Deref(deals[0].CloseDate) != row.TermEnd {
		t.Fatalf("the renewal deal, worth a year of it and closing when the term ends: %+v", deals)
	}
	if code, _ := renew(hannah.ID); code < 300 {
		t.Error("a second renewal was started while the first is open")
	}
	if l, ok := offered(); !l.Renewing || ok {
		t.Error("the subscription still offers a renewal while one is open")
	}
	// Once the renewal is closed, another can be started next time round.
	if err := s.ChangeDeal(o.ID, deals[0].ID, "stage", "won", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if _, ok := offered(); !ok {
		t.Error("after the renewal was won, the subscription offers no next one")
	}
	// Removing the subscription leaves the deal, pointing at nothing.
	if err := s.RemoveSubscription(o.ID, sub.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if left := must(s.OtherDeals(o.ID))["won"]; len(left) != 1 || left[0].SubscriptionID != 0 {
		t.Errorf("the renewal deal after its subscription was removed: %+v", left)
	}
}

func TestARobotWorksSubscriptionsAsItsScopesAllow(t *testing.T) {
	t.Parallel()
	s, o, srv, _ := signedIn(t)
	other := mustOrg(t, s, "beta")
	key := func(org int64, scopes ...string) string {
		secret, _, err := s.CreateKey(org, "robot "+strings.Join(scopes, "+"), scopes, "Ada Lovelace")
		if err != nil {
			t.Fatal(err)
		}
		return secret
	}
	reader, writer, stranger := key(o.ID, "read"), key(o.ID, "customers:write"), key(other.ID, "read", "customers:write")
	hannah := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	subs := srv.URL + "/o/alpha/api/v1/subscriptions"
	plan := map[string]any{"company": *hannah.CompanyID, "name": "Standard plan", "amount": "14,000", "start": "2026-01-12", "term_months": 12, "notice_days": 60}

	if code, _ := asKey(t, "GET", subs, "", nil); code != http.StatusUnauthorized {
		t.Errorf("nobody at all listing subscriptions: %d", code)
	}
	if code, body := asKey(t, "POST", subs, reader, plan); code != http.StatusForbidden || !strings.Contains(body, "customers:write") {
		t.Errorf("a read key recording a subscription: %d %s", code, body)
	}
	code, body := asKey(t, "POST", subs, writer, plan)
	if code != http.StatusCreated || !strings.Contains(body, `"amount":1400000`) || !strings.Contains(body, `"per":"year"`) || !strings.Contains(body, `"then":"renews"`) || !strings.Contains(body, `"company":"Larkmoor Yachts"`) {
		t.Fatalf("recording a subscription: %d %s", code, body)
	}
	sub := must(s.Subscriptions(o.ID, 0, store.Today()))[0]
	one := subs + "/" + itoa(sub.ID)
	if code, body := asKey(t, "PATCH", one, writer, map[string]any{}); code != http.StatusBadRequest {
		t.Errorf("changing nothing: %d %s", code, body)
	}
	if code, body := asKey(t, "PATCH", one, writer, map[string]any{"term_months": 24, "amount": "18,000", "then": "ends"}); code != http.StatusOK || !strings.Contains(body, `"term_months":24`) || !strings.Contains(body, `"amount":1800000`) || !strings.Contains(body, `"then":"ends"`) {
		t.Errorf("changing a subscription: %d %s", code, body)
	}
	// Saved together or not at all, and a date cleared in the same change counts as cleared.
	if code, body := asKey(t, "PATCH", one, writer, map[string]any{"name": "Renamed", "per": "fortnight"}); code != http.StatusBadRequest {
		t.Errorf("a change with one part refused: %d %s", code, body)
	} else if now := must(s.Subscriptions(o.ID, 0, store.Today()))[0]; now.Name != "Standard plan" {
		t.Errorf("the part before the refused one was saved: %q", now.Name)
	}
	if code, body := asKey(t, "PATCH", one, writer, map[string]any{"notice_on": "2026-03-01"}); code != http.StatusOK {
		t.Fatalf("giving notice: %d %s", code, body)
	}
	if code, body := asKey(t, "PATCH", one, writer, map[string]any{"start": "2026-04-01", "notice_on": ""}); code != http.StatusOK || !strings.Contains(body, `"start":"2026-04-01"`) || !strings.Contains(body, `"notice_on":""`) {
		t.Errorf("moving the start and taking back notice together: %d %s", code, body)
	}
	if code, body := asKey(t, "PATCH", one, writer, map[string]any{"per": "fortnight"}); code != http.StatusBadRequest {
		t.Errorf("a period that is not one: %d %s", code, body)
	}
	if code, body := asKey(t, "GET", subs+"?company="+itoa(*hannah.CompanyID), reader, nil); code != http.StatusOK || !strings.Contains(body, `"name":"Standard plan"`) || !strings.Contains(body, `"status":"`) {
		t.Errorf("listing: %d %s", code, body)
	}

	theirs := srv.URL + "/o/beta/api/v1/subscriptions/" + itoa(sub.ID)
	if code, _ := asKey(t, "GET", subs, stranger, nil); code < 400 {
		t.Errorf("another organisation's key listing ours: %d", code)
	}
	if code, body := asKey(t, "PATCH", theirs, stranger, map[string]any{"name": "Theirs now"}); code != http.StatusNotFound {
		t.Errorf("another organisation changing ours by number: %d %s", code, body)
	}
	if code, body := asKey(t, "POST", theirs+"/renewals", stranger, map[string]any{"contact": hannah.ID}); code < 400 {
		t.Errorf("another organisation renewing ours by number: %d %s", code, body)
	}
	if code, body := asKey(t, "DELETE", theirs, stranger, nil); code != http.StatusNotFound {
		t.Errorf("another organisation removing ours by number: %d %s", code, body)
	}
	if now := must(s.Subscriptions(o.ID, 0, store.Today())); len(now) != 1 || now[0].Name != "Standard plan" {
		t.Errorf("another organisation's key changed something: %+v", now)
	}

	if code, body := asKey(t, "DELETE", one, reader, nil); code != http.StatusForbidden {
		t.Errorf("a read key removing: %d %s", code, body)
	}
	if code, body := asKey(t, "DELETE", one, writer, nil); code != http.StatusNoContent {
		t.Errorf("removing: %d %s", code, body)
	}
}

// The React page reads from the list what the server-rendered one worked out itself.
func TestTheSubscriptionsListCarriesWhatThePageShows(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	hannah := repliedPerson(t, s, o, "Hannah Pryce")
	subs := srv.URL + "/o/alpha/api/v1/subscriptions"
	now := store.MustDay(store.Today())
	record := func(name, amount, per string, start time.Time) {
		t.Helper()
		if code, body := call(t, client, "POST", subs, "application/json", map[string]any{"company": *hannah.CompanyID, "name": name, "amount": amount,
			"per": per, "start": start.Format(store.DayLayout), "term_months": 12, "then": "renews"}); code != http.StatusCreated {
			t.Fatalf("recording %s: %d %s", name, code, body)
		}
	}
	record("Standard plan", "14,000", "year", store.AddMonths(now, -12).AddDate(0, 0, 10))
	record("Support", "1,000", "month", store.AddMonths(now, -1))

	var list struct {
		Items []struct {
			Name, Figure string
			Soon         bool
			People       []struct {
				ID   int64
				Name string
			}
		}
		Contracted string
	}
	code, body := call(t, client, "GET", subs+"?limit=1", "", nil)
	if err := json.Unmarshal([]byte(body), &list); code != http.StatusOK || err != nil {
		t.Fatalf("listing: %d %s", code, body)
	}
	if list.Contracted != "£26,000" {
		t.Errorf("contracted over every page is %q, want £26,000", list.Contracted)
	}
	if len(list.Items) != 1 {
		t.Fatalf("one a page: %+v", list.Items)
	}
	first := list.Items[0]
	if first.Name != "Standard plan" || first.Figure != "£14,000" || !first.Soon {
		t.Errorf("the one with its term ending in days: %+v", first)
	}
	if len(first.People) != 1 || first.People[0].ID != hannah.ID || first.People[0].Name != "Hannah Pryce" {
		t.Errorf("who a renewal can be with: %+v", first.People)
	}
	_, body = call(t, client, "GET", subs, "", nil)
	if err := json.Unmarshal([]byte(body), &list); err != nil || len(list.Items) != 2 || list.Items[1].Soon || list.Items[1].Figure != "£1,000" {
		t.Errorf("the one with most of its term to run: %s", body)
	}
}
