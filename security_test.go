package main

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSafeNext(t *testing.T) {
	for next, want := range map[string]string{
		"/o/alpha/today":             "/o/alpha/today",
		"/o/alpha/add?popup=1&url=x": "/o/alpha/add?popup=1&url=x",
		"":                           "/",
		"https://evil.example":       "/",
		"//evil.example":             "/",
		"/\\evil.example":            "/",
		"/\t/evil.example":           "/",
		"/\n/evil.example":           "/",
		"/x\\..\\evil.example":       "/",
	} {
		if got := safeNext(next); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", next, got, want)
		}
	}
}

func TestLoginSlowsGuessing(t *testing.T) {
	s := tempStore(t)
	if _, err := s.CreateUser("ada@example.com", "Ada", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	app := &App{s: s}
	app.logins.now = func() time.Time { return now }
	srv := httptest.NewServer(app.Routes())
	t.Cleanup(srv.Close)
	attempt := func(email, password string) (int, error) {
		jar, _ := cookiejar.New(nil)
		res, err := (&http.Client{Jar: jar}).PostForm(srv.URL+"/login", url.Values{"email": {email}, "password": {password}})
		if err != nil {
			return 0, err
		}
		res.Body.Close()
		return res.StatusCode, nil
	}
	try := func(email, password string) int {
		t.Helper()
		code, err := attempt(email, password)
		if err != nil {
			t.Fatal(err)
		}
		return code
	}
	// burst sends n wrong passwords at once and counts those that were checked.
	burst := func(email string, n int) int {
		t.Helper()
		var wg sync.WaitGroup
		var checked atomic.Int32
		for range n {
			wg.Go(func() {
				code, err := attempt(email, "wrong")
				if err != nil {
					t.Error(err)
				} else if code != http.StatusTooManyRequests {
					checked.Add(1)
				}
			})
		}
		wg.Wait()
		return int(checked.Load())
	}
	for i := range freeAttempts {
		if code := try("Ada@Example.com", "wrong"); code != http.StatusUnauthorized {
			t.Fatalf("wrong password %d: %d", i+1, code)
		}
	}
	if code := try("ada@example.com", "correct horse battery"); code != http.StatusTooManyRequests {
		t.Fatalf("after %d failures the next attempt got %d", freeAttempts, code)
	}
	if code := try("someone-else@example.com", "wrong"); code != http.StatusUnauthorized {
		t.Fatalf("another email was slowed too: %d", code)
	}
	now = now.Add(2 * time.Second)
	if n := burst("ada@example.com", 8); n != 1 {
		t.Fatalf("%d of 8 parallel guesses were checked once the wait ran out", n)
	}
	now = now.Add(3 * time.Second)
	if code := try("ada@example.com", "correct horse battery"); code != http.StatusOK {
		t.Fatalf("the right password after the wait: %d", code)
	}
	if n := burst("burst@example.com", 40); n > freeAttempts+cap(hashSlots) {
		t.Fatalf("%d of 40 parallel guesses at a fresh email were checked", n)
	}

	for range 40 {
		app.logins.record("ada@example.com", false)
	}
	if wait := app.logins.wait("ada@example.com"); wait != maxLoginWait {
		t.Fatalf("the wait after 40 failures is %v", wait)
	}
	now = now.Add(forgetAfter + time.Second)
	if wait := app.logins.begin("ada@example.com"); wait != 0 {
		t.Fatalf("old failures still count: %v", wait)
	}
	if app.logins.record("ada@example.com", false); app.logins.wait("ada@example.com") != 0 {
		t.Fatal("one failure after a long gap was made to wait")
	}

	// A flood of other emails neither grows the map past its cap nor frees one that is waiting.
	for range freeAttempts {
		app.logins.record("held@example.com", false)
	}
	for i := range maxTracked + 50 {
		app.logins.record(strings.Repeat("x", 1<<12)+strconv.Itoa(i), false)
	}
	if len(app.logins.fails) > maxTracked {
		t.Fatalf("tracking %d emails", len(app.logins.fails))
	}
	if app.logins.wait("held@example.com") == 0 {
		t.Fatal("a flood of other emails cleared a wait")
	}
	for key := range app.logins.fails {
		if len(key) > 64 {
			t.Fatalf("a %d-byte key is held in memory", len(key))
		}
	}
	big, err := http.Post(srv.URL+"/login", "application/x-www-form-urlencoded", strings.NewReader("email="+strings.Repeat("x", 1<<17)))
	if err != nil {
		t.Fatal(err)
	}
	big.Body.Close()
	if big.StatusCode != http.StatusBadRequest {
		t.Fatalf("an oversized sign-in form got %d", big.StatusCode)
	}
}

func TestChangePassword(t *testing.T) {
	s, _, srv, client := signedIn(t)
	elsewhere, _, err := s.Login("ada@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	change := func(current, password string) int {
		code, _ := call(t, client, "POST", srv.URL+"/password", "application/json", map[string]string{"current": current, "new": password})
		return code
	}
	if code := change("wrong", "a brand new password"); code != http.StatusBadRequest {
		t.Fatalf("wrong current password: %d", code)
	}
	if code := change("correct horse battery", "short"); code != http.StatusBadRequest {
		t.Fatalf("short password: %d", code)
	}
	if code := change("correct horse battery", "a brand new password"); code != http.StatusOK {
		t.Fatalf("change: %d", code)
	}
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/settings", "", nil); !strings.Contains(body, "Your password") {
		t.Fatal("the session that changed the password was signed out")
	}
	if _, err := s.SessionUser(elsewhere); err == nil {
		t.Fatal("another session survived the password change")
	}
	if _, _, err := s.Login("ada@example.com", "correct horse battery"); err == nil {
		t.Fatal("the old password still works")
	}
	fresh, _, err := s.Login("ada@example.com", "a brand new password")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPassword("ada@example.com", "set from the command line"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser(fresh); err == nil {
		t.Fatal("a session survived a password reset from the CLI")
	}
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/settings", "", nil); strings.Contains(body, "Your password") {
		t.Fatal("the browser session survived a password reset from the CLI")
	}

	// Guessing the current password from an open session is slowed like guessing at sign-in.
	_, _, srv, client = signedIn(t)
	guess := func() int {
		code, _ := call(t, client, "POST", srv.URL+"/password", "application/json", map[string]string{"current": "a guess", "new": "a brand new password"})
		return code
	}
	for i := range freeAttempts {
		if code := guess(); code != http.StatusBadRequest {
			t.Fatalf("guess %d: %d", i+1, code)
		}
	}
	if code := guess(); code != http.StatusTooManyRequests {
		t.Fatalf("after %d wrong guesses the next got %d", freeAttempts, code)
	}
}

