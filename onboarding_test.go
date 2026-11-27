package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const fourSteps = "them: Name a project lead, 2 days\nthem: Send your product files, 7 days\n\nKick-off call, 10 days\nus: Go live"

func TestOnboardingStepsAreReadALineEach(t *testing.T) {
	t.Parallel()
	plan, err := OrgSettings{OnboardingSteps: fourSteps}.Onboarding()
	if err != nil {
		t.Fatal(err)
	}
	want := []StepPlan{{Name: "Name a project lead", Who: StepTheirs, Days: 2}, {Name: "Send your product files", Who: StepTheirs, Days: 7}, {Name: "Kick-off call", Who: StepOurs, Days: 10}, {Name: "Go live", Who: StepOurs, Days: -1}}
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
	if err != nil || len(odd) != 2 || odd[0] != (StepPlan{Name: "Call them: agree 3 sites", Who: StepOurs, Days: -1}) || odd[1] != (StepPlan{Name: "Sign", Who: StepTheirs, Days: 0}) {
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
	t.Parallel()
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
	if err := s.DB.Where("org_id = ? and company_id = ? and action like ?", o.ID, company, "onboard:%").Order("id").Find(&said).Error; err != nil {
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
	if err := s.DB.Model(&OnboardingStep{}).Where("org_id = ?", o.ID).Count(&steps).Error; err != nil || steps != 4 {
		t.Errorf("removing an onboarding left %d steps, want the other one's four (%v)", steps, err)
	}
}

func TestOnboardingIsWorkedFromItsPages(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	person := repliedPerson(t, s, o, "Hannah Pryce")
	company := *person.CompanyID
	// The page says no steps are written while the settings hold none.
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/settings", "", nil); !strings.Contains(body, `"onboarding_steps":""`) {
		t.Errorf("with no steps written the settings hold some: %s", body)
	}
	settings := func(steps string) (int, string) {
		return call(t, client, "PATCH", srv.URL+"/o/alpha/api/v1/settings", "application/json", map[string]any{"onboarding_steps": steps})
	}
	if code, body := settings("Go live, 9999 days"); code != http.StatusBadRequest || !strings.Contains(body, "730") {
		t.Errorf("steps due in 27 years were saved: %d %s", code, body)
	}
	if code, body := settings(fourSteps); code != http.StatusOK {
		t.Fatalf("saving the steps: %d %s", code, body)
	}
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/settings", "", nil); !strings.Contains(body, "them: Send your product files, 7 days") {
		t.Errorf("the settings don't hold the steps that were saved: %s", body)
	}
	if code, body := call(t, client, "POST", srv.URL+"/o/alpha/api/v1/onboardings", "application/json", map[string]any{"company": company, "name": "Leith site", "start": "2020-01-06"}); code != http.StatusCreated || !strings.Contains(body, `"who":"them"`) {
		t.Fatalf("starting an onboarding: %d %s", code, body)
	}
	row := must(s.Onboardings(o.ID, 0, today()))[0]
	// The API says who could be invited and whether a link can go, as the page does.
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/onboardings", "", nil); !strings.Contains(body, `"invitable":[`) || !strings.Contains(body, `"can_invite":false`) {
		t.Errorf("the onboardings should say who could be invited and that no link can go without mail: %s", body)
	}
	// The Onboarding page and the company's page both draw it from what the API gives.
	for _, path := range []string{"/o/alpha/api/v1/onboardings", "/o/alpha/api/v1/companies/" + itoa(company) + "/overview"} {
		_, body := call(t, client, "GET", srv.URL+path, "", nil)
		for _, want := range []string{`"name":"Leith site"`, `"name":"Send your product files"`, `"late":3`, `"due":"2020-01-08"`, `"id":` + itoa(row.Steps[0].ID) + `,`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s doesn't give %s", path, want)
			}
		}
	}
	late := func() string {
		_, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/today/summary", "", nil)
		return body
	}
	if !strings.Contains(late(), `"onboarding_steps":3`) {
		t.Errorf("Today says nothing of onboarding steps that are late: %s", late())
	}
	for _, st := range row.Steps {
		if code, body := call(t, client, "PATCH", srv.URL+"/o/alpha/api/v1/onboarding-steps/"+itoa(st.ID), "application/json", map[string]any{"done": true}); code != http.StatusOK {
			t.Fatalf("finishing step %d: %d %s", st.Seq, code, body)
		}
	}
	if code, _ := call(t, client, "PATCH", srv.URL+"/o/alpha/api/v1/onboarding-steps/"+itoa(row.Steps[0].ID), "application/json", map[string]any{"name": "Other"}); code == http.StatusOK {
		t.Error("a step was renamed through the route")
	}
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/onboardings", "", nil); !strings.Contains(body, `"live_on":"`+today()+`"`) || !strings.Contains(body, `"done_by":"Ada Lovelace"`) {
		t.Errorf("the onboarding doesn't say the customer is live, or who finished the steps: %s", body)
	}
	if !strings.Contains(late(), `"onboarding_steps":0`) {
		t.Errorf("Today still points at onboarding once every step is done: %s", late())
	}
	if code, body := call(t, client, "DELETE", srv.URL+"/o/alpha/api/v1/onboardings/"+itoa(row.ID), "", nil); code != http.StatusNoContent {
		t.Fatalf("removing an onboarding: %d %s", code, body)
	}
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/onboardings", "", nil); !strings.Contains(body, `"items":[]`) {
		t.Errorf("the onboardings after the only one was removed: %s", body)
	}
}

