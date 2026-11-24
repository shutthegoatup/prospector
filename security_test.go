package main

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
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