func TestNeverContactCatchesMistakes(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	p := readFixtures(t)
	if _, err := s.Import(o.ID, p); err != nil {
		t.Fatal(err)
	}
	c, err := s.FindOne(o.ID, "Hannah")
	if err != nil {
		t.Fatal(err)
	}
	o.Settings.NeverContact = "blocked.example\nlinkedin.com/in/off-limits"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(o.ID, c.ID, "email", "hannah@blocked.example", "test"); err == nil {
		t.Fatal("an edit set a never-contact email")
	}
	if err := s.Update(o.ID, c.ID, "linkedin", "https://www.linkedin.com/in/off-limits", "test"); err == nil {
		t.Fatal("an edit set a never-contact profile")
	}
	if err := s.Update(o.ID, c.ID, "email", "hannah@notblocked.example", "test"); err != nil {
		t.Fatalf("an address that only ends like a listed domain was refused: %v", err)
	}
	fresh := readFixtures(t)
	if fresh.Drafts[1].Email == "" {
		t.Fatal("the fixture draft has no email to list")
	}
	o.Settings.NeverContact = "blocked.example\n" + fresh.Drafts[1].Email
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Import(o.ID, fresh); err != nil {
		t.Fatalf("listing someone already stored broke the next import: %v", err)
	}
	fresh.Drafts[1].Linkedin = "https://www.linkedin.com/in/a-new-profile"
	fresh.Drafts[1].Email = strings.ToUpper(fresh.Drafts[1].Email)
	if _, err := s.Import(o.ID, fresh); err != nil {
		t.Fatalf("a listed person's unlisted link couldn't change: %v", err)
	}
	p.Drafts[0].Email = "someone@blocked.example"
	p.Drafts[0].Note = "changed"
	if _, err := s.Import(o.ID, p); err == nil || !strings.Contains(err.Error(), p.Drafts[0].Key) {
		t.Fatalf("an import with a never-contact address went through: %v", err)
	}
	var kept Contact
	s.in(o.ID).Where("key = ?", p.Drafts[0].Key).First(&kept)
	if deref(kept.Note) == "changed" {
		t.Fatal("a refused import still wrote")
	}
}

