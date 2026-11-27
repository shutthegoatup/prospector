package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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

	"github.com/shutthegoatup/prospector/internal/paging"
)

func TestSafeNext(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
		res, err := signInWith(&http.Client{Jar: jar}, srv.URL, email, password)
		if err != nil {
			return 0, err
		}
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
	big, err := http.Post(srv.URL+"/api/v1/session", "application/json", strings.NewReader(`{"email": "`+strings.Repeat("x", 1<<17)+`", "password": "x"}`))
	if err != nil {
		t.Fatal(err)
	}
	big.Body.Close()
	if big.StatusCode != http.StatusBadRequest {
		t.Fatalf("an oversized sign-in got %d", big.StatusCode)
	}
}

func TestChangePassword(t *testing.T) {
	t.Parallel()
	s, _, srv, client := signedIn(t)
	elsewhere, _, err := s.Login("ada@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	change := func(current, password string) int {
		code, _ := call(t, client, "PUT", srv.URL+"/api/v1/password", "application/json", map[string]string{"current": current, "new": password})
		return code
	}
	if code := change("wrong", "a brand new password"); code != http.StatusBadRequest {
		t.Fatalf("wrong current password: %d", code)
	}
	if code := change("correct horse battery", "short"); code != http.StatusBadRequest {
		t.Fatalf("short password: %d", code)
	}
	if code := change("correct horse battery", "a brand new password"); code != http.StatusNoContent {
		t.Fatalf("change: %d", code)
	}
	if code, _ := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/me", "", nil); code != http.StatusOK {
		t.Fatalf("the session that changed the password was signed out: %d", code)
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
	if code, _ := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/me", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("the browser session survived a password reset from the CLI: %d", code)
	}

	// Guessing the current password from an open session is slowed like guessing at sign-in.
	_, _, srv, client = signedIn(t)
	guess := func() int {
		code, _ := call(t, client, "PUT", srv.URL+"/api/v1/password", "application/json", map[string]string{"current": "a guess", "new": "a brand new password"})
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
	t.Parallel()
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
	t.Parallel()
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
	if got, _ := s.AgentActivity(o.ID, 1); len(got) != 1 || got[0].Actor != "claude" {
		t.Fatalf("agent activity: %+v", got)
	}
}

func TestOnlyAssetsAreKeptByABrowser(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	key, _, err := s.CreateKey(o.ID, "bot", []string{"read"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"/o/alpha/today":                           "no-store",
		"/o/alpha/companies":                       "no-store",
		"/login":                                   "no-store",
		"/o/alpha/api/v1/due":                      "no-store",
		"/o/alpha/api/v1/companies/no":             "no-store",
		"/assets/css/output.css":                   "no-cache",
		"/assets/css/output.css?v=" + assetVersion: "public, max-age=31536000, immutable",
	} {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		who := http.DefaultClient
		if !strings.Contains(path, "/api/") {
			req.Header.Del("Authorization")
			who = client
		}
		res, err := who.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if got := res.Header.Get("Cache-Control"); got != want {
			t.Errorf("%s: %d with Cache-Control %q, want %q", path, res.StatusCode, got, want)
		}
	}
}

func TestTransportHeaders(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	// The sign-in page goes on to next itself, through safeNext in web/src/lib/next.ts.
	if res, err := stay.Get(srv.URL + to.String()); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("the sign-in page it was sent to: %v %v", res, err)
	}
	for next, want := range map[string]string{wanted: wanted, "/\t/evil.example": "/", "//evil.example": "/"} {
		if got := safeNext(next); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", next, got, want)
		}
	}
}

func TestRemoveMember(t *testing.T) {
	t.Parallel()
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
		res, err := signInWith(client, srv.URL, email, "correct horse battery")
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
		return call(t, client, "DELETE", srv.URL+"/o/alpha/api/v1/members/"+itoa(user), "application/json", nil)
	}

	if code, _ := remove(bobClient, adaUser.ID); code != http.StatusForbidden {
		t.Fatalf("a member removed someone: %d", code)
	}
	if code, _ := remove(ada, adaUser.ID); code != http.StatusBadRequest {
		t.Fatalf("the last owner left: %d", code)
	}
	if code, _ := call(t, ada, "POST", srv.URL+"/o/alpha/api/v1/members", "application/json",
		map[string]string{"email": "ada@example.com", "role": "member"}); code != http.StatusBadRequest {
		t.Fatalf("the last owner was demoted: %d", code)
	}
	if m, err := s.Membership(o.ID, adaUser.ID); err != nil || m.Role != "owner" {
		t.Fatalf("the last owner is now %+v %v", m, err)
	}
	if code, _ := remove(ada, 987654); code != http.StatusBadRequest {
		t.Fatalf("removing someone who isn't a member: %d", code)
	}

	// Settings offers Remove to whomever the nav says is an owner.
	if _, body := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/nav", "", nil); !strings.Contains(body, `"owner":true`) {
		t.Fatalf("an owner isn't offered Remove: %s", body)
	}
	if code, body := call(t, bobClient, "GET", srv.URL+"/o/alpha/api/v1/nav", "", nil); code != http.StatusOK || !strings.Contains(body, `"owner":false`) {
		t.Fatalf("a member is offered Remove: %d %s", code, body)
	}
	if code, _ := remove(ada, bob.ID); code != http.StatusNoContent {
		t.Fatalf("remove: %d", code)
	}
	if code, _ := call(t, bobClient, "GET", srv.URL+"/o/alpha/today", "", nil); code != http.StatusNotFound {
		t.Fatalf("a removed member still reaches the organisation: %d", code)
	}
	if code, _ := call(t, bobClient, "GET", srv.URL+"/o/beta/api/v1/me", "", nil); code != http.StatusOK {
		t.Fatalf("removal from one organisation reached another: %d", code)
	}
	if _, err := s.UserByEmail("bob@example.com"); err != nil {
		t.Fatal("removing a member deleted their account")
	}

	join("carol@example.com", o, "owner")
	if code, body := remove(ada, adaUser.ID); code != http.StatusNoContent {
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
	t.Parallel()
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

	covers := func() string {
		_, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/never-contact/covers", "", nil)
		return body
	}
	if !strings.Contains(covers(), `"total":0`) {
		t.Fatalf("settings reports people held back by an empty list: %s", covers())
	}
	o.Settings.NeverContact = domain
	do(s.SaveOrg(o))
	if body := covers(); !strings.Contains(body, `"items":["Hannah Pryce"]`) || !strings.Contains(body, `"total":1`) {
		t.Fatalf("settings doesn't name who the list holds back: %s", body)
	}
	do(s.Mark(o.ID, c.ID, "replied", "2020-01-04", "test"))
	if !strings.Contains(covers(), `"total":0`) {
		t.Fatalf("settings counts someone who has replied, whom the list doesn't hide: %s", covers())
	}
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/conversations", "", nil); !strings.Contains(body, "Hannah") {
		t.Fatalf("a listed person who replied left Conversations: %s", body)
	}
	var back Contact
	do(s.in(o.ID).First(&back, c.ID).Error)
	do(s.db.Model(&back).Updates(map[string]any{"replied": nil, "stage": ""}).Error)
	fresh, err := s.OrgByID(o.ID)
	do(err)
	due, err := s.AgentDue(fresh, paging.Page{Limit: paging.Most})
	do(err)
	for _, item := range due.Items {
		if item.ContactID == c.ID {
			t.Fatalf("an agent is told a listed person is due: %+v", item)
		}
	}
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/today", "", nil); strings.Contains(body, "Hannah") {
		t.Fatal("a listed person is on Today")
	}
	o.Settings.NeverContact = ""
	do(s.SaveOrg(o))
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/today", "", nil); !strings.Contains(body, "Hannah") {
		t.Fatalf("unlisting didn't bring the person back to Today: %s", body)
	}
}