func TestAWonDealOffersOnboarding(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	won := repliedPerson(t, s, o, "Hannah Pryce")
	open := repliedPerson(t, s, o, "Tom Ellery")
	if err := s.Update(o.ID, won.ID, "stage", "won", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	// The board offers to start onboarding on a won card whose company has none, once steps are written.
	board := func() (planned int, offered, running int) {
		t.Helper()
		var b struct {
			Items []struct {
				Stage      string `json:"stage"`
				CompanyID  int64  `json:"company_id"`
				Onboarding string `json:"onboarding"`
			} `json:"items"`
			Steps int `json:"onboarding_steps"`
		}
		_, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/conversations", "", nil)
		if err := json.Unmarshal([]byte(body), &b); err != nil {
			t.Fatalf("the board: %v %s", err, body)
		}
		for _, c := range b.Items {
			switch {
			case c.Stage != "won" || c.CompanyID == 0:
			case c.Onboarding == "running":
				running++
			case c.Onboarding == "":
				offered++
			}
		}
		return b.Steps, offered, running
	}
	if planned, _, _ := board(); planned != 0 {
		t.Errorf("onboarding is offered though no steps are written: %d steps", planned)
	}
	o.Settings.OnboardingSteps = fourSteps
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	if planned, offered, _ := board(); planned != 4 || offered != 1 {
		t.Fatalf("onboarding is offered on %d cards with %d steps, want only the won one with 4", offered, planned)
	}
	if code, text := call(t, client, "POST", srv.URL+"/o/alpha/api/v1/onboardings", "application/json", map[string]any{"company": *won.CompanyID, "start": today()}); code != http.StatusCreated {
		t.Fatalf("starting onboarding from a won deal: %d %s", code, text)
	}
	if _, offered, running := board(); offered != 0 || running != 1 {
		t.Error("a won deal whose company is being onboarded still offers to start")
	}
	// A renewal that is won, with the same company, says the same.
	renewal := must(s.StartDeal(o.ID, open.ID, "renewal", "Ada Lovelace"))
	if err := s.ChangeDeal(o.ID, renewal.ID, "stage", "won", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if _, _, running := board(); running != 2 {
		t.Errorf("%d cards say onboarding has started, want the won deal and the won renewal", running)
	}
}

// Once every step is done the won card says so, and the way to start another is the company page.
func TestAWonDealSaysWhenItsCompanyIsOnboarded(t *testing.T) {
	t.Parallel()
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
	_, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/conversations", "", nil)
	if !strings.Contains(body, `"onboarding":"live"`) || strings.Contains(body, `"onboarding":"running"`) || strings.Contains(body, `"onboarding":""`) {
		t.Errorf("a won deal whose company is live lacks Onboarded, or still offers to start: %s", body)
	}
}

func TestARobotWorksOnboardingAsItsScopesAllow(t *testing.T) {
	t.Parallel()
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
	} else if err := s.DB.Where("name = ?", "Second site").Delete(&Onboarding{}).Error; err != nil {
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
	if code, body := asKey(t, "GET", base+"onboardings?company=99999", reader, nil); code != http.StatusOK || strings.TrimSpace(body) != `{"items":[]}` {
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