func TestMCPSessionsBelongToOneKey(t *testing.T) {
	s, o, srv, _ := signedIn(t)
	owner, _, err := s.CreateKey(o.ID, "claude", []string{"read", "research:write"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := s.CreateKey(o.ID, "reader", []string{"read"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := srv.URL + "/o/alpha/mcp"
	transport := &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: &http.Client{
		Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			r.Header.Set("Authorization", "Bearer "+owner)
			return http.DefaultTransport.RoundTrip(r)
		})}}
	ctx := context.Background()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	replay := func(key string) int {
		req, _ := http.NewRequest("POST", endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":99,"method":"tools/list"}`))
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("content-type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Session-Id", session.ID())
		req.Header.Set("Mcp-Protocol-Version", session.InitializeResult().ProtocolVersion)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := replay(owner); code != http.StatusOK {
		t.Fatalf("the key that opened the session: %d", code)
	}
	if code := replay(other); code != http.StatusForbidden {
		t.Fatalf("another key used the session: %d", code)
	}
	closing, _ := http.NewRequest("DELETE", endpoint, nil)
	closing.Header.Set("Authorization", "Bearer "+other)
	closing.Header.Set("Mcp-Session-Id", session.ID())
	closed, err := http.DefaultClient.Do(closing)
	if err != nil {
		t.Fatal(err)
	}
	closed.Body.Close()
	if closed.StatusCode != http.StatusForbidden {
		t.Fatalf("another key closed the session: %d", closed.StatusCode)
	}
	if code := replay(owner); code != http.StatusOK {
		t.Fatalf("the session after another key tried to close it: %d", code)
	}

	// A never-contact entry added while the session is open applies to its next call.
	if _, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "add_company", Arguments: map[string]any{"name": "Larkmoor Yachts"}}); err != nil {
		t.Fatal(err)
	}
	o.Settings.NeverContact = "blocked.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "add_contact", Arguments: map[string]any{
		"company": "Larkmoor Yachts", "name": "Max Muster", "email": "max@blocked.example", "source_url": "https://larkmoor.example/team"}})
	if err != nil || !res.IsError {
		t.Fatalf("an open session ignored a new never-contact entry: %v %+v", err, res)
	}
	if got := s.AgentActivity(o.ID, 20); len(got) != 1 || got[0].Actor != "claude" {
		t.Fatalf("agent activity: %+v", got)
	}
}

func TestTransportHeaders(t *testing.T) {
	_, _, srv, _ := signedIn(t)
	get := func(proto string) http.Header {
		req, _ := http.NewRequest("GET", srv.URL+"/healthz", nil)
		if proto != "" {
			req.Header.Set("X-Forwarded-Proto", proto)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.Header
	}
	if h := get("https"); h.Get("Strict-Transport-Security") == "" || h.Get("X-Frame-Options") != "DENY" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("behind HTTPS: %v", h)
	}
	if h := get(""); h.Get("Strict-Transport-Security") != "" {
		t.Fatal("HSTS sent over plain HTTP")
	}
}

func TestLoginRedirects(t *testing.T) {
	_, _, srv, _ := signedIn(t)
	stay := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	wanted := "/o/alpha/add?popup=1&url=https%3A%2F%2Fwww.linkedin.com%2Fin%2Fhannah-pryce&name=Hannah"
	res, err := stay.Get(srv.URL + wanted)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	to, err := url.Parse(res.Header.Get("Location"))
	if err != nil || to.Path != "/login" || to.Query().Get("next") != wanted {
		t.Fatalf("signed out, sent to %q", res.Header.Get("Location"))
	}
	login := func(next string) string {
		res, err := stay.PostForm(srv.URL+"/login", url.Values{"email": {"ada@example.com"}, "password": {"correct horse battery"}, "next": {next}})
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusSeeOther {
			t.Fatalf("login: %d", res.StatusCode)
		}
		return res.Header.Get("Location")
	}
	if got := login(wanted); got != wanted {
		t.Fatalf("after signing in, sent to %q", got)
	}
	if got := login("/\t/evil.example"); got != "/" {
		t.Fatalf("a tab in next sent the browser to %q", got)
	}
}

