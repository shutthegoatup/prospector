package main

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

var customerLink = regexp.MustCompile(regexp.QuoteMeta(testSite) + `(/c/[A-Za-z0-9_-]+)`)

// as makes a request the way a browser with that cookie jar would, following redirects,
// and returns where it ended up, the status and the page.
func as(t *testing.T, c *http.Client, method, u string) (*http.Response, string) {
	t.Helper()
	var res *http.Response
	var err error
	if method == "POST" {
		res, err = c.PostForm(u, url.Values{})
	} else {
		res, err = c.Get(u)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res, string(body)
}

// ageCustomerLinks makes every link look as if it was mailed long enough ago for another to go.
func ageCustomerLinks(t *testing.T, s *Store) {
	t.Helper()
	if err := s.db.Model(&CustomerLink{}).Where("1 = 1").Update("created_at", time.Now().Add(-2*customerLinkGap)).Error; err != nil {
		t.Fatal(err)
	}
}

func TestACustomerOpensTheirStepsFromAnEmailedLink(t *testing.T) {
	s, srv, member, box := mailSite(t, nil)
	o := must(s.OrgBySlug("alpha"))
	o.Settings.OnboardingSteps = "them: Send your product files, 7 days\nus: Internal credit check, 3 days\nthem: Approve the first renders\nus: Go live"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	hannah := must(s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	noEmail := must(s.QuickAddPerson(o, QuickAdd{Name: "Tom Ellery", Company: "Larkmoor Yachts"}, "Ada Lovelace"))
	elsewhere := must(s.QuickAddPerson(o, QuickAdd{Name: "Sven Dahl", Company: "Nordkap Boats", Email: "sven@nordkap.example"}, "Ada Lovelace"))
	ob := must(s.StartOnboarding(o.ID, OnboardingIn{Company: *hannah.CompanyID, Name: "Leith site", Start: today()}, "Ada Lovelace"))
	invite := func(contact int64) (int, string) {
		return call(t, member, "POST", srv.URL+"/o/alpha/api/v1/onboardings/"+itoa(ob.ID)+"/invites", "application/json", map[string]any{"contact": contact})
	}
	for why, who := range map[string]int64{"nobody": 0, "someone with no email address": noEmail.ID, "someone at another company": elsewhere.ID, "someone who is not there": 99999} {
		if code, _ := invite(who); code < 300 {
			t.Errorf("%s was invited", why)
		}
	}
	box.take(t, 0)
	if _, page := call(t, member, "GET", srv.URL+"/o/alpha/onboarding", "", nil); !strings.Contains(page, `/invites"`) || !strings.Contains(page, "hannah@larkmoor.example") || strings.Contains(page, "Tom Ellery ·") {
		t.Error("the onboarding page doesn't offer to invite the person with an address, or offers one without")
	}
	if code, body := invite(hannah.ID); code != http.StatusNoContent {
		t.Fatalf("inviting a customer: %d %s", code, body)
	}
	mail := box.take(t, 1)[0]
	found := customerLink.FindStringSubmatch(mail.Body)
	if found == nil || mail.To != "hannah@larkmoor.example" {
		t.Fatalf("the invitation: to %q, %q", mail.To, mail.Body)
	}
	if strings.Contains(mail.Body+mail.Subject, "product files") || strings.Contains(mail.Body+mail.Subject, "Ada") {
		t.Errorf("the invitation names a step or a teammate: %q %q", mail.Subject, mail.Body)
	}
	link := srv.URL + found[1]

	// Looking at the link changes nothing, however often: mail scanners follow links.
	outsider := newMailClient()
	for range 2 {
		res, page := as(t, outsider, "GET", link)
		if res.StatusCode != http.StatusOK || !strings.Contains(page, "Open my steps") || strings.Contains(page, "product files") {
			t.Fatalf("looking at the link: %d %.200s", res.StatusCode, page)
		}
		if res.Header.Get("Referrer-Policy") != "no-referrer" || !strings.Contains(res.Header.Get("X-Robots-Tag"), "noindex") || len(res.Cookies()) != 0 {
			t.Errorf("the link's page: referrer %q, robots %q, cookies %v", res.Header.Get("Referrer-Policy"), res.Header.Get("X-Robots-Tag"), res.Cookies())
		}
	}
	if _, page := as(t, outsider, "GET", srv.URL+"/c"); !strings.Contains(page, "Nothing to show here") {
		t.Error("the page opened before the link was used")
	}

	// Pressing the button opens it, in this browser, at an address with no secret in it.
	res, page := as(t, outsider, "POST", link)
	if res.Request.URL.Path != "/c" || !strings.Contains(page, "Send your product files") || !strings.Contains(page, "Approve the first renders") || !strings.Contains(page, "0 of 2") {
		t.Fatalf("opening the page: ended at %s with %.300s", res.Request.URL.Path, page)
	}
	for _, private := range []string{"Internal credit check", "Go live", "Ada Lovelace", "/o/alpha", "Tom Ellery"} {
		if strings.Contains(page, private) {
			t.Errorf("the customer's page shows %q", private)
		}
	}
	if !strings.Contains(page, "hannah@larkmoor.example") || strings.Contains(page, "Leith site") {
		t.Error("the customer's page lacks whose it is, or shows the name members gave the onboarding")
	}

	// The link is spent. Another browser gets nothing from it but the offer of a new one.
	second := newMailClient()
	if _, page := as(t, second, "POST", link); !strings.Contains(page, "used or has run out") || strings.Contains(page, "product files") {
		t.Errorf("a second browser using the same link: %.200s", page)
	}
	if _, page := as(t, second, "GET", srv.URL+"/c"); !strings.Contains(page, "Nothing to show here") {
		t.Error("a second browser has the page without a link of its own")
	}
	// A member's sign-in is not a customer's, and a customer's opens nothing of the members'.
	if _, page := as(t, member, "GET", srv.URL+"/c"); !strings.Contains(page, "Nothing to show here") {
		t.Error("a member's session opened a customer's page")
	}
	if res, page := as(t, outsider, "GET", srv.URL+"/o/alpha/onboarding"); res.Request.URL.Path != "/login" || strings.Contains(page, "Leith site") {
		t.Errorf("a customer's browser reached a members' page: ended at %s", res.Request.URL.Path)
	}
	if code, _ := call(t, outsider, "PATCH", srv.URL+"/o/alpha/api/v1/onboarding-steps/1", "application/json", map[string]any{"done": true}); code != http.StatusUnauthorized {
		t.Errorf("a customer's browser posting to a members' route got %d", code)
	}

	// Asking again mails only the invited address, not more often than the gap, and says
	// the same thing whether or not anything was sent.
	if _, page := as(t, second, "POST", link+"/again"); !strings.Contains(page, "Check your email") {
		t.Errorf("asking for a new link: %.200s", page)
	}
	box.take(t, 0)
	if res, page := as(t, second, "POST", srv.URL+"/c/no-such-link/again"); res.StatusCode != http.StatusOK || !strings.Contains(page, "Check your email") {
		t.Errorf("asking for a new link with one that never existed: %d %.200s", res.StatusCode, page)
	}
	if res, _ := as(t, second, "GET", srv.URL+"/c/no-such-link"); res.StatusCode != http.StatusNotFound {
		t.Errorf("looking at a link that never existed: %d", res.StatusCode)
	}
	ageCustomerLinks(t, s)
	as(t, second, "POST", link+"/again")
	fresh := box.take(t, 1)[0]
	if fresh.To != "hannah@larkmoor.example" {
		t.Fatalf("the new link went to %q", fresh.To)
	}
	freshLink := srv.URL + customerLink.FindStringSubmatch(fresh.Body)[1]
	if _, page := as(t, second, "POST", freshLink); !strings.Contains(page, "Send your product files") {
		t.Errorf("the second browser with a link of its own: %.200s", page)
	}

	// What a member marks shows on the customer's page.
	row := must(s.Onboardings(o.ID, 0, today()))[0]
	if err := s.SetOnboardingStep(o.ID, row.Steps[0].ID, "done", "yes", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if _, page := as(t, outsider, "GET", srv.URL+"/c"); !strings.Contains(page, "1 of 2") || strings.Contains(page, "Ada Lovelace") {
		t.Errorf("after a step was marked done: %.300s", page)
	}

	// Withdrawing access closes the page in every browser at once, and kills the links.
	grants := must(s.CustomerInvites(o.ID))[ob.ID]
	if len(grants) != 1 || grants[0].Name != "Hannah Pryce" {
		t.Fatalf("who can see the onboarding: %+v", grants)
	}
	if code, body := call(t, member, "DELETE", srv.URL+"/o/alpha/api/v1/onboarding-invites/"+itoa(grants[0].ID), "", nil); code != http.StatusNoContent {
		t.Fatalf("withdrawing access: %d %s", code, body)
	}
	for name, c := range map[string]*http.Client{"the first": outsider, "the second": second} {
		if _, page := as(t, c, "GET", srv.URL+"/c"); !strings.Contains(page, "Nothing to show here") {
			t.Errorf("%s browser still has the page after access was withdrawn", name)
		}
	}
	if res, _ := as(t, outsider, "GET", freshLink); res.StatusCode != http.StatusNotFound {
		t.Errorf("a link still answers after access was withdrawn: %d", res.StatusCode)
	}
	as(t, outsider, "POST", link+"/again")
	box.take(t, 0)

	// Inviting them again brings it back; removing the onboarding ends it.
	ageCustomerLinks(t, s)
	if code, body := invite(hannah.ID); code != http.StatusNoContent {
		t.Fatalf("inviting again: %d %s", code, body)
	}
	again := srv.URL + customerLink.FindStringSubmatch(box.take(t, 1)[0].Body)[1]
	if _, page := as(t, outsider, "POST", again); !strings.Contains(page, "1 of 2") {
		t.Fatalf("the page after being invited again: %.200s", page)
	}
	if err := s.RemoveOnboarding(o.ID, ob.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if _, page := as(t, outsider, "GET", srv.URL+"/c"); !strings.Contains(page, "Nothing to show here") {
		t.Error("the page outlived its onboarding")
	}
	var left int64
	for _, table := range []any{&CustomerGrant{}, &CustomerLink{}, &CustomerSession{}} {
		if err := s.db.Model(table).Where("org_id = ?", o.ID).Count(&left).Error; err != nil || left != 0 {
			t.Errorf("removing the onboarding left %d rows of %T (%v)", left, table, err)
		}
	}
}

func TestErasingAPersonClosesTheirPage(t *testing.T) {
	s, srv, member, box := mailSite(t, nil)
	o := must(s.OrgBySlug("alpha"))
	o.Settings.OnboardingSteps = "them: Send your product files"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	hannah := must(s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	ob := must(s.StartOnboarding(o.ID, OnboardingIn{Company: *hannah.CompanyID, Start: today()}, "Ada Lovelace"))
	if code, body := call(t, member, "POST", srv.URL+"/o/alpha/api/v1/onboardings/"+itoa(ob.ID)+"/invites", "application/json", map[string]any{"contact": hannah.ID}); code != http.StatusNoContent {
		t.Fatalf("inviting: %d %s", code, body)
	}
	link := srv.URL + customerLink.FindStringSubmatch(box.take(t, 1)[0].Body)[1]
	outsider := newMailClient()
	if _, page := as(t, outsider, "POST", link); !strings.Contains(page, "Send your product files") {
		t.Fatalf("opening the page: %.200s", page)
	}
	if export := must(s.ExportPerson(o.ID, hannah.ID, "Ada Lovelace")); len(export.Access) != 1 {
		t.Errorf("the person's export of what they can open: %+v", export.Access)
	}
	if err := s.ErasePerson(o.ID, hannah.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if _, page := as(t, outsider, "GET", srv.URL+"/c"); !strings.Contains(page, "Nothing to show here") || strings.Contains(page, "product files") {
		t.Error("an erased person's browser still has their page")
	}
	var left int64
	if err := s.db.Model(&CustomerGrant{}).Where("org_id = ?", o.ID).Count(&left).Error; err != nil || left != 0 {
		t.Errorf("erasing them left %d records of their access (%v)", left, err)
	}
}

// A link runs out, and so does a browser; neither opens anything afterwards.
func TestACustomersLinkAndBrowserRunOut(t *testing.T) {
	s, srv, member, box := mailSite(t, nil)
	o := must(s.OrgBySlug("alpha"))
	o.Settings.OnboardingSteps = "them: Send your product files"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	hannah := must(s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	ob := must(s.StartOnboarding(o.ID, OnboardingIn{Company: *hannah.CompanyID, Start: today()}, "Ada Lovelace"))
	if code, body := call(t, member, "POST", srv.URL+"/o/alpha/api/v1/onboardings/"+itoa(ob.ID)+"/invites", "application/json", map[string]any{"contact": hannah.ID}); code != http.StatusNoContent {
		t.Fatalf("inviting: %d %s", code, body)
	}
	link := srv.URL + customerLink.FindStringSubmatch(box.take(t, 1)[0].Body)[1]
	if err := s.db.Model(&CustomerLink{}).Where("1 = 1").Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	outsider := newMailClient()
	if _, page := as(t, outsider, "POST", link); !strings.Contains(page, "used or has run out") || strings.Contains(page, "product files") {
		t.Errorf("a link past its seven days: %.200s", page)
	}
	if err := s.db.Model(&CustomerLink{}).Where("1 = 1").Update("expires_at", time.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if _, page := as(t, outsider, "POST", link); !strings.Contains(page, "Send your product files") {
		t.Fatalf("opening the page: %.200s", page)
	}
	if err := s.db.Model(&CustomerSession{}).Where("1 = 1").Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if _, page := as(t, outsider, "GET", srv.URL+"/c"); !strings.Contains(page, "Nothing to show here") {
		t.Error("a browser past its thirty days still has the page")
	}
	// Leaving closes it in that browser.
	if err := s.db.Model(&CustomerSession{}).Where("1 = 1").Update("expires_at", time.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if _, page := as(t, outsider, "GET", srv.URL+"/c"); !strings.Contains(page, "Send your product files") {
		t.Fatal("the page didn't come back when the browser was in date again")
	}
	if _, page := as(t, outsider, "POST", srv.URL+"/c/leave"); !strings.Contains(page, "Nothing to show here") {
		t.Errorf("after closing the page in this browser: %.200s", page)
	}
	var sessions int64
	if err := s.db.Model(&CustomerSession{}).Count(&sessions).Error; err != nil || sessions != 0 {
		t.Errorf("closing the page left %d browsers on record (%v)", sessions, err)
	}
}

// Without a mail server and a public address nobody can be invited: access with no way
// to send the link is no use, and a link is never built from a request's own host.
func TestNobodyIsInvitedWithoutMail(t *testing.T) {
	s, o, srv, member := signedIn(t)
	o.Settings.OnboardingSteps = "them: Send your product files"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	hannah := must(s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	ob := must(s.StartOnboarding(o.ID, OnboardingIn{Company: *hannah.CompanyID, Start: today()}, "Ada Lovelace"))
	code, body := call(t, member, "POST", srv.URL+"/o/alpha/api/v1/onboardings/"+itoa(ob.ID)+"/invites", "application/json", map[string]any{"contact": hannah.ID})
	if code < 300 || !strings.Contains(body, "mail isn't set up") {
		t.Errorf("inviting with no mail server: %d %s", code, body)
	}
	if grants := must(s.CustomerInvites(o.ID)); len(grants) != 0 {
		t.Errorf("someone was let in though no link could be sent: %+v", grants)
	}
	if _, page := call(t, member, "GET", srv.URL+"/o/alpha/onboarding", "", nil); strings.Contains(page, `/invites"`) || !strings.Contains(page, "PROSPECTOR_URL") {
		t.Error("the onboarding page offers to invite, or doesn't say what is missing")
	}
}

// What made someone invitable has to hold each time, not only on the day they were invited.
func TestACustomersPageClosesWhenTheyNoLongerQualify(t *testing.T) {
	s, srv, member, box := mailSite(t, nil)
	o := must(s.OrgBySlug("alpha"))
	o.Settings.OnboardingSteps = "them: Send your product files"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	hannah := must(s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	other := must(s.QuickAddPerson(o, QuickAdd{Name: "Sven Dahl", Company: "Nordkap Boats", Email: "sven@nordkap.example"}, "Ada Lovelace"))
	ob := must(s.StartOnboarding(o.ID, OnboardingIn{Company: *hannah.CompanyID, Start: today()}, "Ada Lovelace"))
	if code, body := call(t, member, "POST", srv.URL+"/o/alpha/api/v1/onboardings/"+itoa(ob.ID)+"/invites", "application/json", map[string]any{"contact": hannah.ID}); code != http.StatusNoContent {
		t.Fatalf("inviting: %d %s", code, body)
	}
	link := srv.URL + customerLink.FindStringSubmatch(box.take(t, 1)[0].Body)[1]
	outsider := newMailClient()
	open := func() bool {
		_, page := as(t, outsider, "GET", srv.URL+"/c")
		return strings.Contains(page, "Send your product files")
	}
	if as(t, outsider, "POST", link); !open() {
		t.Fatal("the page did not open")
	}
	set := func(column string, value any) {
		t.Helper()
		if err := s.db.Model(&Contact{}).Where("id = ?", hannah.ID).Update(column, value).Error; err != nil {
			t.Fatal(err)
		}
	}
	again := func() int {
		t.Helper()
		ageCustomerLinks(t, s)
		as(t, newMailClient(), "POST", link+"/again")
		box.mu.Lock()
		defer box.mu.Unlock()
		n := len(box.all)
		box.all = nil
		return n
	}

	set("company_id", *other.CompanyID)
	if open() || again() != 0 {
		t.Error("someone who moved to another company kept the page, or was mailed a new link to it")
	}
	set("company_id", *hannah.CompanyID)
	if !open() {
		t.Fatal("the page did not come back when they were at the company again")
	}

	o.Settings.NeverContact = "larkmoor.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	if open() || again() != 0 {
		t.Error("someone put on the never-contact list kept the page, or was mailed")
	}
	o.Settings.NeverContact = ""
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}

	set("email", "hannah@larkmoor.example, other@elsewhere.example")
	if open() || again() != 0 {
		t.Error("someone whose address became two addresses kept the page, or was mailed")
	}
	set("email", nil)
	if open() {
		t.Error("someone with no address kept the page")
	}
	// A changed address is still them: the next link goes to the new one, and only there.
	set("email", "hannah.pryce@larkmoor.example")
	if !open() {
		t.Fatal("the page did not come back with a good address")
	}
	ageCustomerLinks(t, s)
	as(t, newMailClient(), "POST", link+"/again")
	if sent := box.take(t, 1); sent[0].To != "hannah.pryce@larkmoor.example" {
		t.Errorf("the new link went to %q", sent[0].To)
	}

	// A send that fails does not use up the wait before the next.
	ageCustomerLinks(t, s)
	box.mu.Lock()
	box.fail = errors.New("the mail server is away")
	box.mu.Unlock()
	as(t, newMailClient(), "POST", link+"/again")
	box.mu.Lock()
	box.fail = nil
	box.mu.Unlock()
	as(t, newMailClient(), "POST", link+"/again")
	box.take(t, 1)

	// A restore that gives them another id takes their page with them, and one that
	// leaves them out closes it.
	var kept Contact
	if err := s.db.First(&kept, hannah.ID).Error; err != nil {
		t.Fatal(err)
	}
	kept.ID = hannah.ID + 500
	if _, err := s.replaceContacts(o.ID, []Contact{kept}, nil); err != nil {
		t.Fatal(err)
	}
	if grants := must(s.CustomerInvites(o.ID))[ob.ID]; len(grants) != 1 || grants[0].ContactID != kept.ID || !open() {
		t.Errorf("after a restore that renumbered them: %+v, page open %v", grants, open())
	}
	someone := "Someone Else"
	stranger := Contact{ID: kept.ID + 1, Key: "someone else", File: "restore", Person: &someone}
	if _, err := s.replaceContacts(o.ID, []Contact{stranger}, nil); err != nil {
		t.Fatal(err)
	}
	if grants := must(s.CustomerInvites(o.ID)); len(grants) != 0 || open() {
		t.Errorf("after a restore without them: %+v, page open %v", grants, open())
	}
}

// A customer marks their own steps, and only those: not ours, not another onboarding's,
// and not over a mark a member made.
func TestACustomerMarksTheirOwnSteps(t *testing.T) {
	s, srv, member, box := mailSite(t, nil)
	o := must(s.OrgBySlug("alpha"))
	o.Settings.OnboardingSteps = "them: Send your product files\nus: Internal credit check\nthem: Approve the first renders"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	hannah := must(s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	sven := must(s.QuickAddPerson(o, QuickAdd{Name: "Sven Dahl", Company: "Nordkap Boats", Email: "sven@nordkap.example"}, "Ada Lovelace"))
	ob := must(s.StartOnboarding(o.ID, OnboardingIn{Company: *hannah.CompanyID, Start: today()}, "Ada Lovelace"))
	other := must(s.StartOnboarding(o.ID, OnboardingIn{Company: *sven.CompanyID, Start: today()}, "Ada Lovelace"))
	browser := func(onboarding, contact int64) *http.Client {
		t.Helper()
		ageCustomerLinks(t, s)
		if code, body := call(t, member, "POST", srv.URL+"/o/alpha/api/v1/onboardings/"+itoa(onboarding)+"/invites", "application/json", map[string]any{"contact": contact}); code != http.StatusNoContent {
			t.Fatalf("inviting: %d %s", code, body)
		}
		c := newMailClient()
		as(t, c, "POST", srv.URL+customerLink.FindStringSubmatch(box.take(t, 1)[0].Body)[1])
		return c
	}
	theirs, svens := browser(ob.ID, hannah.ID), browser(other.ID, sven.ID)
	steps := func(onboarding int64) []OnboardingStep {
		t.Helper()
		for _, row := range must(s.Onboardings(o.ID, 0, today())) {
			if row.ID == onboarding {
				return row.Steps
			}
		}
		t.Fatal("no such onboarding")
		return nil
	}
	mark := func(c *http.Client, step int64, what string) string {
		t.Helper()
		res, page := as(t, c, "POST", srv.URL+"/c/steps/"+itoa(step)+"/"+what)
		if res.Request.URL.Path != "/c" {
			t.Fatalf("marking a step ended at %s", res.Request.URL.Path)
		}
		return page
	}
	files, credit, renders := steps(ob.ID)[0], steps(ob.ID)[1], steps(ob.ID)[2]

	if _, page := as(t, theirs, "GET", srv.URL+"/c"); strings.Count(page, "Mark done") != 2 || strings.Contains(page, "/c/steps/"+itoa(credit.ID)+"/") {
		t.Errorf("the page offers the wrong steps to mark: %.400s", page)
	}
	if page := mark(theirs, files.ID, "done"); !strings.Contains(page, "1 of 2") || !strings.Contains(page, "Not done yet") {
		t.Errorf("after marking a step: %.300s", page)
	}
	got := steps(ob.ID)[0]
	if got.DoneOn == nil || got.DoneBy != customerActor {
		t.Fatalf("the step: %+v", got)
	}
	if _, page := call(t, member, "GET", srv.URL+"/o/alpha/onboarding", "", nil); !strings.Contains(page, "the customer") {
		t.Error("the members' page does not say the customer marked it")
	}
	var entry Event
	if err := s.db.Where("org_id = ? and action = ?", o.ID, "onboard:done").Last(&entry).Error; err != nil || entry.ContactID != hannah.ID || entry.User != customerActor || deref(entry.Value) != "Send your product files" {
		t.Errorf("the history entry: %+v (%v)", entry, err)
	}

	// Not our steps, not another onboarding's, from either side, and not without a page.
	mark(theirs, credit.ID, "done")
	mark(theirs, steps(other.ID)[0].ID, "done")
	mark(svens, renders.ID, "done")
	mark(newMailClient(), renders.ID, "done")
	mark(member, renders.ID, "done")
	mark(svens, files.ID, "undone")
	if a, b := steps(ob.ID), steps(other.ID); a[1].DoneOn != nil || a[2].DoneOn != nil || a[0].DoneOn == nil || b[0].DoneOn != nil {
		t.Errorf("a step was marked that was not theirs to mark: %+v %+v", a, b)
	}

	// They take back their own mark, but not one a member made.
	if page := mark(theirs, files.ID, "undone"); !strings.Contains(page, "0 of 2") {
		t.Errorf("after taking a mark back: %.300s", page)
	}
	if err := s.SetOnboardingStep(o.ID, files.ID, "done", "yes", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if page := mark(theirs, files.ID, "undone"); !strings.Contains(page, "1 of 2") || strings.Contains(page, "/c/steps/"+itoa(files.ID)+"/undone") || strings.Contains(page, "Ada Lovelace") {
		t.Errorf("a member's mark was taken back, or offered to be: %.300s", page)
	}
	if got := steps(ob.ID)[0]; got.DoneBy != "Ada Lovelace" {
		t.Errorf("the step a member marked: %+v", got)
	}
	// Marking again what is done keeps who did it.
	mark(theirs, files.ID, "done")
	if got := steps(ob.ID)[0]; got.DoneBy != "Ada Lovelace" {
		t.Errorf("marking a done step again changed who did it: %+v", got)
	}

	// Withdrawn access marks nothing.
	grants := must(s.CustomerInvites(o.ID))[ob.ID]
	if err := s.RevokeCustomer(o.ID, grants[0].ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	mark(theirs, renders.ID, "done")
	if got := steps(ob.ID)[2]; got.DoneOn != nil {
		t.Error("a step was marked after access was withdrawn")
	}
	// A member cannot pass themselves off as the customer by taking the name.
	if name := (&Req{User: &User{Name: customerActor, Email: "ada@alpha.example"}}).Actor(); name == customerActor {
		t.Error("a member named as the customer acts under that name")
	}
}

// What someone outside can do to the record is bounded: pressing the two buttons in turn
// leaves the history as it was, a press that changes nothing writes nothing, and the
// buttons answer only to the page's own forms.
func TestACustomersMarksKeepTheHistoryHonest(t *testing.T) {
	s, srv, member, box := mailSite(t, nil)
	o := must(s.OrgBySlug("alpha"))
	o.Settings.OnboardingSteps = "them: Send your product files\nus: Internal credit check"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	hannah := must(s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	ob := must(s.StartOnboarding(o.ID, OnboardingIn{Company: *hannah.CompanyID, Start: today()}, "Ada Lovelace"))
	if code, body := call(t, member, "POST", srv.URL+"/o/alpha/api/v1/onboardings/"+itoa(ob.ID)+"/invites", "application/json", map[string]any{"contact": hannah.ID}); code != http.StatusNoContent {
		t.Fatalf("inviting: %d %s", code, body)
	}
	outsider := newMailClient()
	as(t, outsider, "POST", srv.URL+customerLink.FindStringSubmatch(box.take(t, 1)[0].Body)[1])
	row := must(s.Onboardings(o.ID, 0, today()))[0]
	files, credit := row.Steps[0], row.Steps[1]
	events := func() int64 {
		t.Helper()
		var n int64
		if err := s.db.Model(&Event{}).Where("org_id = ?", o.ID).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	done := func() bool {
		t.Helper()
		return must(s.Onboardings(o.ID, 0, today()))[0].Steps[0].DoneOn != nil
	}
	before := events()
	press := srv.URL + "/c/steps/" + itoa(files.ID)

	if res, _ := as(t, outsider, "GET", press+"/done"); res.StatusCode != http.StatusMethodNotAllowed || done() {
		t.Errorf("looking at a button's address: %d, done %v", res.StatusCode, done())
	}
	elsewhere, _ := http.NewRequest("POST", press+"/done", nil)
	elsewhere.Header.Set("Sec-Fetch-Site", "cross-site")
	elsewhere.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if res, err := outsider.Do(elsewhere); err != nil || res.StatusCode != http.StatusForbidden || done() {
		t.Errorf("another site pressing the button: %v %v, done %v", res, err, done())
	}
	as(t, outsider, "POST", srv.URL+"/c/steps/"+itoa(credit.ID)+"/done")
	as(t, outsider, "POST", press+"/undone")
	if events() != before {
		t.Errorf("presses that changed nothing wrote %d history entries", events()-before)
	}

	for range 5 {
		as(t, outsider, "POST", press+"/done")
		as(t, outsider, "POST", press+"/done")
		if events() != before+1 || !done() {
			t.Fatalf("after marking: %d new entries, done %v", events()-before, done())
		}
		as(t, outsider, "POST", press+"/undone")
		if events() != before || done() {
			t.Fatalf("after taking it back: %d new entries, done %v", events()-before, done())
		}
	}

	// Erasing them leaves the step done by "the customer" and nothing that says who.
	as(t, outsider, "POST", press+"/done")
	if err := s.ErasePerson(o.ID, hannah.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	var theirs int64
	if err := s.db.Model(&Event{}).Where("org_id = ? and contact_id = ?", o.ID, hannah.ID).Count(&theirs).Error; err != nil || theirs != 0 || !done() {
		t.Errorf("after erasing them: %d entries still theirs (%v), done %v", theirs, err, done())
	}
}
