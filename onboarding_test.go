package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fourSteps = "them: Name a project lead, 2 days\nthem: Send your product files, 7 days\n\nKick-off call, 10 days\nus: Go live"

func TestOnboardingStepsAreReadALineEach(t *testing.T) {
	plan, err := OrgSettings{OnboardingSteps: fourSteps}.Onboarding()
	if err != nil {
		t.Fatal(err)
	}
	want := []StepPlan{{"Name a project lead", StepTheirs, 2}, {"Send your product files", StepTheirs, 7}, {"Kick-off call", StepOurs, 10}, {"Go live", StepOurs, -1}}
	if len(plan) != len(want) {
		t.Fatalf("read %d steps, want %d: %+v", len(plan), len(want), plan)
	}
	for i := range want {
		if plan[i] != want[i] {
			t.Errorf("step %d read as %+v, want %+v", i+1, plan[i], want[i])
		}
	}
	// A colon in a step's own name isn't who does it, and a number in it isn't its days.
	odd, err := OrgSettings{OnboardingSteps: "Call them: agree 3 sites\nTHEM : Sign, 0 day"}.Onboarding()
	if err != nil || len(odd) != 2 || odd[0] != (StepPlan{"Call them: agree 3 sites", StepOurs, -1}) || odd[1] != (StepPlan{"Sign", StepTheirs, 0}) {
		t.Errorf("odd lines read as %+v (%v)", odd, err)
	}
	for why, text := range map[string]string{"no name": "them:, 3 days", "three years": "Go live, 1000 days", "too many": strings.Repeat("Step\n", 41)} {
		if _, err := (OrgSettings{OnboardingSteps: text}).Onboarding(); err == nil {
			t.Errorf("steps with %s were accepted", why)
		}
	}
	if none, err := (OrgSettings{}).Onboarding(); err != nil || len(none) != 0 {
		t.Errorf("no steps read as %+v (%v)", none, err)
	}
}