func TestNeverContactEntries(t *testing.T) {
	t.Parallel()
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
		got := OrgSettings{NeverContact: c.entry}.Listed(c.email, c.link) != nil
		if got != c.listed {
			t.Errorf("entry %q against %q %q: listed = %v, want %v", c.entry, c.email, c.link, got, c.listed)
		}
	}
}

func TestContentPolicy(t *testing.T) {
	t.Parallel()
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
	// A task with work and a note on it, so those parts of the pages are rendered.
	task := must(s.CreateTask(o.ID, "", `Makers with a "configurator" <script>alert(1)</script>`, "Ada Lovelace"))
	bot := Actor{Name: "agent:bot", KeyID: 1}
	must(s.AgentAddCompany(o.ID, bot, AddCompanyIn{Name: company.Name, Task: task.ID}))
	must(s.AgentNote(o.ID, bot, NoteIn{Task: task.ID, Company: company.Name, Kind: "skipped", Text: `No builder, only a <img src=x onerror=alert(1)> brochure.`}))
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
	pages := []string{"/login", "/api", "/o/alpha/today", "/o/alpha/today?step=linkedin", "/o/alpha/research", "/o/alpha/research?f=rejected", "/o/alpha/research?f=details",
		"/o/alpha/conversations", "/o/alpha/companies", "/o/alpha/companies/" + strconv.FormatInt(company.ID, 10),
		"/o/alpha/add", "/o/alpha/add?popup=1&name=Hannah", "/o/alpha/settings", "/o/alpha/agents", "/o/alpha/enquiries", "/o/alpha/subscriptions", "/o/alpha/onboarding", "/o/alpha/campaigns", "/c", "/u/never-made",
		"/o/alpha/tasks", "/o/alpha/tasks?closed=1", "/o/alpha/tasks/" + strconv.FormatInt(task.ID, 10),
		"/o/alpha/agreements/" + strconv.FormatInt(must(s.AddAgreement(o.ID, company.ID, "Service agreement", "test")).ID, 10) + "/prepare"}
	for _, p := range appPages {
		pages = append(pages, "/o/alpha/"+p)
	}
	check := func(page, marker string) {
		t.Helper()
		code, body := call(t, client, "GET", srv.URL+page, "", nil)
		if code != http.StatusOK || !strings.Contains(body, marker) {
			t.Fatalf("GET %s: %d, without %q", page, code, marker)
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
	for _, page := range pages {
		check(page, "<title>")
	}
	if err := s.db.Model(&User{}).Where("email = ?", "ada@example.com").Update("must_change_password", true).Error; err != nil {
		t.Fatal(err)
	}
	check("/password", `<div id="app"></div>`)
}

func TestOrgIsCreatedWithItsOwner(t *testing.T) {
	t.Parallel()
	s, _, srv, client := signedIn(t)
	ada, err := s.UserByEmail("ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	code, body := call(t, client, "POST", srv.URL+"/api/v1/orgs", "application/json", map[string]string{"name": "Gamma Boats"})
	if code != http.StatusCreated || !strings.Contains(body, `"slug":"gamma-boats"`) {
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
	// A name whose address is taken, here by the organisation every server makes, is
	// refused in words about organisations rather than the database's about an index.
	if err := s.EnsureFrontOrg(""); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Prospector", "gamma boats"} {
		code, body := call(t, client, "POST", srv.URL+"/api/v1/orgs", "application/json", map[string]string{"name": name})
		if code != http.StatusConflict || !strings.Contains(body, "already has the address /o/"+slugify(name)) || regexp.MustCompile(`(?i)unique|constraint|duplicate`).MatchString(body) {
			t.Errorf("a second organisation named %q: %d %s", name, code, body)
		}
	}
	if code, _ := call(t, client, "POST", srv.URL+"/api/v1/orgs", "application/json", map[string]string{"name": "!!!"}); code != http.StatusBadRequest {
		t.Fatalf("a name with no usable characters: %d", code)
	}
}

// Someone in no organisation is offered the page that starts one through the API, and
// lands on its settings.
func TestTheFirstOrganisationIsStartedThroughTheAPI(t *testing.T) {
	t.Parallel()
	s, _, srv, _ := signedIn(t)
	if _, err := s.CreateUser("grace@example.com", "Grace Hopper", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	grace := signInAs(t, srv, "grace@example.com", "correct horse battery")
	if _, page := call(t, grace, "GET", srv.URL+"/", "", nil); served(t, page).Page != "start" {
		t.Fatalf("the first-organisation page: %s", page)
	}
	code, body := call(t, grace, "POST", srv.URL+"/api/v1/orgs", "application/json", map[string]string{"name": "Grace Works"})
	var made struct{ Slug string }
	if code != http.StatusCreated || json.Unmarshal([]byte(body), &made) != nil || made.Slug == "" {
		t.Fatalf("starting it: %d %s", code, body)
	}
	if code, _ := call(t, grace, "GET", srv.URL+"/o/"+made.Slug+"/settings", "", nil); code != http.StatusOK {
		t.Fatalf("its settings: %d", code)
	}
	if code, _ := call(t, grace, "POST", srv.URL+"/orgs", "application/json", map[string]string{"name": "Again"}); code != http.StatusNotFound {
		t.Errorf("the old route still starts one: %d", code)
	}
}

func TestHistoryNamesPeopleInFull(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, email, want string }{
		{"Sam Reed", "sam.reed@example.com", "Sam Reed"},
		{"  Sam Hale ", "sam.hale@example.com", "Sam Hale"},
		{"", "nameless@example.com", "nameless@example.com"},
		{"agent:claude", "sly@example.com", "sly@example.com"},
		{"cli", "sly@example.com", "sly@example.com"},
		{"import", "sly@example.com", "sly@example.com"},
	} {
		if got := (&Req{User: &User{Name: c.name, Email: c.email}}).Actor(); got != c.want {
			t.Errorf("a person named %q is recorded as %q, want %q", c.name, got, c.want)
		}
	}
	if got := (&Req{User: &User{Name: "Sam Reed"}, Key: &APIKey{Name: "claude"}}).Actor(); got != "agent:claude" {
		t.Errorf("a key is recorded as %q", got)
	}

	s, o, srv, client := signedIn(t)
	code, _ := call(t, client, "POST", srv.URL+"/o/alpha/api/v1/people", "application/json",
		QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Linkedin: "https://www.linkedin.com/in/hannah-pryce"})
	if code != http.StatusCreated {
		t.Fatalf("quick add: %d", code)
	}
	c, err := s.FindOne(o.ID, "Hannah")
	if err != nil {
		t.Fatal(err)
	}
	history, _, err := s.History(o.ID, *c.CompanyID, 1)
	if err != nil || len(history) != 1 || history[0].Actor != "Ada Lovelace" {
		t.Fatalf("history: %+v %v", history, err)
	}
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/companies/"+strconv.FormatInt(*c.CompanyID, 10)+"/history", "", nil); !strings.Contains(body, `"actor":"Ada Lovelace"`) {
		t.Fatalf("the company page doesn't name who added the person: %s", body)
	}
	if got, _ := s.AgentActivity(o.ID, 1); len(got) != 0 {
		t.Fatalf("a person's action is listed as an agent's: %+v", got)
	}
}

func TestFirstPasswordMustBeChanged(t *testing.T) {
	t.Parallel()
	s, _, srv, ada := signedIn(t)
	code, body := call(t, ada, "POST", srv.URL+"/o/alpha/api/v1/members", "application/json",
		map[string]string{"email": "bob@example.com", "name": "Bob Stone", "role": "member"})
	_, after, found := strings.Cut(body, "shown once: ")
	if code != http.StatusCreated || !found {
		t.Fatalf("invite: %d %s", code, body)
	}
	given := strings.Fields(after)[0]
	jar, _ := cookiejar.New(nil)
	bob := &http.Client{Jar: jar}
	res, err := signInWith(bob, srv.URL, "bob@example.com", given)
	if err != nil {
		t.Fatal(err)
	}
	landing, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(landing), `"must_change_password":true`) {
		t.Fatalf("a new member's first sign-in didn't say a password must be chosen: %s", landing)
	}
	// landsOn is where a page sends Bob, after any redirects.
	landsOn := func(path string) string {
		res, _ := get(t, bob, srv.URL+path)
		return res.Request.URL.Path
	}
	change := func(current, password string) (int, string) {
		return call(t, bob, "PUT", srv.URL+"/api/v1/password", "application/json", map[string]string{"current": current, "new": password})
	}
	for _, path := range []string{"/", "/o/alpha/today", "/o/alpha/settings"} {
		if landsOn(path) != "/password" {
			t.Fatalf("%s is open before the first password is changed", path)
		}
	}
	if code, _ := call(t, bob, "POST", srv.URL+"/o/alpha/api/v1/people", "application/json", QuickAdd{Name: "Hannah Pryce"}); code != http.StatusForbidden {
		t.Fatalf("an action before the first password is changed: %d", code)
	}
	if code, _ := call(t, bob, "POST", srv.URL+"/api/v1/orgs", "application/json", map[string]string{"name": "Mine"}); code != http.StatusForbidden {
		t.Fatalf("a new organisation through the API before the first password is changed: %d", code)
	}
	if code, _ := change(given, given); code != http.StatusBadRequest {
		t.Fatalf("keeping the given password: %d", code)
	}
	if landsOn("/o/alpha/today") != "/password" {
		t.Fatal("a refused change opened the app")
	}
	if code, body := change(given, "a password of my own"); code != http.StatusNoContent {
		t.Fatalf("choosing a password: %d %s", code, body)
	}
	if landsOn("/o/alpha/settings") != "/o/alpha/settings" || landsOn("/password") == "/password" {
		t.Fatal("the app is still closed after choosing a password")
	}
	if code, body := call(t, bob, "GET", srv.URL+"/o/alpha/api/v1/me", "", nil); code != http.StatusOK || !strings.Contains(body, `"has_password":true`) {
		t.Fatalf("the API is still closed after choosing a password: %d %s", code, body)
	}
	if u, err := s.UserByEmail("bob@example.com"); err != nil || u.MustChangePassword {
		t.Fatalf("still marked for a change: %+v %v", u, err)
	}
	if _, _, err := s.Login("bob@example.com", given); err == nil {
		t.Fatal("the password the inviter saw still signs in")
	}
	if code, body := change("a password of my own", "another of my own"); code != http.StatusNoContent {
		t.Fatalf("an ordinary change afterwards: %d %s", code, body)
	}

	// Being added to another organisation, or made by the CLI, doesn't ask again.
	other := mustOrg(t, s, "beta")
	adaUser, _ := s.UserByEmail("ada@example.com")
	if err := s.AddMember(other.ID, adaUser.ID, "owner", adaUser.Name); err != nil {
		t.Fatal(err)
	}
	if code, body := call(t, ada, "POST", srv.URL+"/o/beta/api/v1/members", "application/json",
		map[string]string{"email": "bob@example.com", "role": "member"}); code != http.StatusCreated || strings.Contains(body, "shown once") {
		t.Fatalf("adding an existing account: %d %s", code, body)
	}
	if landsOn("/o/beta/settings") != "/o/beta/settings" {
		t.Fatal("an existing account was asked to choose a password again")
	}
	if res, _ := get(t, ada, srv.URL+"/password"); res.Request.URL.Path == "/password" {
		t.Fatal("an account made by the CLI wasn't sent straight on to the app")
	}
}