func TestRemoveMember(t *testing.T) {
	s, o, srv, ada := signedIn(t)
	other := mustOrg(t, s, "beta")
	join := func(email string, org *Org, role string) (*User, *http.Client) {
		t.Helper()
		u, err := s.UserByEmail(email)
		if err != nil {
			if u, err = s.CreateUser(email, strings.Split(email, "@")[0], "correct horse battery"); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.AddMember(org.ID, u.ID, role, u.Name); err != nil {
			t.Fatal(err)
		}
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar}
		res, err := client.PostForm(srv.URL+"/login", url.Values{"email": {email}, "password": {"correct horse battery"}})
		if err != nil || res.StatusCode != http.StatusOK {
			t.Fatalf("login %s: %v", email, err)
		}
		res.Body.Close()
		return u, client
	}
	bob, bobClient := join("bob@example.com", o, "member")
	join("bob@example.com", other, "owner")
	adaUser, err := s.UserByEmail("ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	remove := func(client *http.Client, user int64) (int, string) {
		return call(t, client, "POST", srv.URL+"/o/alpha/ui/member/remove", "application/json", map[string]int64{"user_id": user})
	}

	if code, _ := remove(bobClient, adaUser.ID); code != http.StatusForbidden {
		t.Fatalf("a member removed someone: %d", code)
	}
	if code, _ := remove(ada, adaUser.ID); code != http.StatusBadRequest {
		t.Fatalf("the last owner left: %d", code)
	}
	if code, _ := call(t, ada, "POST", srv.URL+"/o/alpha/ui/member", "application/json",
		map[string]string{"email": "ada@example.com", "role": "member"}); code != http.StatusBadRequest {
		t.Fatalf("the last owner was demoted: %d", code)
	}
	if m, err := s.Membership(o.ID, adaUser.ID); err != nil || m.Role != "owner" {
		t.Fatalf("the last owner is now %+v %v", m, err)
	}
	if code, _ := remove(ada, 987654); code != http.StatusBadRequest {
		t.Fatalf("removing someone who isn't a member: %d", code)
	}

	if _, body := call(t, ada, "GET", srv.URL+"/o/alpha/settings", "", nil); !strings.Contains(body, "ui/member/remove") {
		t.Fatal("an owner isn't offered Remove")
	}
	if _, body := call(t, bobClient, "GET", srv.URL+"/o/alpha/settings", "", nil); !strings.Contains(body, "Your password") || strings.Contains(body, "ui/member/remove") {
		t.Fatal("a member is offered Remove")
	}
	if code, _ := remove(ada, bob.ID); code != http.StatusOK {
		t.Fatalf("remove: %d", code)
	}
	if code, _ := call(t, bobClient, "GET", srv.URL+"/o/alpha/today", "", nil); code != http.StatusNotFound {
		t.Fatalf("a removed member still reaches the organisation: %d", code)
	}
	if _, body := call(t, bobClient, "GET", srv.URL+"/o/beta/settings", "", nil); !strings.Contains(body, "Your password") {
		t.Fatal("removal from one organisation reached another")
	}
	if _, err := s.UserByEmail("bob@example.com"); err != nil {
		t.Fatal("removing a member deleted their account")
	}

	join("carol@example.com", o, "owner")
	code, body := remove(ada, adaUser.ID)
	if code != http.StatusOK || !strings.Contains(body, `"go":"/"`) {
		t.Fatalf("an owner leaving with another owner in place: %d %s", code, body)
	}
	if code, _ := call(t, ada, "GET", srv.URL+"/o/alpha/today", "", nil); code != http.StatusNotFound {
		t.Fatalf("an owner who left still reaches the organisation: %d", code)
	}
	carol, _ := s.UserByEmail("carol@example.com")
	if m, err := s.Membership(o.ID, carol.ID); err != nil || m.Role != "owner" {
		t.Fatalf("the remaining owner is now %+v %v", m, err)
	}
	if _, err := s.RemoveMember(o.ID, carol.ID, "test"); err == nil {
		t.Fatal("the organisation was left with no owner")
	}
	if err := s.AddMember(o.ID, carol.ID, "", carol.Name); err == nil {
		t.Fatal("an empty role was accepted")
	}
	var removals int64
	s.in(o.ID).Model(&Event{}).Where("action = ?", "member:remove").Count(&removals)
	if removals != 2 {
		t.Fatalf("%d removals in the history, want 2", removals)
	}
}