func TestACustomerIsOnboardedStepByStep(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	person := repliedPerson(t, s, o, "Hannah Pryce")
	company := *person.CompanyID
	in := OnboardingIn{Company: company, Name: "Leith site", Start: "2026-11-02"}
	if _, err := s.StartOnboarding(o.ID, in, "Ada Lovelace"); err == nil {
		t.Error("an onboarding was started with no steps written")
	}
	o.Settings.OnboardingSteps = fourSteps
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	for why, bad := range map[string]OnboardingIn{"no start": {Company: company}, "no company": {Company: company + 99, Start: "2026-11-02"}} {
		if _, err := s.StartOnboarding(o.ID, bad, "Ada Lovelace"); err == nil {
			t.Errorf("an onboarding with %s was started", why)
		}
	}
	ob := must(s.StartOnboarding(o.ID, in, "Ada Lovelace"))

	rows := must(s.Onboardings(o.ID, 0, "2026-11-09"))
	if len(rows) != 1 || rows[0].ID != ob.ID || rows[0].Company != "Larkmoor Yachts" || len(rows[0].Steps) != 4 {
		t.Fatalf("the onboarding as started: %+v", rows)
	}
	r := rows[0]
	if deref(r.Steps[0].Due) != "2026-11-04" || deref(r.Steps[1].Due) != "2026-11-09" || deref(r.Steps[2].Due) != "2026-11-12" || r.Steps[3].Due != nil || r.Steps[0].Who != StepTheirs {
		t.Errorf("the steps' dates and who does them: %+v", r.Steps)
	}
	if r.Late != 1 || r.DueToday != 1 || r.Done != 0 || r.Next == nil || r.Next.Seq != 1 || r.Live() {
		t.Errorf("a week in with nothing done: %+v", r)
	}

	// Changing the steps in Settings leaves this one as it was.
	o.Settings.OnboardingSteps = "Only one step now"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	set := func(seq int, field, value string) error {
		return s.SetOnboardingStep(o.ID, r.Steps[seq-1].ID, field, value, "Ada Lovelace")
	}
	for field, value := range map[string]string{"due": "next week", "name": "Renamed", "who": "them", "done": "no"} {
		if err := set(1, field, value); err == nil {
			t.Errorf("a step's %s was set to %q", field, value)
		}
	}
	for _, step := range []struct {
		seq          int
		field, value string
	}{{1, "done", "yes"}, {2, "due", "2026-11-20"}, {3, "done", "yes"}, {3, "done", ""}} {
		if err := set(step.seq, step.field, step.value); err != nil {
			t.Fatalf("setting step %d's %s: %v", step.seq, step.field, err)
		}
	}
	// A teammate on a page that hadn't caught up presses Done on a step already done.
	if err := s.SetOnboardingStep(o.ID, r.Steps[0].ID, "done", "yes", "Grace Okoro"); err != nil {
		t.Fatal(err)
	}
	r = must(s.Onboardings(o.ID, company, "2026-11-13"))[0]
	if len(r.Steps) != 4 || r.Done != 1 || r.Steps[0].DoneBy != "Ada Lovelace" || deref(r.Steps[0].DoneOn) != today() ||
		r.Late != 1 || r.Next.Seq != 2 || deref(r.Steps[1].Due) != "2026-11-20" || r.Steps[2].DoneOn != nil || r.Steps[2].DoneBy != "" {
		t.Errorf("part way through: %+v", r)
	}
	for seq := 2; seq <= 4; seq++ {
		if err := set(seq, "done", "yes"); err != nil {
			t.Fatal(err)
		}
	}
	if r = must(s.Onboardings(o.ID, 0, "2026-11-13"))[0]; !r.Live() || r.LiveOn != today() || r.Late != 0 || r.Next != nil {
		t.Errorf("with every step done: %+v", r)
	}

	var said []Event
	if err := s.db.Where("org_id = ? and company_id = ? and action like ?", o.ID, company, "onboard:%").Order("id").Find(&said).Error; err != nil {
		t.Fatal(err)
	}
	if len(said) != 8 || said[0].Action != "onboard:start" || deref(said[0].Value) != "2026-11-02" || deref(said[1].Value) != "Name a project lead" ||
		deref(said[2].Value) != "Send your product files: 2026-11-20" || said[4].Action != "onboard:undone" || said[0].ContactID != 0 {
		t.Errorf("the history of the onboarding: %+v", said)
	}

	other := mustOrg(t, s, "beta")
	other.Settings.OnboardingSteps = fourSteps
	if err := s.SaveOrg(other); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartOnboarding(other.ID, in, "Eve"); err == nil {
		t.Error("another organisation started an onboarding for the first one's company")
	}
	if err := s.SetOnboardingStep(other.ID, r.Steps[0].ID, "done", "", "Eve"); err == nil {
		t.Error("another organisation reopened a step")
	}
	if err := s.RemoveOnboarding(other.ID, ob.ID, "Eve"); err == nil {
		t.Error("another organisation removed the onboarding")
	}
	if theirs := must(s.Onboardings(other.ID, 0, "2026-11-13")); len(theirs) != 0 {
		t.Errorf("another organisation sees onboardings: %+v", theirs)
	}

	// A second for the same company, late, sorts ahead of the one that is live.
	o.Settings.OnboardingSteps = fourSteps
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	second := must(s.StartOnboarding(o.ID, OnboardingIn{Company: company, Name: "Perth site", Start: "2026-11-01"}, "Ada Lovelace"))
	if rows := must(s.Onboardings(o.ID, 0, "2026-11-13")); len(rows) != 2 || rows[0].ID != second.ID || rows[0].Late != 3 {
		t.Errorf("two onboardings, the late one first: %+v", rows)
	}

	// It is the company's, so it outlasts the person and is in the organisation's export.
	if err := s.ErasePerson(o.ID, person.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if export := must(s.ExportOrg(o.ID, "Ada Lovelace")); len(export.Onboardings) != 2 || len(export.OnboardSteps) != 8 {
		t.Errorf("the export holds %d onboardings and %d steps", len(export.Onboardings), len(export.OnboardSteps))
	}
	if err := s.RemoveOnboarding(o.ID, second.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	var steps int64
	if err := s.db.Model(&OnboardingStep{}).Where("org_id = ?", o.ID).Count(&steps).Error; err != nil || steps != 4 {
		t.Errorf("removing an onboarding left %d steps, want the other one's four (%v)", steps, err)
	}
}

func TestOnboardingIsWorkedFromItsPages(t *testing.T) {
	s, o, srv, client := signedIn(t)
	person := repliedPerson(t, s, o, "Hannah Pryce")
	company := *person.CompanyID
	post := func(path string, body map[string]any) (int, string) {
		return call(t, client, "POST", srv.URL+"/o/alpha/ui/"+path, "application/json", body)
	}
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/onboarding", "", nil); !strings.Contains(page, "No steps written yet") {
		t.Error("the page with no steps written doesn't say so")
	}
	settings := func(steps string) (int, string) {
		return post("settings", map[string]any{"name": "Alpha", "color": "#56c5f4", "onboarding_steps": steps})
	}
	if code, body := settings("Go live, 9999 days"); code != http.StatusBadRequest || !strings.Contains(body, "730") {
		t.Errorf("steps due in 27 years were saved: %d %s", code, body)
	}
	if code, body := settings(fourSteps); code != http.StatusOK {
		t.Fatalf("saving the steps: %d %s", code, body)
	}
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/settings", "", nil); !strings.Contains(page, "them: Send your product files, 7 days") {
		t.Error("Settings doesn't show the steps that were saved")
	}
	if code, body := call(t, client, "POST", srv.URL+"/o/alpha/api/v1/onboardings", "application/json", map[string]any{"company": company, "name": "Leith site", "start": "2020-01-06"}); code != http.StatusCreated || !strings.Contains(body, `"who":"them"`) {
		t.Fatalf("starting an onboarding: %d %s", code, body)
	}
	row := must(s.Onboardings(o.ID, 0, today()))[0]
	for _, path := range []string{"/o/alpha/onboarding", "/o/alpha/companies/" + itoa(company)} {
		_, page := call(t, client, "GET", srv.URL+path, "", nil)
		for _, want := range []string{"Leith site", "Send your product files", "3 steps late", "late, was due 2020-01-08", `data-step="` + itoa(row.Steps[0].ID) + `"`, "api/v1/onboarding-steps/"} {
			if !strings.Contains(page, want) {
				t.Errorf("%s doesn't show %q", path, want)
			}
		}
	}
	const prompt = "Move onboarding steps on"
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/today", "", nil); !strings.Contains(page, prompt) {
		t.Error("Today says nothing of onboarding steps that are late")
	}
	for _, st := range row.Steps {
		if code, body := call(t, client, "PATCH", srv.URL+"/o/alpha/api/v1/onboarding-steps/"+itoa(st.ID), "application/json", map[string]any{"done": true}); code != http.StatusOK {
			t.Fatalf("finishing step %d: %d %s", st.Seq, code, body)
		}
	}
	if code, _ := call(t, client, "PATCH", srv.URL+"/o/alpha/api/v1/onboarding-steps/"+itoa(row.Steps[0].ID), "application/json", map[string]any{"name": "Other"}); code == http.StatusOK {
		t.Error("a step was renamed through the route")
	}
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/onboarding", "", nil); !strings.Contains(page, "live since "+today()) || !strings.Contains(page, "Ada Lovelace") {
		t.Error("the page doesn't say the customer is live, or who finished the steps")
	}
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/today", "", nil); strings.Contains(page, prompt) {
		t.Error("Today still points at onboarding once every step is done")
	}
	if code, body := call(t, client, "DELETE", srv.URL+"/o/alpha/api/v1/onboardings/"+itoa(row.ID), "", nil); code != http.StatusNoContent {
		t.Fatalf("removing an onboarding: %d %s", code, body)
	}
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/onboarding", "", nil); !strings.Contains(page, "Nobody is being onboarded") {
		t.Error("the page after the only onboarding was removed")
	}
}