func TestOtherSitesCantPost(t *testing.T) {
	t.Parallel()
	s, _, srv, client := signedIn(t)
	host := strings.TrimPrefix(srv.URL, "http://")
	signedInAs := func() bool {
		code, _ := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/me", "", nil)
		return code == http.StatusOK
	}
	send := func(c *http.Client, method, path, body, ctype string, headers map[string]string) int {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("content-type", ctype)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	post := func(c *http.Client, path, body, ctype string, headers map[string]string) int {
		t.Helper()
		return send(c, "POST", path, body, ctype, headers)
	}
	login := `{"email": "ada@example.com", "password": "correct horse battery"}`
	stay := func() *http.Client {
		jar, _ := cookiejar.New(nil)
		return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}

	for name, c := range map[string]struct {
		headers map[string]string
		want    int
	}{
		"this site, a current browser":           {map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": srv.URL}, http.StatusOK},
		"typed address or bookmark":              {map[string]string{"Sec-Fetch-Site": "none"}, http.StatusOK},
		"this site, an older browser":            {map[string]string{"Origin": srv.URL}, http.StatusOK},
		"no browser headers (curl, the CLI)":     {nil, http.StatusOK},
		"another site, a current browser":        {map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}, http.StatusForbidden},
		"a sibling subdomain, a current browser": {map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "http://other." + host}, http.StatusForbidden},
		"another site, an older browser":         {map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
	} {
		if got := post(stay(), "/api/v1/session", login, "application/json", c.headers); got != c.want {
			t.Errorf("sign-in from %s: %d, want %d", name, got, c.want)
		}
	}

	// Another site can't sign this browser out, or act through it.
	cross := map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}
	if got := send(client, "DELETE", "/api/v1/session", "", "application/json", cross); got != http.StatusForbidden || !signedInAs() {
		t.Fatalf("sign-out from another site: %d, still signed in: %v", got, signedInAs())
	}
	if got := post(client, "/o/alpha/api/v1/people", `{"name":"Mallory"}`, "application/json", cross); got != http.StatusForbidden {
		t.Fatalf("an action from another site: %d", got)
	}
	if got := post(client, "/o/alpha/api/v1/people", `{"name":"Hannah Pryce"}`, "application/json",
		map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": srv.URL}); got != http.StatusCreated {
		t.Fatalf("an action from this site: %d", got)
	}

	// A key isn't a browser session, so where the request came from doesn't matter.
	o, _ := s.OrgBySlug("alpha")
	key, _, err := s.CreateKey(o.ID, "bot", []string{"read", "research:write"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	cross["Authorization"] = "Bearer " + key
	if got := post(http.DefaultClient, "/o/alpha/api/v1/companies", `{"name":"Quay Marine"}`, "application/json", cross); got != http.StatusOK {
		t.Fatalf("an API key with browser headers: %d", got)
	}
	delete(cross, "Authorization")
	if got := send(client, "DELETE", "/api/v1/session", "", "application/json", map[string]string{"Sec-Fetch-Site": "same-origin"}); got != http.StatusNoContent || signedInAs() {
		t.Fatalf("sign-out from this site: %d, still signed in: %v", got, signedInAs())
	}
}

func TestPublicURL(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{
		"":                                 "",
		"https://prospector.example.com":   "https://prospector.example.com",
		" https://prospector.example.com/": "https://prospector.example.com",
		"http://10.0.0.5:8766":             "http://10.0.0.5:8766",
	} {
		if got, err := publicURL(raw); err != nil || got != want {
			t.Errorf("publicURL(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"prospector.example.com", "ftp://x.example", "https://", "https://x.example/app", "https://x.example?a=1", "https://u:p@x.example"} {
		if got, err := publicURL(raw); err == nil {
			t.Errorf("publicURL(%q) = %q, want an error", raw, got)
		}
	}

	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	u, err := s.CreateUser("ada@example.com", "Ada Lovelace", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddMember(o.ID, u.ID, "owner", u.Name); err != nil {
		t.Fatal(err)
	}
	signIn := func(app *App, headers map[string]string) (*httptest.Server, *http.Cookie, http.Header) {
		t.Helper()
		srv := httptest.NewServer(app.Routes())
		t.Cleanup(srv.Close)
		req, _ := http.NewRequest("POST", srv.URL+"/api/v1/session", strings.NewReader(`{"email": "ada@example.com", "password": "correct horse battery"}`))
		req.Header.Set("content-type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		res, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		for _, c := range res.Cookies() {
			if c.Name == sessionCookie {
				return srv, c, res.Header
			}
		}
		t.Fatalf("no session cookie: %d", res.StatusCode)
		return nil, nil, nil
	}
	page := func(srv *httptest.Server, c *http.Cookie, path string) string {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.AddCookie(c)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return string(body)
	}

	// Not set: as before, the request decides.
	srv, c, h := signIn(&App{s: s}, nil)
	if c.Secure || h.Get("Strict-Transport-Security") != "" {
		t.Fatal("plain HTTP with no address set was treated as HTTPS")
	}
	if !strings.Contains(page(srv, c, "/o/alpha/api/v1/connect"), srv.URL+"/o/alpha/mcp") {
		t.Fatal("with no address set, the agents page doesn't use the request's host")
	}
	if _, c, _ := signIn(&App{s: s}, map[string]string{"X-Forwarded-Proto": "https"}); !c.Secure {
		t.Fatal("a proxy's X-Forwarded-Proto no longer marks the cookie Secure")
	}

	// Set to an https address: Secure whatever the proxy sends, and links use it.
	srv, c, h = signIn(&App{s: s, base: "https://prospector.example.com"}, nil)
	if !c.Secure || h.Get("Strict-Transport-Security") == "" {
		t.Fatalf("with an https address set: Secure=%v, HSTS=%q", c.Secure, h.Get("Strict-Transport-Security"))
	}
	if body := page(srv, c, "/o/alpha/api/v1/connect"); !strings.Contains(body, "https://prospector.example.com/o/alpha/mcp") || strings.Contains(body, srv.URL) {
		t.Fatal("the agents page doesn't use the address that was set")
	}
	if body := page(srv, c, "/o/alpha/api/v1/people/draft"); !strings.Contains(body, "https://prospector.example.com/o/alpha/add?popup=1") || strings.Contains(body, srv.URL) {
		t.Fatalf("the API's bookmarklet doesn't use the address that was set: %s", body)
	}
	if _, c, _ := signIn(&App{s: s, base: "http://10.0.0.5:8766"}, nil); c.Secure {
		t.Fatal("an http address marked the cookie Secure")
	}

	// A proxy that rewrites Host leaves Origin as the only sign of where a request came
	// from in a browser that sends no Sec-Fetch-Site (any browser, over plain HTTP).
	status := func(app *App, origin string) int {
		srv := httptest.NewServer(app.Routes())
		defer srv.Close()
		req, _ := http.NewRequest("POST", srv.URL+"/api/v1/session", strings.NewReader(`{"email": "ada@example.com", "password": "correct horse battery"}`))
		req.Header.Set("content-type", "application/json")
		req.Header.Set("Origin", origin)
		res, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if got := status(&App{s: s, base: "https://prospector.example.com"}, "https://prospector.example.com"); got != http.StatusOK {
		t.Fatalf("sign-in from the address that was set, behind a proxy that rewrites Host: %d", got)
	}
	if got := status(&App{s: s, base: "https://prospector.example.com"}, "https://evil.example"); got != http.StatusForbidden {
		t.Fatalf("sign-in from another site with an address set: %d", got)
	}
	if got := status(&App{s: s}, "https://prospector.example.com"); got != http.StatusForbidden {
		t.Fatalf("with no address set, an Origin that doesn't match Host: %d", got)
	}
}

func TestProblemsLookLikeTheApp(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	get := func(c *http.Client, method, path string, headers map[string]string) (int, string, string) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader("{}"))
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header.Get("Content-Type"), string(body)
	}
	browser := map[string]string{"Accept": "text/html,application/xhtml+xml,*/*;q=0.8"}
	styled := func(body string) bool {
		return dataPage.MatchString(body) && strings.Contains(body, `data-page="{&quot;page&quot;:&quot;problem&quot;`) && strings.Contains(body, "/assets/css/output.css")
	}

	for path, want := range map[string]string{
		"/o/nope/today":         "Nothing here",
		"/no/such/page":         "Nothing here",
		"/o/alpha/settings/odd": "Nothing here",
	} {
		code, ctype, body := get(client, "GET", path, browser)
		if code < 400 || !strings.HasPrefix(ctype, "text/html") || !styled(body) || !strings.Contains(body, want) {
			t.Errorf("GET %s in a browser: %d %s, styled=%v, want %q", path, code, ctype, styled(body), want)
		}
	}
	// What the app reads for a page answers as the API does, even to a browser; the app
	// shows the problem in its own page.
	for path, want := range map[string]int{
		"/o/alpha/api/v1/companies/98765/overview": http.StatusNotFound,
		"/o/alpha/api/v1/research?filter=nope":     http.StatusBadRequest,
	} {
		code, ctype, body := get(client, "GET", path, browser)
		if code != want || !strings.HasPrefix(ctype, "application/problem+json") || styled(body) {
			t.Errorf("GET %s in a browser: %d %s %s, want %d as a problem", path, code, ctype, body, want)
		}
	}
	crossSite := map[string]string{"Accept": browser["Accept"], "Sec-Fetch-Site": "cross-site", "content-type": "application/json"}
	code, _, body := get(client, "POST", "/c/some-link", crossSite)
	if code != http.StatusForbidden || !styled(body) || !strings.Contains(body, "This request came from another site.") {
		t.Fatalf("a form from another site, in a browser: %d, styled=%v", code, styled(body))
	}
	code, ctype, body := get(client, "POST", "/api/v1/session", crossSite)
	if code != http.StatusForbidden || ctype != "application/problem+json" || !strings.Contains(body, "this request came from another site") {
		t.Fatalf("a sign-in from another site, in a browser: %d %s %s, want a problem", code, ctype, body)
	}
	if h := func() http.Header {
		req, _ := http.NewRequest("GET", srv.URL+"/no/such/page", nil)
		req.Header.Set("Accept", browser["Accept"])
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.Header
	}(); h.Get("Content-Security-Policy") == "" || h.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("a problem page lacks the security headers: %v", h)
	}

	// The page script, the CLI and agents read the error as text, so theirs stay plain.
	for name, c := range map[string]struct {
		method, path string
		headers      map[string]string
	}{
		"the page script":  {"POST", "/password", map[string]string{"content-type": "application/json", "Accept": "*/*"}},
		"no Accept header": {"GET", "/o/nope/today", nil},
	} {
		code, ctype, body := get(client, c.method, c.path, c.headers)
		if code < 400 || !strings.HasPrefix(ctype, "text/plain") || styled(body) {
			t.Errorf("%s: %d %s, styled=%v", name, code, ctype, styled(body))
		}
	}
	key, _, err := s.CreateKey(o.ID, "bot", []string{"read"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	code, ctype, body = get(http.DefaultClient, "GET", "/o/alpha/api/v1/companies/nope", map[string]string{"Accept": "text/html", "Authorization": "Bearer " + key})
	if code != http.StatusNotFound || ctype != "application/problem+json" || styled(body) {
		t.Fatalf("an agent's error: %d %s, styled=%v", code, ctype, styled(body))
	}

	// What isn't a plain-text error passes through as it was: a redirect, the sign-in
	// page's own refusal, and a method the route doesn't take keeping its Allow header.
	stay := &http.Client{Jar: client.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	redirect, _ := http.NewRequest("GET", srv.URL+"/o/alpha/", nil)
	redirect.Header.Set("Accept", browser["Accept"])
	res, err := stay.Do(redirect)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/o/alpha/today" {
		t.Fatalf("a redirect in a browser: %d to %q", res.StatusCode, res.Header.Get("Location"))
	}
	wrong, _ := http.NewRequest("POST", srv.URL+"/api/v1/session", strings.NewReader(`{"email": "ada@example.com", "password": "wrong"}`))
	wrong.Header.Set("Accept", browser["Accept"])
	wrong.Header.Set("content-type", "application/json")
	res, err = http.DefaultClient.Do(wrong)
	if err != nil {
		t.Fatal(err)
	}
	said, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized || !strings.Contains(string(said), `"type":"/api/problems/wrong_login"`) || styled(string(said)) {
		t.Fatalf("a wrong password in a browser: %d %s", res.StatusCode, said)
	}
	method, _ := http.NewRequest("DELETE", srv.URL+"/login", nil)
	method.Header.Set("Accept", browser["Accept"])
	method.Header.Set("Sec-Fetch-Site", "same-origin")
	res, err = client.Do(method)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed || res.Header.Get("Allow") == "" {
		t.Fatalf("a method the route doesn't take: %d, Allow %q", res.StatusCode, res.Header.Get("Allow"))
	}
	if _, message := problemText(http.StatusUnauthorized, "an API key is needed: Authorization: Bearer pk_…"); strings.HasSuffix(message, "….") {
		t.Fatalf("a full stop was added after an ellipsis: %q", message)
	}

	// A proxy's Basic sign-in travels with every request the browser makes, another
	// site's included, so it doesn't count as a key.
	basic := map[string]string{"Authorization": "Basic dXNlcjpwYXNz", "Sec-Fetch-Site": "cross-site", "content-type": "application/x-www-form-urlencoded"}
	if code, _, _ := get(client, "POST", "/logout", basic); code != http.StatusForbidden {
		t.Fatalf("a cross-site post carrying a proxy's Basic sign-in: %d", code)
	}
	basic["Sec-Fetch-Site"] = "same-origin"
	if code, _, _ := get(client, "POST", "/o/alpha/api/v1/people", basic); code != http.StatusUnsupportedMediaType {
		t.Fatalf("a form post carrying a proxy's Basic sign-in skipped the JSON rule: %d", code)
	}

	// Pages that work are passed through untouched, and a server fault keeps its detail to itself.
	if code, ctype, body := get(client, "GET", "/o/alpha/today", browser); code != http.StatusOK || !strings.HasPrefix(ctype, "text/html") || styled(body) {
		t.Fatalf("a working page: %d %s", code, ctype)
	}
	if title, message := problemText(http.StatusInternalServerError, "pq: relation \"contacts\" does not exist"); title != "Something went wrong" || strings.Contains(message, "contacts") {
		t.Fatalf("a server fault shows %q %q", title, message)
	}
}

func TestRequestsHaveDeadlines(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	key, _, err := s.CreateKey(o.ID, "bot", []string{"read", "research:write"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	const limit = 300 * time.Millisecond
	var host string
	serve := func(app *App) {
		srv := httptest.NewServer(app.Routes())
		t.Cleanup(srv.Close)
		host = strings.TrimPrefix(srv.URL, "http://")
	}
	dial := func() (net.Conn, *bufio.Reader) {
		t.Helper()
		conn, err := net.Dial("tcp", host)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn, bufio.NewReader(conn)
	}
	answer := func(conn net.Conn, r *bufio.Reader, within time.Duration) (*http.Response, error) {
		conn.SetReadDeadline(time.Now().Add(within))
		res, err := http.ReadResponse(r, nil)
		if err != nil {
			return nil, err
		}
		io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
		return res, nil
	}
	head := func(method, path, extra string, length int) string {
		return fmt.Sprintf("%s %s HTTP/1.1\r\nHost: %s\r\n%sContent-Type: application/json\r\nAccept: application/json, text/event-stream\r\nContent-Length: %d\r\n\r\n", method, path, host, extra, length)
	}
	bearer := "Authorization: Bearer " + key + "\r\n"

	// A body that stops arriving is given up on, wherever it is sent: a page's form, a
	// path that doesn't exist, and the agents' route before any key has been accepted.
	serve(&App{s: s, readLimit: limit})
	for _, path := range []string{"/login", "/nope/mcp", "/o/alpha/mcp", "/o/alpha/api/v1/companies"} {
		conn, r := dial()
		fmt.Fprint(conn, head("POST", path, "", 200)+`{"name":`)
		began := time.Now()
		if res, err := answer(conn, r, 5*time.Second); err == nil && res.StatusCode < 400 {
			t.Fatalf("a stalled body to %s got %d", path, res.StatusCode)
		}
		if waited := time.Since(began); waited > 3*time.Second {
			t.Fatalf("a stalled body to %s was waited on for %v", path, waited)
		}
	}

	// A body that arrives slowly but in time is still answered: the answer's clock
	// starts when the body is in, and an agent's answer has none.
	serve(&App{s: s, writeLimit: limit})
	slowly := func(path, body string) (*http.Response, error) {
		conn, r := dial()
		fmt.Fprint(conn, head("POST", path, bearer, len(body))+body[:10])
		time.Sleep(3 * limit)
		fmt.Fprint(conn, body[10:])
		return answer(conn, r, 3*time.Second)
	}
	if res, err := slowly("/o/alpha/api/v1/companies", `{"name":"Quay Marine"}`); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("a slow upload to the API: %v %v", res, err)
	}
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
	res, err := slowly("/o/alpha/mcp", initialize)
	if err != nil || res.StatusCode != http.StatusOK || res.Header.Get("Mcp-Session-Id") == "" {
		t.Fatalf("a slow request from an agent: %v %v", res, err)
	}

	// An agent's listening stream stays open well past both limits.
	serve(&App{s: s, readLimit: limit, writeLimit: limit})
	conn, r := dial()
	fmt.Fprint(conn, head("POST", "/o/alpha/mcp", bearer, len(initialize))+initialize)
	if res, err = answer(conn, r, 3*time.Second); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("an agent's first request: %v %v", res, err)
	}
	agent := bearer + "Mcp-Session-Id: " + res.Header.Get("Mcp-Session-Id") + "\r\n"
	ready := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	fmt.Fprint(conn, head("POST", "/o/alpha/mcp", agent, len(ready))+ready)
	if res, err := answer(conn, r, 3*time.Second); err != nil || res.StatusCode >= 300 {
		t.Fatalf("finishing the agent's handshake: %v %v", res, err)
	}
	stream, sr := dial()
	fmt.Fprintf(stream, "GET /o/alpha/mcp HTTP/1.1\r\nHost: %s\r\n%sAccept: text/event-stream\r\n\r\n", host, agent)
	stream.SetReadDeadline(time.Now().Add(3 * time.Second))
	opened, err := http.ReadResponse(sr, nil)
	if err != nil || opened.StatusCode != http.StatusOK {
		t.Fatalf("opening the agent's stream: %v %v", opened, err)
	}
	time.Sleep(4 * limit)
	stream.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	// Whatever the server has already sent is read off; what matters is that the
	// read then waits rather than finding the stream closed.
	for err = error(nil); err == nil; {
		_, err = sr.ReadByte()
	}
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("the agent's stream was closed: %v", err)
	}

	// A page with no body isn't cut short by the limit on bodies.
	if a := (&App{}); a.readLimit != 0 || bodyLimit < answerLimit {
		t.Fatal("the defaults changed without this test")
	}
}

func TestFontsAreServedFromHere(t *testing.T) {
	t.Parallel()
	_, _, srv, client := signedIn(t)
	fetch := func(path string) (http.Header, []byte) {
		t.Helper()
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d", path, res.StatusCode)
		}
		return res.Header, body
	}
	h, css := fetch("/assets/css/fonts.css?v=" + assetVersion)
	if !strings.HasPrefix(h.Get("Content-Type"), "text/css") || !strings.Contains(h.Get("Cache-Control"), "immutable") {
		t.Fatalf("the font stylesheet: %v", h)
	}
	files := regexp.MustCompile(`url\((/assets/fonts/[a-z0-9-]+\.woff2)\)`).FindAllStringSubmatch(string(css), -1)
	if len(files) < 6 || strings.Contains(string(css), "http") {
		t.Fatalf("the font stylesheet names %d local files; it must name no other site", len(files))
	}
	for _, f := range files {
		h, font := fetch(f[1])
		if h.Get("Content-Type") != "font/woff2" || !strings.Contains(h.Get("Cache-Control"), "immutable") || !bytes.HasPrefix(font, []byte("wOF2")) {
			t.Errorf("%s: %s, %q, %d bytes", f[1], h.Get("Content-Type"), h.Get("Cache-Control"), len(font))
		}
	}
	for _, family := range []string{"Bricolage Grotesque", "IBM Plex Sans", "IBM Plex Mono"} {
		if !strings.Contains(string(css), "font-family: '"+family+"'") {
			t.Errorf("the font stylesheet lacks %s", family)
		}
	}
	if _, licence := fetch("/assets/fonts/OFL.txt"); !strings.Contains(string(licence), "SIL OPEN FONT LICENSE Version 1.1") {
		t.Fatal("the fonts' licence isn't served with them")
	}

	// Nothing a page loads, and nothing the policy allows, is on another site.
	for _, page := range []string{"/login", "/o/alpha/today", "/o/alpha/settings"} {
		_, body := call(t, client, "GET", srv.URL+page, "", nil)
		if !strings.Contains(body, "/assets/css/fonts.css?v=") {
			t.Errorf("%s doesn't load the fonts", page)
		}
		for _, outside := range regexp.MustCompile(`(?:href|src)="(https?:)?//[^"]+"`).FindAllString(body, -1) {
			if !strings.Contains(outside, "linkedin.com") {
				t.Errorf("%s loads from another site: %s", page, outside)
			}
		}
	}
	if strings.Contains(contentPolicy, "http") {
		t.Fatalf("the policy allows another site: %s", contentPolicy)
	}
}

func TestKnownBrowserIsntHeldUpByGuessing(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	for _, email := range []string{"ada@example.com", "bob@example.com"} {
		if _, err := s.CreateUser(email, "Someone", "correct horse battery"); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	app := &App{s: s}
	app.logins.now = func() time.Time { return now }
	srv := httptest.NewServer(app.Routes())
	t.Cleanup(srv.Close)
	try := func(email, password, device string) (int, *http.Cookie) {
		t.Helper()
		req, _ := http.NewRequest("POST", srv.URL+"/api/v1/session", bytes.NewReader(must(json.Marshal(map[string]string{"email": email, "password": password}))))
		req.Header.Set("content-type", "application/json")
		if device != "" {
			req.AddCookie(&http.Cookie{Name: deviceCookie, Value: device})
		}
		res, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		for _, c := range res.Cookies() {
			if c.Name == deviceCookie {
				return res.StatusCode, c
			}
		}
		return res.StatusCode, nil
	}
	const right, wrong = "correct horse battery", "a wrong guess"

	code, issued := try("ada@example.com", right, "")
	// For every path, so the sign-in form and the API both count by it.
	if code != http.StatusOK || issued == nil || !issued.HttpOnly || issued.Path != "/" || strings.Contains(issued.Value, "example") {
		t.Fatalf("first sign-in: %d, device cookie %+v", code, issued)
	}
	_, bobs := try("bob@example.com", right, "")
	if code, again := try("ada@example.com", right, issued.Value); code != http.StatusOK || again != nil {
		t.Fatalf("a known browser signing in again: %d, reissued %v", code, again != nil)
	}

	// Someone guesses at Ada's address until it is being held.
	for range freeAttempts + 3 {
		try("ada@example.com", wrong, "")
	}
	if code, _ := try("ada@example.com", right, ""); code != http.StatusTooManyRequests {
		t.Fatalf("an unknown browser while the address is held: %d", code)
	}
	for name, device := range map[string]string{
		"made up":             "bm90IGEgcmVhbCBub25jZQ." + strings.Repeat("0", 64),
		"issued to Bob":       bobs.Value,
		"Ada's with a new id": "x" + issued.Value,
		"no separator":        strings.ReplaceAll(issued.Value, ".", ""),
	} {
		if code, _ := try("ada@example.com", right, device); code != http.StatusTooManyRequests {
			t.Errorf("a device cookie that is %s got past the hold: %d", name, code)
		}
	}
	if code, _ := try("ada@example.com", right, issued.Value); code != http.StatusOK {
		t.Fatalf("Ada's own browser while her address is held: %d", code)
	}
	if code, _ := try("ada@example.com", right, ""); code != http.StatusTooManyRequests {
		t.Fatal("her signing in lifted the hold for everyone else")
	}

	// Her browser's own wrong guesses are still slowed.
	for i := range freeAttempts {
		if code, _ := try("ada@example.com", wrong, issued.Value); code != http.StatusUnauthorized {
			t.Fatalf("wrong password %d from a known browser: %d", i+1, code)
		}
	}
	if code, _ := try("ada@example.com", right, issued.Value); code != http.StatusTooManyRequests {
		t.Fatalf("a known browser after %d wrong passwords: %d", freeAttempts, code)
	}

	// However the address is typed, and whatever else is typed with it, a guess
	// lands in the one count for that address.
	now = now.Add(maxLoginWait + time.Second)
	for _, typed := range []string{" bob@example.com", "BOB@example.com ", "bob@example.com", "\tbob@example.com", "Bob@Example.com"} {
		if code, _ := try(typed, wrong, ""); code != http.StatusUnauthorized {
			t.Fatalf("a wrong password for %q: %d", typed, code)
		}
	}
	if code, _ := try("bob@example.com", right, ""); code != http.StatusTooManyRequests {
		t.Fatalf("spelling the address differently got fresh guesses: %d", code)
	}

	// Guessing at the sign-in form doesn't hold up a signed-in person changing their
	// password, and the change leaves their browser known under the new one.
	bob, err := s.UserByEmail("bob@example.com")
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := s.Login("bob@example.com", right)
	if err != nil {
		t.Fatal(err)
	}
	change, _ := http.NewRequest("PUT", srv.URL+"/api/v1/password", strings.NewReader(`{"current":"correct horse battery","new":"bob's new password"}`))
	change.Header.Set("content-type", "application/json")
	change.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	changed, err := http.DefaultClient.Do(change)
	if err != nil {
		t.Fatal(err)
	}
	changed.Body.Close()
	var renewed *http.Cookie
	for _, c := range changed.Cookies() {
		if c.Name == deviceCookie {
			renewed = c
		}
	}
	if changed.StatusCode != http.StatusNoContent || renewed == nil || renewed.Path != "/" {
		t.Fatalf("changing a password while the address is held: %d, device cookie %+v", changed.StatusCode, renewed)
	}
	if _, ok := s.KnownDevice("bob@example.com", bobs.Value); ok {
		t.Fatal("the device cookie from before the change still verifies")
	}
	if code, _ := try("bob@example.com", "bob's new password", renewed.Value); code != http.StatusOK {
		t.Fatalf("signing in with the renewed device cookie while the address is held: %d", code)
	}
	if code, _ := try("bob@example.com\x00"+strings.Split(renewed.Value, ".")[0], wrong, ""); code != http.StatusUnauthorized {
		t.Fatalf("an address with a NUL in it: %d", code)
	}
	if got := app.logins.wait(changeBucket(bob)); got != 0 {
		t.Fatalf("sign-in guesses reached the password-change count: %v", got)
	}

	// Changing the password ends what the old one vouched for.
	now = now.Add(maxLoginWait + time.Second)
	if err := s.SetPassword("ada@example.com", "a different password"); err != nil {
		t.Fatal(err)
	}
	for range freeAttempts + 1 {
		try("ada@example.com", wrong, "")
	}
	if code, _ := try("ada@example.com", "a different password", issued.Value); code != http.StatusTooManyRequests {
		t.Fatalf("a device cookie from before a password change still counts: %d", code)
	}
	now = now.Add(maxLoginWait + time.Second)
	if code, fresh := try("ada@example.com", "a different password", issued.Value); code != http.StatusOK || fresh == nil {
		t.Fatalf("signing in after a password change: %d, new device cookie: %v", code, fresh != nil)
	}
}