func TestNeverContactLeavesTheQueue(t *testing.T) {
	s, o, srv, client := signedIn(t)
	c, err := s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah.pryce@larkmoor.example",
		Linkedin: "https://www.linkedin.com/in/hannah-pryce"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	email := deref(c.Email)
	domain := email[strings.Index(email, "@")+1:]
	now := time.Now()
	queued := func(step, list string) bool {
		t.Helper()
		rows, err := s.Due(o.ID, step, now, OrgSettings{NeverContact: list})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.ID == c.ID {
				return true
			}
		}
		return false
	}
	do := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	steps := []struct {
		step  string
		reach func()
	}{
		{"linkedin", func() {}},
		{"call", func() { do(s.Update(o.ID, c.ID, "phone", "+44 20 7946 0000", "test")) }},
		{"email", func() { do(s.Mark(o.ID, c.ID, "linkedin", "2020-01-01", "test")) }},
		{"accepted", func() { do(s.Mark(o.ID, c.ID, "accepted", "2020-01-02", "test")) }},
		{"followup", func() { do(s.Mark(o.ID, c.ID, "email", "2020-01-03", "test")) }},
	}
	for _, st := range steps {
		st.reach()
		if !queued(st.step, "") {
			t.Fatalf("%s: the contact isn't due with an empty list", st.step)
		}
		for _, list := range []string{domain, strings.ToUpper(email), "other.example\n" + email, "linkedin.com/in/hannah-pryce"} {
			if queued(st.step, list) {
				t.Fatalf("%s: still due with %q on the list", st.step, list)
			}
		}
		// The domain rule matches from the @, so a domain that only ends the same stays.
		if !queued(st.step, domain[1:]) {
			t.Fatalf("%s: dropped by %q, which isn't their domain", st.step, domain[1:])
		}
	}

	settings := func() string {
		_, body := call(t, client, "GET", srv.URL+"/o/alpha/settings", "", nil)
		return body
	}
	if strings.Contains(settings(), "out of the queue") {
		t.Fatal("settings reports people held back by an empty list")
	}
	o.Settings.NeverContact = domain
	do(s.SaveOrg(o))
	if !strings.Contains(settings(), "keeps 1 person already here out of the queue: Hannah Pryce.") {
		t.Fatal("settings doesn't name who the list holds back")
	}
	do(s.Mark(o.ID, c.ID, "replied", "2020-01-04", "test"))
	if strings.Contains(settings(), "out of the queue") {
		t.Fatal("settings counts someone who has replied, whom the list doesn't hide")
	}
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/conversations", "", nil); !strings.Contains(body, "Hannah") {
		t.Fatal("a listed person who replied left Conversations")
	}
	var back Contact
	do(s.in(o.ID).First(&back, c.ID).Error)
	do(s.db.Model(&back).Updates(map[string]any{"replied": nil, "stage": ""}).Error)
	fresh, err := s.OrgByID(o.ID)
	do(err)
	due, err := s.AgentDue(fresh)
	do(err)
	for _, item := range due {
		if item.ContactID == c.ID {
			t.Fatalf("an agent is told a listed person is due: %+v", item)
		}
	}
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/today", "", nil); strings.Contains(body, "Hannah") {
		t.Fatal("a listed person is on Today")
	}
	o.Settings.NeverContact = ""
	do(s.SaveOrg(o))
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/today", "", nil); !strings.Contains(body, "Hannah") {
		t.Fatal("unlisting didn't bring the person back to Today")
	}
}

func TestNeverContactEntries(t *testing.T) {
	const email, profile = "hannah.pryce@larkmoor.example", "https://www.linkedin.com/in/hannah-pryce?trk=x"
	search := "https://www.linkedin.com/search/results/people/?keywords=Hannah%20Pryce%20Larkmoor"
	for _, c := range []struct {
		entry, email, link string
		listed             bool
	}{
		{"larkmoor.example", email, "", true},
		{"https://www.larkmoor.example/", email, "", true},
		{"LARKMOOR.example", email, "", true},
		{"example", email, "", true},
		{"hannah.pryce@larkmoor.example", email, "", true},
		{"pryce@larkmoor.example", email, "", false},
		{"moor.example", email, "", false},
		{"larkmoor", email, search, false},
		{"The Larkmoor Company", email, search, false},
		{"hannah", email, profile, false},
		{"linkedin.com", email, profile, false},
		{"linkedin.com/", email, profile, false},
		{"https", email, profile, false},
		{"linkedin.com/in/hannah-pryce", "", profile, true},
		{"https://uk.linkedin.com/in/hannah-pryce/", "", profile, true},
		{"linkedin.com/in/hannah", "", profile, false},
		{"linkedin.com/in/hannah-pryce-2", "", profile, false},
		{"linkedin.com/in/hannah-pryce", "", "https://www.linkedin.com/in/hannah-pryce/details/experience", true},
		{"linkedin.com/in/hannah-pryce", email, "https://linkedin.com/", false},
		{"other.example, larkmoor.example", email, "", true},
		{"", email, profile, false},
	} {
		got := OrgSettings{NeverContact: c.entry}.listed(c.email, c.link) != nil
		if got != c.listed {
			t.Errorf("entry %q against %q %q: listed = %v, want %v", c.entry, c.email, c.link, got, c.listed)
		}
	}
}