// A database written before onboarding existed has companies and neither table.
func TestADatabaseFromBeforeOnboardingOpens(t *testing.T) {
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
	if err := old.db.Migrator().DropTable(&Onboarding{}, &OnboardingStep{}); err != nil {
		t.Fatal(err)
	}
	s := must(Open(dsn))
	if rows := must(s.Onboardings(o.ID, 0, "2026-01-01")); len(rows) != 0 {
		t.Fatalf("onboardings nobody started: %+v", rows)
	}
	o.Settings.OnboardingSteps = fourSteps
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartOnboarding(o.ID, OnboardingIn{Company: *person.CompanyID, Start: "2026-01-01"}, "Ada Lovelace"); err != nil {
		t.Errorf("the first onboarding after the upgrade: %v", err)
	}
}

func TestAWonDealOffersOnboarding(t *testing.T) {
	s, o, srv, client := signedIn(t)
	won := repliedPerson(t, s, o, "Hannah Pryce")
	open := repliedPerson(t, s, o, "Tom Ellery")
	if err := s.Update(o.ID, won.ID, "stage", "won", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	board := func() string {
		_, page := call(t, client, "GET", srv.URL+"/o/alpha/conversations", "", nil)
		return page
	}
	// With no steps written there is nothing to start.
	if strings.Contains(board(), "Start onboarding") {
		t.Error("onboarding is offered though no steps are written")
	}
	o.Settings.OnboardingSteps = fourSteps
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	page := board()
	if n := strings.Count(page, "Start onboarding"); n != 1 {
		t.Fatalf("onboarding is offered on %d cards, want only the won one", n)
	}
	// The button carries what the route needs; pressing it is this post.
	body := `{&#34;company&#34;:` + itoa(*won.CompanyID) + `,&#34;start&#34;:&#34;` + today() + `&#34;}`
	if !strings.Contains(page, body) {
		t.Errorf("the button lacks the company and the date: want %s", body)
	}
	if code, text := call(t, client, "POST", srv.URL+"/o/alpha/api/v1/onboardings", "application/json", map[string]any{"company": *won.CompanyID, "start": today()}); code != http.StatusCreated {
		t.Fatalf("starting onboarding from a won deal: %d %s", code, text)
	}
	if page = board(); strings.Contains(page, "Start onboarding") || !strings.Contains(page, "Onboarding under way") {
		t.Error("a won deal whose company is being onboarded still offers to start")
	}
	// A renewal that is won, with the same company, says the same.
	renewal := must(s.StartDeal(o.ID, open.ID, "renewal", "Ada Lovelace"))
	if err := s.ChangeDeal(o.ID, renewal.ID, "stage", "won", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(board(), "Onboarding under way"); n != 2 {
		t.Errorf("%d cards say onboarding has started, want the won deal and the won renewal", n)
	}
}

// Once every step is done the won card says so, and the way to start another is the company page.
func TestAWonDealSaysWhenItsCompanyIsOnboarded(t *testing.T) {
	s, o, srv, client := signedIn(t)
	won := repliedPerson(t, s, o, "Hannah Pryce")
	if err := s.Update(o.ID, won.ID, "stage", "won", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	o.Settings.OnboardingSteps = "Go live"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	must(s.StartOnboarding(o.ID, OnboardingIn{Company: *won.CompanyID, Start: today()}, "Ada Lovelace"))
	step := must(s.Onboardings(o.ID, 0, today()))[0].Steps[0]
	if err := s.SetOnboardingStep(o.ID, step.ID, "done", "yes", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	_, page := call(t, client, "GET", srv.URL+"/o/alpha/conversations", "", nil)
	if !strings.Contains(page, ">Onboarded<") || strings.Contains(page, "Onboarding under way") || strings.Contains(page, "Start onboarding") {
		t.Error("a won deal whose company is live lacks Onboarded, or still offers to start")
	}
}

func TestARobotWorksOnboardingAsItsScopesAllow(t *testing.T) {
	s, srv, _, box := mailSite(t, nil)
	o := must(s.OrgBySlug("alpha"))
	other := mustOrg(t, s, "beta")
	o.Settings.OnboardingSteps = "them: Send your product files, 7 days\nus: Go live"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	key := func(org int64, scopes ...string) string {
		secret, _, err := s.CreateKey(org, "robot "+strings.Join(scopes, "+"), scopes, "Ada Lovelace")
		if err != nil {
			t.Fatal(err)
		}
		return secret
	}
	reader, writer, inviter, stranger := key(o.ID, "read"), key(o.ID, "read", "customers:write"), key(o.ID, "customers:invite"), key(other.ID, "read", "customers:write", "customers:invite")
	hannah := must(s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	base := srv.URL + "/o/alpha/api/v1/"
	start := map[string]any{"company": *hannah.CompanyID, "name": "Leith site", "start": "2020-01-06"}

	if code, _ := asKey(t, "GET", base+"onboardings", "", nil); code != http.StatusUnauthorized {
		t.Errorf("nobody at all listing onboardings: %d", code)
	}
	if code, body := asKey(t, "POST", base+"onboardings", reader, start); code != http.StatusForbidden || !strings.Contains(body, "customers:write") {
		t.Errorf("a read key starting an onboarding: %d %s", code, body)
	}
	code, body := asKey(t, "POST", base+"onboardings", writer, start)
	if code != http.StatusCreated || !strings.Contains(body, `"due":"2020-01-13"`) || !strings.Contains(body, `"company":"Larkmoor Yachts"`) {
		t.Fatalf("starting an onboarding: %d %s", code, body)
	}
	row := must(s.Onboardings(o.ID, 0, today()))[0]
	step := base + "onboarding-steps/" + itoa(row.Steps[0].ID)
	if code, body := asKey(t, "PATCH", step, reader, map[string]any{"done": true}); code != http.StatusForbidden {
		t.Errorf("a read key marking a step: %d %s", code, body)
	}
	if code, body := asKey(t, "PATCH", step, writer, map[string]any{}); code != http.StatusBadRequest {
		t.Errorf("changing nothing about a step: %d %s", code, body)
	}
	if code, body := asKey(t, "PATCH", step, writer, map[string]any{"done": true, "due": "2020-02-01"}); code != http.StatusOK || !strings.Contains(body, `"due":"2020-02-01"`) || !strings.Contains(body, `"done_by":"agent:robot read+customers:write"`) {
		t.Errorf("marking a step and moving its date: %d %s", code, body)
	}
	if code, body := asKey(t, "PATCH", step, writer, map[string]any{"done": false}); code != http.StatusOK || !strings.Contains(body, `"done_on":""`) {
		t.Errorf("reopening a step: %d %s", code, body)
	}

	// With no start given, steps are dated from today.
	if code, body := asKey(t, "POST", base+"onboardings", writer, map[string]any{"company": *hannah.CompanyID, "name": "Second site"}); code != http.StatusCreated || !strings.Contains(body, `"start":"`+today()+`"`) {
		t.Errorf("starting an onboarding with no start: %d %s", code, body)
	} else if err := s.db.Where("name = ?", "Second site").Delete(&Onboarding{}).Error; err != nil {
		t.Fatal(err)
	}
	// Mailing a customer is a scope of its own.
	invites := base + "onboardings/" + itoa(row.ID) + "/invites"
	if code, body := asKey(t, "POST", invites, writer, map[string]any{"contact": hannah.ID}); code != http.StatusForbidden || !strings.Contains(body, "customers:invite") {
		t.Errorf("a key without customers:invite inviting: %d %s", code, body)
	}
	box.take(t, 0)
	if code, body := asKey(t, "POST", invites, inviter, map[string]any{"contact": hannah.ID}); code != http.StatusNoContent {
		t.Fatalf("inviting: %d %s", code, body)
	}
	if got := box.take(t, 1)[0]; got.To != "hannah@larkmoor.example" {
		t.Errorf("the link went to %q", got.To)
	}
	code, body = asKey(t, "GET", base+"onboardings?company="+itoa(*hannah.CompanyID), reader, nil)
	if code != http.StatusOK || !strings.Contains(body, `"email":"hannah@larkmoor.example"`) || !strings.Contains(body, `"withdrawn":false`) || strings.Contains(body, "token") {
		t.Errorf("listing onboardings: %d %s", code, body)
	}
	if code, body := asKey(t, "GET", base+"onboardings?company=99999", reader, nil); code != http.StatusOK || strings.TrimSpace(body) != "[]" {
		t.Errorf("listing another company's: %d %s", code, body)
	}

	// Another organisation's key reaches none of it, here or by number from its own address.
	grant := must(s.CustomerInvites(o.ID))[row.ID][0]
	theirs := srv.URL + "/o/beta/api/v1/"
	for what, try := range map[string][3]string{
		"listing": {"GET", base + "onboardings", ""}, "marking": {"PATCH", theirs + "onboarding-steps/" + itoa(row.Steps[0].ID), `done`},
		"inviting": {"POST", theirs + "onboardings/" + itoa(row.ID) + "/invites", `contact`}, "withdrawing": {"DELETE", theirs + "onboarding-invites/" + itoa(grant.ID), ""},
		"removing": {"DELETE", theirs + "onboardings/" + itoa(row.ID), ""},
	} {
		var sent any
		switch try[2] {
		case "done":
			sent = map[string]any{"done": true}
		case "contact":
			sent = map[string]any{"contact": hannah.ID}
		}
		if code, body := asKey(t, try[0], try[1], stranger, sent); code < 400 {
			t.Errorf("another organisation's key %s: %d %s", what, code, body)
		}
	}
	box.take(t, 0)
	if left := must(s.Onboardings(o.ID, 0, today())); len(left) != 1 || left[0].Steps[0].DoneOn != nil {
		t.Errorf("another organisation's key changed something: %+v", left)
	}

	if code, body := asKey(t, "DELETE", base+"onboarding-invites/"+itoa(grant.ID), inviter, nil); code != http.StatusNoContent {
		t.Errorf("withdrawing: %d %s", code, body)
	}
	if code, body := asKey(t, "DELETE", base+"onboardings/"+itoa(row.ID), writer, nil); code != http.StatusNoContent {
		t.Errorf("removing: %d %s", code, body)
	}
	if code, _ := asKey(t, "DELETE", base+"onboardings/"+itoa(row.ID), writer, nil); code != http.StatusNotFound {
		t.Errorf("removing what is gone: %d", code)
	}
}