func TestContentPolicy(t *testing.T) {
	s, o, srv, client := signedIn(t)
	if _, err := s.Import(o.ID, readFixtures(t)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateKey(o.ID, "bot", []string{"read"}, "test"); err != nil {
		t.Fatal(err)
	}
	var company Company
	if err := s.in(o.ID).First(&company).Error; err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(srv.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	policy := res.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "script-src 'self';", "frame-ancestors 'none'", "base-uri 'none'", "form-action 'self'"} {
		if !strings.Contains(policy, want) {
			t.Errorf("policy %q lacks %q", policy, want)
		}
	}
	if scripts := policy[strings.Index(policy, "script-src"):]; strings.Contains(scripts[:strings.Index(scripts, ";")], "unsafe") {
		t.Errorf("the policy allows inline or evaluated scripts: %q", policy)
	}

	// The policy blocks inline scripts and handlers, so no page may depend on one.
	tag := regexp.MustCompile(`(?i)<script[^>]*>`)
	handler := regexp.MustCompile(`(?i)<[^>]*\son[a-z]+\s*=`)
	pages := []string{"/login", "/o/alpha/today", "/o/alpha/today?step=linkedin", "/o/alpha/research", "/o/alpha/research?f=rejected",
		"/o/alpha/conversations", "/o/alpha/companies", "/o/alpha/companies/" + strconv.FormatInt(company.ID, 10),
		"/o/alpha/add", "/o/alpha/add?popup=1&name=Hannah", "/o/alpha/settings", "/o/alpha/agents"}
	for _, page := range pages {
		code, body := call(t, client, "GET", srv.URL+page, "", nil)
		if code != http.StatusOK {
			t.Fatalf("GET %s: %d", page, code)
		}
		for _, script := range tag.FindAllString(body, -1) {
			if !strings.Contains(script, "src=") {
				t.Errorf("%s has an inline script: %s", page, script)
			}
		}
		if found := handler.FindString(body); found != "" {
			t.Errorf("%s has an inline event handler: %s", page, found)
		}
	}
}

func TestOrgIsCreatedWithItsOwner(t *testing.T) {
	s, _, srv, client := signedIn(t)
	ada, err := s.UserByEmail("ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	code, body := call(t, client, "POST", srv.URL+"/orgs", "application/json", map[string]string{"name": "Gamma Boats"})
	if code != http.StatusOK || !strings.Contains(body, "/o/gamma-boats/settings") {
		t.Fatalf("new organisation: %d %s", code, body)
	}
	gamma, err := s.OrgBySlug("gamma-boats")
	if err != nil {
		t.Fatal(err)
	}
	if m, err := s.Membership(gamma.ID, ada.ID); err != nil || m.Role != "owner" || m.CallerName != ada.Name {
		t.Fatalf("the creator's membership: %+v %v", m, err)
	}

	// The owner's row can't be written (one is already there for the id the next
	// organisation will get), so the organisation must not be left behind either.
	if err := s.db.Create(&Membership{OrgID: gamma.ID + 1, UserID: ada.ID, Role: "member"}).Error; err != nil {
		t.Fatal(err)
	}
	if o, err := s.CreateOrgWithOwner("delta", "Delta", ada); err == nil {
		t.Fatalf("expected the owner's row to be refused, got organisation %d", o.ID)
	}
	if _, err := s.OrgBySlug("delta"); err == nil {
		t.Fatal("an organisation was left with nobody in it")
	}
	if code, _ := call(t, client, "POST", srv.URL+"/orgs", "application/json", map[string]string{"name": "!!!"}); code != http.StatusBadRequest {
		t.Fatalf("a name with no usable characters: %d", code)
	}
}
