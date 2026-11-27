package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

const testSite = "https://prospector.example"

type sent struct {
	via MailServer
	Message
}

// outbox records what would have been sent, and can be told to fail.
type outbox struct {
	mu   sync.Mutex
	all  []sent
	fail error
}

func (o *outbox) send(_ context.Context, via MailServer, m Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.fail != nil {
		return o.fail
	}
	o.all = append(o.all, sent{via, m})
	return nil
}

func (o *outbox) take(t *testing.T, want int) []sent {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	got := o.all
	o.all = nil
	if len(got) != want {
		t.Fatalf("%d messages sent, want %d: %+v", len(got), want, got)
	}
	return got
}

var resetLink = regexp.MustCompile(regexp.QuoteMeta(testSite) + `(/reset/[A-Za-z0-9_-]+)`)

func linkIn(t *testing.T, m sent) string {
	t.Helper()
	found := resetLink.FindStringSubmatch(m.Body)
	if found == nil {
		t.Fatalf("no link to %s in %q", testSite, m.Body)
	}
	return found[1]
}

// askReset asks for a link to choose a new password, as the forgot page does.
func askReset(t *testing.T, srv *httptest.Server, email string) (int, string) {
	t.Helper()
	return call(t, http.DefaultClient, "POST", srv.URL+"/api/v1/password-resets", "application/json", map[string]string{"email": email})
}

// choose sets a password from an emailed link, as the page the link opens does.
func choose(t *testing.T, srv *httptest.Server, link, password string) (int, string) {
	t.Helper()
	return call(t, http.DefaultClient, "POST", srv.URL+"/api/v1/password-resets/"+strings.TrimPrefix(link, "/reset/"), "application/json", map[string]string{"password": password})
}

var mailTestKey = strings.Repeat("ab", 32)

// mailSite is signedIn with mail configured: Ada owns alpha and is signed in.
func mailSite(t *testing.T, configure func(*App)) (*Store, *httptest.Server, *http.Client, *outbox) {
	t.Helper()
	s := tempStore(t)
	s.keys = mustKeys(t, "1:"+mailTestKey)
	o := mustOrg(t, s, "alpha")
	u := must(s.CreateUser("ada@example.com", "Ada Lovelace", "correct horse battery"))
	if err := s.AddMember(o.ID, u.ID, "owner", u.Name); err != nil {
		t.Fatal(err)
	}
	box := &outbox{}
	a := &App{s: s, base: testSite, send: box.send, later: func(f func()) { f() },
		mail: &MailServer{Host: "smtp.site.example", Port: 587, From: "Prospector <site@prospector.example>"}}
	if configure != nil {
		configure(a)
	}
	srv := httptest.NewServer(a.Routes())
	t.Cleanup(srv.Close)
	client := newMailClient()
	if !signIn(t, client, srv, "ada@example.com", "correct horse battery") {
		t.Fatal("Ada can't sign in")
	}
	return s, srv, client, box
}

func newMailClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

func signIn(t *testing.T, c *http.Client, srv *httptest.Server, email, password string) bool {
	t.Helper()
	res, err := signInWith(c, srv.URL, email, password)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode == http.StatusOK
}

func TestMailIsOffUntilConfigured(t *testing.T) {
	t.Parallel()
	for name, configure := range map[string]func(*App){
		"no server":         func(a *App) { a.mail = nil },
		"no PROSPECTOR_URL": func(a *App) { a.base = "" },
	} {
		_, srv, client, box := mailSite(t, configure)
		if code, _ := call(t, http.DefaultClient, "GET", srv.URL+"/forgot", "", nil); code != http.StatusNotFound {
			t.Errorf("%s: GET /forgot: %d", name, code)
		}
		if code, _ := askReset(t, srv, "ada@example.com"); code != http.StatusNotFound {
			t.Errorf("%s: POST /forgot: %d", name, code)
		}
		if _, body := call(t, http.DefaultClient, "GET", srv.URL+"/api/v1/sign-in", "", nil); !strings.Contains(body, `"forgot":false`) {
			t.Errorf("%s: the sign-in page offers a reset it can't send: %s", name, body)
		}
		// A new member's first password is still shown, as before there was mail.
		code, body := call(t, client, "POST", srv.URL+"/o/alpha/api/v1/members", "application/json", map[string]string{"email": "grace@example.com", "name": "Grace"})
		if code != http.StatusCreated || !strings.Contains(body, "first password") {
			t.Errorf("%s: adding a member: %d %s", name, code, body)
		}
		box.take(t, 0)
	}
}

func TestPasswordReset(t *testing.T) {
	t.Parallel()
	s, srv, ada, box := mailSite(t, nil)
	if _, body := call(t, http.DefaultClient, "GET", srv.URL+"/api/v1/sign-in", "", nil); !strings.Contains(body, `"forgot":true`) {
		t.Fatal("the sign-in page doesn't offer a reset")
	}

	// A stranger learns nothing: the answer is the same, and nothing is sent.
	_, unknown := askReset(t, srv, "nobody@example.com")
	box.take(t, 0)

	// The link points at PROSPECTOR_URL whatever host the request named.
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/password-resets", strings.NewReader(`{"email": "Ada@Example.com"}`))
	req.Header.Set("content-type", "application/json")
	req.Host = "evil.example"
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != http.StatusAccepted {
		t.Fatalf("asking for a link: %v %v", res, err)
	}
	var known strings.Builder
	bufio.NewReader(res.Body).WriteTo(&known)
	res.Body.Close()
	if known.String() != unknown {
		t.Error("the answer differs between an address with an account and one without")
	}
	mail := box.take(t, 1)[0]
	if mail.To != "ada@example.com" || mail.via.Host != "smtp.site.example" || strings.Contains(mail.Body, "evil.example") {
		t.Fatalf("reset mail: %+v", mail)
	}
	link := linkIn(t, mail)

	// Asking again straight away sends nothing more.
	askReset(t, srv, "ada@example.com")
	box.take(t, 0)

	// The token is stored hashed.
	var stored PasswordToken
	if err := s.db.First(&stored).Error; err != nil || strings.Contains(link, stored.TokenHash) {
		t.Fatalf("stored token: %+v %v", stored, err)
	}

	// Opening the link, as a mail scanner does, doesn't spend it.
	for range 2 {
		if code, _ := call(t, http.DefaultClient, "GET", srv.URL+link, "", nil); code != http.StatusOK {
			t.Fatalf("GET %s: %d", link, code)
		}
		if code, body := call(t, http.DefaultClient, "GET", srv.URL+"/api/v1/password-resets/"+strings.TrimPrefix(link, "/reset/"), "", nil); code != http.StatusOK || !strings.Contains(body, "ada@example.com") {
			t.Fatalf("whose link %s is: %d %s", link, code, body)
		}
	}
	if code, body := choose(t, srv, link, "short"); code != http.StatusBadRequest || !strings.Contains(body, "10 characters") {
		t.Fatalf("a short password: %d", code)
	}
	if code, _ := choose(t, srv, link+"x", "a brand new password"); code != http.StatusGone {
		t.Fatalf("a made-up link: %d", code)
	}

	if code, body := choose(t, srv, link, "a brand new password"); code != http.StatusNoContent {
		t.Fatalf("choosing a password: %d %s", code, body)
	}

	if code, _ := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/me", "", nil); code != http.StatusUnauthorized {
		t.Error("the session from before the reset still works")
	}
	if signIn(t, newMailClient(), srv, "ada@example.com", "correct horse battery") {
		t.Error("the old password still works")
	}
	if !signIn(t, newMailClient(), srv, "ada@example.com", "a brand new password") {
		t.Error("the new password doesn't work")
	}
	if code, _ := choose(t, srv, link, "yet another password"); code != http.StatusGone {
		t.Errorf("the link worked twice: %d", code)
	}
	// A browser is told what happened to the link, on a page like the rest.
	req, _ = http.NewRequest("GET", srv.URL+link, nil)
	req.Header.Set("Accept", "text/html")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var page strings.Builder
	bufio.NewReader(res.Body).WriteTo(&page)
	res.Body.Close()
	if res.StatusCode != http.StatusGone || !strings.Contains(page.String(), "This link has expired or has been used") || !strings.Contains(page.String(), "<title>") {
		t.Errorf("a spent link in a browser: %d %s", res.StatusCode, page.String())
	}
}

func TestResetLinksEnd(t *testing.T) {
	t.Parallel()
	s, srv, _, _ := mailSite(t, nil)
	u := must(s.UserByEmail("ada@example.com"))

	expired := must(s.ResetToken(u.ID))
	if err := s.db.Model(&PasswordToken{}).Where("user_id = ?", u.ID).Update("expires_at", "2001-01-01").Error; err != nil {
		t.Fatal(err)
	}
	if code, _ := call(t, http.DefaultClient, "GET", srv.URL+"/reset/"+expired, "", nil); code != http.StatusGone {
		t.Errorf("an expired link: %d", code)
	}

	// Changing the password any other way ends a link that is still out.
	live := must(s.InviteToken(u.ID))
	if err := s.SetPassword("ada@example.com", "set from the command line"); err != nil {
		t.Fatal(err)
	}
	if err := s.RedeemPasswordToken(live, "whatever comes next"); !errors.Is(err, errBadLink) {
		t.Errorf("a link from before the password changed: %v", err)
	}
}

func TestSignedOutFormsRefuseOtherSites(t *testing.T) {
	t.Parallel()
	s, srv, _, box := mailSite(t, nil)
	token := must(s.InviteToken(must(s.UserByEmail("ada@example.com")).ID))
	for _, path := range []string{"/api/v1/password-resets", "/api/v1/password-resets/" + token} {
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(`{"email": "ada@example.com", "password": "chosen by another site"}`))
		req.Header.Set("content-type", "application/json")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.Header.Set("Origin", "https://evil.example")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Errorf("POST %s from another site: %d", path, res.StatusCode)
		}
	}
	box.take(t, 0)
	if !signIn(t, newMailClient(), srv, "ada@example.com", "correct horse battery") {
		t.Error("another site changed the password")
	}

	// The policy blocks inline scripts and handlers on these pages as on the rest.
	inline := regexp.MustCompile(`(?i)<script(?:\s[^>]*)?>|<[^>]*\son[a-z]+\s*=`)
	for _, page := range []string{"/forgot", "/reset/" + token, "/login?reset=1"} {
		code, body := call(t, http.DefaultClient, "GET", srv.URL+page, "", nil)
		if code != http.StatusOK {
			t.Fatalf("GET %s: %d", page, code)
		}
		for _, tag := range inline.FindAllString(body, -1) {
			if !strings.Contains(tag, "src=") {
				t.Errorf("%s has inline script: %s", page, tag)
			}
		}
	}
}

func TestInvitationByEmail(t *testing.T) {
	t.Parallel()
	s, srv, ada, box := mailSite(t, nil)
	code, body := call(t, ada, "POST", srv.URL+"/o/alpha/api/v1/members", "application/json", map[string]string{"email": "grace@example.com", "name": "Grace Hopper"})
	if code != http.StatusCreated || strings.Contains(body, "first password") || !strings.Contains(body, "emailed a link") {
		t.Fatalf("adding a member: %d %s", code, body)
	}
	mail := box.take(t, 1)[0]
	if mail.To != "grace@example.com" || !strings.Contains(mail.Subject, "Ada Lovelace added you to Alpha") {
		t.Fatalf("invitation: %+v", mail)
	}
	if code, _ := choose(t, srv, linkIn(t, mail), "grace's own password"); code != http.StatusNoContent {
		t.Fatalf("choosing a password from the invitation: %d", code)
	}
	grace := newMailClient()
	if !signIn(t, grace, srv, "grace@example.com", "grace's own password") {
		t.Fatal("Grace can't sign in with the password she chose")
	}
	if code, body := call(t, grace, "GET", srv.URL+"/o/alpha/api/v1/me", "", nil); code != http.StatusOK {
		t.Errorf("Grace is asked to change a password nobody else has seen: %d %s", code, body)
	}
	if res, _ := get(t, grace, srv.URL+"/o/alpha/today"); res.Request.URL.Path != "/o/alpha/today" {
		t.Error("Grace is asked to change a password nobody else has seen")
	}
	if u := must(s.UserByEmail("grace@example.com")); u.MustChangePassword {
		t.Error("the account is still marked as on a first password")
	}

	// Someone added again before choosing a password is sent the link again.
	if code, _ := call(t, ada, "POST", srv.URL+"/o/alpha/api/v1/members", "application/json", map[string]string{"email": "alan@example.com", "name": "Alan"}); code != http.StatusCreated {
		t.Fatal("adding Alan")
	}
	first := linkIn(t, box.take(t, 1)[0])
	alan := must(s.UserByEmail("alan@example.com"))
	// Straight away nothing more is sent, so adding someone repeatedly can't fill their inbox.
	code, body = call(t, ada, "POST", srv.URL+"/o/alpha/api/v1/members", "application/json", map[string]string{"email": "alan@example.com"})
	if code != http.StatusCreated || !strings.Contains(body, "a moment ago") || strings.Contains(body, "first password") {
		t.Fatalf("adding Alan again at once: %d %s", code, body)
	}
	box.take(t, 0)
	if err := s.db.Model(&PasswordToken{}).Where("user_id = ?", alan.ID).Update("created_at", time.Now().Add(-2*resetGap)).Error; err != nil {
		t.Fatal(err)
	}
	code, body = call(t, ada, "POST", srv.URL+"/o/alpha/api/v1/members", "application/json", map[string]string{"email": "alan@example.com"})
	if code != http.StatusCreated || !strings.Contains(body, "emailed a link") || strings.Contains(body, "first password") {
		t.Fatalf("adding Alan again: %d %s", code, body)
	}
	if again := linkIn(t, box.take(t, 1)[0]); again == first {
		t.Error("the second invitation reused the first link")
	}
	if code, _ := call(t, http.DefaultClient, "GET", srv.URL+first, "", nil); code != http.StatusGone {
		t.Errorf("the first invitation still works after the second: %d", code)
	}

	// Arriving through a provider ends the invitation with the first password.
	if err := s.db.Transaction(func(tx *gorm.DB) error { return claim(tx, alan) }); err != nil {
		t.Fatal(err)
	}
	var left int64
	if s.db.Model(&PasswordToken{}).Where("user_id = ?", alan.ID).Count(&left); left != 0 {
		t.Error("an invitation outlived the account's sign-in through a provider")
	}

	// Someone who already has an account is added without mail.
	other := mustOrg(t, s, "beta")
	if err := s.AddMember(other.ID, must(s.UserByEmail("ada@example.com")).ID, "owner", "Ada"); err != nil {
		t.Fatal(err)
	}
	if code, _ := call(t, ada, "POST", srv.URL+"/o/beta/api/v1/members", "application/json", map[string]string{"email": "grace@example.com"}); code != http.StatusCreated {
		t.Fatalf("adding an existing account: %d", code)
	}
	box.take(t, 0)

	// When the mail can't be sent the owner is told why and given the password instead.
	box.fail = errors.New("smtp.site.example refused the username and password")
	code, body = call(t, ada, "POST", srv.URL+"/o/alpha/api/v1/members", "application/json", map[string]string{"email": "edsger@example.com", "name": "Edsger"})
	if code != http.StatusCreated || !strings.Contains(body, "refused the username") || !strings.Contains(body, "first password") {
		t.Fatalf("adding a member when mail fails: %d %s", code, body)
	}
}

func TestOrganisationMailServer(t *testing.T) {
	t.Parallel()
	s, srv, ada, box := mailSite(t, nil)
	o := must(s.OrgBySlug("alpha"))
	save := func(c *http.Client, in map[string]any) (int, string) {
		t.Helper()
		return call(t, c, "PUT", srv.URL+"/o/alpha/api/v1/mail-server", "application/json", in)
	}
	server := map[string]any{"host": "smtp.alpha.example", "port": 587, "username": "alpha-user", "password": "s3cret-smtp-pass", "sender": "Alpha <hello@alpha.example>"}

	// Mistakes are refused by name: a database port, an address that isn't one.
	for name, wrong := range map[string]map[string]any{
		"a port mail doesn't use": {"host": "10.0.0.8", "port": 5432, "sender": "a@alpha.example"},
		"this machine":            {"host": "127.0.0.1", "port": 4222, "sender": "a@alpha.example"},
		"this machine, by name":   {"host": "localhost", "port": 587, "sender": "a@alpha.example"},
		"no From address":         {"host": "smtp.alpha.example", "port": 587, "sender": "Alpha"},
		"a From with a header":    {"host": "smtp.alpha.example", "port": 587, "sender": "a@alpha.example\r\nBcc: b@evil.example"},
	} {
		if code, body := save(ada, wrong); code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, code, body)
		}
	}
	if code, body := save(ada, server); code != http.StatusOK {
		t.Fatalf("saving: %d %s", code, body)
	}
	stored := must(s.OrgMail(o.ID))
	if strings.Contains(stored.Password, "s3cret") || !sealed(stored.Password) {
		t.Fatalf("the password is stored unsealed: %q", stored.Password)
	}
	_, page := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/mail-server", "", nil)
	if strings.Contains(page, "s3cret") || strings.Contains(page, stored.Password) || !strings.Contains(page, "smtp.alpha.example") {
		t.Fatalf("the mail server Settings reads gives the password away, or doesn't name the server: %s", page)
	}

	// Invitations to this organisation now leave through its server, as its address.
	if code, body := call(t, ada, "POST", srv.URL+"/o/alpha/api/v1/members", "application/json", map[string]string{"email": "grace@example.com", "name": "Grace"}); code != http.StatusCreated {
		t.Fatalf("adding a member: %d %s", code, body)
	}
	via := box.take(t, 1)[0].via
	if via.Host != "smtp.alpha.example" || via.Password != "s3cret-smtp-pass" || via.From != "Alpha <hello@alpha.example>" {
		t.Fatalf("sent through %+v", via)
	}
	// A reset is the account's, not an organisation's, so it stays with the site.
	askReset(t, srv, "ada@example.com")
	if via := box.take(t, 1)[0].via; via.Host != "smtp.site.example" {
		t.Fatalf("a reset was sent through %+v", via)
	}

	if code, body := call(t, ada, "POST", srv.URL+"/o/alpha/api/v1/mail-server/test", "application/json", map[string]any{}); code != http.StatusOK || !strings.Contains(body, "ada@example.com") {
		t.Fatalf("test message: %d %s", code, body)
	}
	if got := box.take(t, 1)[0]; got.To != "ada@example.com" || got.via.Host != "smtp.alpha.example" {
		t.Fatalf("test message: %+v", got)
	}

	// An empty password keeps the saved one; a different server doesn't inherit it.
	server["password"], server["sender"] = "", "Alpha Team <team@alpha.example>"
	if code, _ := save(ada, server); code != http.StatusOK || must(s.OrgMail(o.ID)).Password != stored.Password {
		t.Fatal("saving with an empty password lost the saved one")
	}
	server["host"] = "smtp.elsewhere.example"
	if code, _ := save(ada, server); code != http.StatusOK || must(s.OrgMail(o.ID)).Password != "" {
		t.Fatal("the saved password followed a change of server")
	}

	// Only an owner sees or changes any of it.
	grace := must(s.UserByEmail("grace@example.com"))
	if err := s.SetPassword(grace.Email, "grace's own password"); err != nil {
		t.Fatal(err)
	}
	member := newMailClient()
	signIn(t, member, srv, grace.Email, "grace's own password")
	if code, _ := save(member, server); code != http.StatusForbidden {
		t.Errorf("a member saving the mail server: %d", code)
	}
	if code, body := call(t, member, "GET", srv.URL+"/o/alpha/api/v1/mail-server", "", nil); code != http.StatusForbidden || strings.Contains(body, "smtp.elsewhere.example") {
		t.Errorf("a member is shown the mail server (%d)", code)
	}

	if code, _ := save(ada, map[string]any{"host": ""}); code != http.StatusOK {
		t.Fatal("clearing the server")
	}
	if _, err := s.OrgMail(o.ID); err == nil {
		t.Error("the cleared server is still stored")
	}
}

func TestMailPasswordNeedsAKey(t *testing.T) {
	t.Parallel()
	s, srv, ada, _ := mailSite(t, func(a *App) { a.s.keys = nil })
	code, body := call(t, ada, "PUT", srv.URL+"/o/alpha/api/v1/mail-server", "application/json",
		map[string]any{"host": "smtp.alpha.example", "port": 587, "username": "u", "password": "s3cret-smtp-pass", "sender": "a@alpha.example"})
	if code != http.StatusBadRequest || !strings.Contains(body, "no secret key") {
		t.Fatalf("saving a password with nothing to seal it: %d %s", code, body)
	}
	if _, err := s.OrgMail(must(s.OrgBySlug("alpha")).ID); err == nil {
		t.Error("it was stored anyway")
	}
}

// Whoever runs an organisation's mail server reads what it carries, so a link to
// choose the password of an account that was already here never goes through it.
func TestAnotherOrganisationsMailServerNeverCarriesAnExistingAccountsLink(t *testing.T) {
	t.Parallel()
	s, srv, ada, box := mailSite(t, nil)
	if code, _ := call(t, ada, "POST", srv.URL+"/o/alpha/api/v1/members", "application/json", map[string]string{"email": "grace@example.com", "name": "Grace Hopper"}); code != http.StatusCreated {
		t.Fatal("adding Grace")
	}
	box.take(t, 1)
	grace := must(s.UserByEmail("grace@example.com"))
	if err := s.db.Model(&PasswordToken{}).Where("user_id = ?", grace.ID).Update("created_at", time.Now().Add(-2*resetGap)).Error; err != nil {
		t.Fatal(err)
	}

	mallory := must(s.CreateUser("mallory@example.com", "Mallory", "mallory's password"))
	beta := mustOrg(t, s, "beta")
	if err := s.AddMember(beta.ID, mallory.ID, "owner", mallory.Name); err != nil {
		t.Fatal(err)
	}
	theirs := newMailClient()
	signIn(t, theirs, srv, "mallory@example.com", "mallory's password")
	if code, body := call(t, theirs, "PUT", srv.URL+"/o/beta/api/v1/mail-server", "application/json",
		map[string]any{"host": "smtp.mallory.example", "port": 587, "sender": "m@mallory.example"}); code != http.StatusOK {
		t.Fatalf("saving beta's mail server: %d %s", code, body)
	}
	if code, body := call(t, theirs, "POST", srv.URL+"/o/beta/api/v1/members", "application/json", map[string]string{"email": "grace@example.com"}); code != http.StatusCreated {
		t.Fatalf("adding Grace to beta: %d %s", code, body)
	}
	for _, m := range box.take(t, 1) {
		if m.via.Host != "smtp.site.example" || m.To != "grace@example.com" {
			t.Fatalf("a link to an existing account's password went through %s to %s", m.via.Host, m.To)
		}
	}

	// Someone the request itself creates is still invited through the organisation's server.
	if code, _ := call(t, theirs, "POST", srv.URL+"/o/beta/api/v1/members", "application/json", map[string]string{"email": "new@example.com", "name": "New"}); code != http.StatusCreated {
		t.Fatal("adding someone new to beta")
	}
	if m := box.take(t, 1)[0]; m.via.Host != "smtp.mallory.example" {
		t.Fatalf("a new member's invitation went through %s", m.via.Host)
	}
}

func TestAResetGivesAProviderOnlyAccountAPassword(t *testing.T) {
	t.Parallel()
	s, srv, _, box := mailSite(t, nil)
	dan := must(s.CreateUser("dan@example.com", "Dan Okafor", "a first password"))
	if err := s.db.Model(&User{}).Where("id = ?", dan.ID).Update("password_hash", unusableHash()).Error; err != nil {
		t.Fatal(err)
	}
	askReset(t, srv, "dan@example.com")
	link := linkIn(t, box.take(t, 1)[0])
	if code, _ := choose(t, srv, link, "dan's chosen password"); code != http.StatusNoContent {
		t.Fatalf("choosing a password: %d", code)
	}
	if u := must(s.UserByEmail("dan@example.com")); !u.HasPassword() {
		t.Fatal("the account still has no password")
	}
	if !signIn(t, newMailClient(), srv, "dan@example.com", "dan's chosen password") {
		t.Error("the chosen password doesn't sign in")
	}
}

func TestAMailPasswordFollowsAKeyChange(t *testing.T) {
	t.Parallel()
	s, srv, ada, _ := mailSite(t, nil)
	code, body := call(t, ada, "PUT", srv.URL+"/o/alpha/api/v1/mail-server", "application/json",
		map[string]any{"host": "smtp.alpha.example", "port": 587, "username": "u", "password": "s3cret-smtp-pass", "sender": "a@alpha.example"})
	if code != http.StatusOK {
		t.Fatalf("saving the mail server: %d %s", code, body)
	}
	o := must(s.OrgBySlug("alpha"))
	under1 := must(s.OrgMail(o.ID)).Password

	newKey := strings.Repeat("cd", 32)
	s.keys = mustKeys(t, "2:"+newKey+",1:"+mailTestKey)
	if moved, err := s.Reseal(); err != nil || moved != 1 {
		t.Fatalf("resealing: %d %v", moved, err)
	}
	under2 := must(s.OrgMail(o.ID)).Password
	if under2 == under1 || sealedVersion(under2) != 2 {
		t.Fatal("the mail password wasn't moved to the new key")
	}
	// The old key retired, the password still opens.
	s.keys = mustKeys(t, "2:"+newKey)
	if via, err := (&App{s: s}).mailFor(o.ID); err != nil || via.Password != "s3cret-smtp-pass" {
		t.Fatalf("after the old key is removed: %+v %v", via, err)
	}
	s.keys = nil
	if _, err := (&App{s: s}).mailFor(o.ID); !errors.Is(err, errNoKeys) {
		t.Fatalf("a server with no key read a sealed mail password: %v", err)
	}
}

func TestMailFromEnv(t *testing.T) {
	t.Parallel()
	env := func(pairs ...string) func(string) string {
		return func(k string) string {
			for i := 0; i < len(pairs); i += 2 {
				if pairs[i] == k {
					return pairs[i+1]
				}
			}
			return ""
		}
	}
	if m, err := mailFromEnv(env()); m != nil || err != nil {
		t.Errorf("nothing set: %+v %v", m, err)
	}
	m, err := mailFromEnv(env("SMTP_ADDR", "smtp.example.com:587", "SMTP_USERNAME", "u", "SMTP_PASSWORD", "p", "MAIL_FROM", "Prospector <p@example.com>"))
	if err != nil || *m != (MailServer{"smtp.example.com", 587, "u", "p", "Prospector <p@example.com>"}) {
		t.Errorf("a full set: %+v %v", m, err)
	}
	for name, pairs := range map[string][]string{
		"an address without a sender": {"SMTP_ADDR", "smtp.example.com:587"},
		"a sender without an address": {"MAIL_FROM", "p@example.com"},
		"no port":                     {"SMTP_ADDR", "smtp.example.com", "MAIL_FROM", "p@example.com"},
		"a port mail doesn't use":     {"SMTP_ADDR", "db.example.com:5432", "MAIL_FROM", "p@example.com"},
	} {
		if _, err := mailFromEnv(env(pairs...)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// fakeSMTP speaks enough of the protocol to take one message at a time.
type fakeSMTP struct {
	addr string
	// tls, when set, is offered as STARTTLS.
	tls   *tls.Config
	mu    sync.Mutex
	from  string
	to    []string
	auth  string
	data  string
	verbs []string
	// secured is whether the message arrived after STARTTLS.
	secured bool
}

func newFakeSMTP(t *testing.T, secure *tls.Config) *fakeSMTP {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	f := &fakeSMTP{addr: l.Addr().String(), tls: secure}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

func (f *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	say := func(s string) { conn.Write([]byte(s + "\r\n")) }
	secured := false
	say("220 fake ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		f.mu.Lock()
		f.verbs = append(f.verbs, verb)
		switch verb {
		case "EHLO":
			if f.tls != nil && !secured {
				say("250-fake\r\n250 STARTTLS")
			} else {
				say("250-fake\r\n250 AUTH PLAIN")
			}
		case "STARTTLS":
			say("220 go ahead")
			conn = tls.Server(conn, f.tls)
			r, secured = bufio.NewReader(conn), true
		case "AUTH":
			f.auth = line
			say("235 ok")
		case "MAIL":
			f.from = line
			say("250 ok")
		case "RCPT":
			f.to = append(f.to, line)
			say("250 ok")
		case "DATA":
			say("354 go on")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil || l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			f.data, f.secured = b.String(), secured
			say("250 queued")
		case "QUIT":
			say("221 bye")
			f.mu.Unlock()
			return
		default:
			say("502 what")
		}
		f.mu.Unlock()
	}
}

func TestSMTPSend(t *testing.T) {
	t.Parallel()
	f := newFakeSMTP(t, nil)
	host, port, _ := net.SplitHostPort(f.addr)
	n, _ := strconv.Atoi(port)
	via := MailServer{Host: host, Port: n, Username: "user", Password: "pass", From: "Prospector <site@prospector.example>"}
	err := smtpSend(context.Background(), via, Message{To: "Grace Hopper <grace@example.com>",
		Subject: "Añadido\r\nBcc: mallory@evil.example", Body: "Choose your password:\nhttps://prospector.example/reset/abc=def\n"})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.from != "MAIL FROM:<site@prospector.example>" || len(f.to) != 1 || f.to[0] != "RCPT TO:<grace@example.com>" || f.auth == "" {
		t.Errorf("envelope: %q %q, auth %q", f.from, f.to, f.auth)
	}
	head, body, _ := strings.Cut(f.data, "\r\n\r\n")
	if strings.Contains(head, "\r\nBcc:") || !strings.Contains(head, "Subject: =?utf-8?q?") {
		t.Errorf("a line break in the subject made a header:\n%s", head)
	}
	for _, want := range []string{`From: "Prospector" <site@prospector.example>`, `To: "Grace Hopper" <grace@example.com>`, "Date: ", "Message-ID: <", "charset=utf-8"} {
		if !strings.Contains(head, want) {
			t.Errorf("headers lack %q:\n%s", want, head)
		}
	}
	if !strings.Contains(body, "reset/abc=3Ddef") {
		t.Errorf("body: %q", body)
	}

	if err := smtpSend(context.Background(), via, Message{To: "grace@example.com\r\nBcc: mallory@evil.example", Subject: "x"}); err == nil {
		t.Error("a recipient spanning lines was accepted")
	}
	// Anywhere but this machine, a server that offers no TLS is sent nothing.
	if err := (MailServer{Host: "db.internal.example", Port: 5432, From: "a@example.com"}).check(); err == nil {
		t.Error("a port mail doesn't use was accepted for another machine")
	}
}

// A real provider is never on this machine, so these are the paths it takes.
func TestSMTPNeedsTLS(t *testing.T) {
	t.Parallel()
	from, to := &mail.Address{Address: "site@prospector.example"}, &mail.Address{Address: "grace@example.com"}
	via := MailServer{Host: "example.com", Port: 587, Username: "user", Password: "pass", From: from.Address}
	dial := func(f *fakeSMTP) net.Conn {
		t.Helper()
		conn, err := net.Dial("tcp", f.addr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn
	}
	said := func(f *fakeSMTP) string {
		f.mu.Lock()
		defer f.mu.Unlock()
		return strings.Join(f.verbs, " ")
	}

	// A server that offers no TLS is told nothing: no password, no message.
	plain := newFakeSMTP(t, nil)
	err := submit(dial(plain), via, from, to, Message{Subject: "x", Body: "y"}, &tls.Config{ServerName: via.Host}, true)
	if err == nil || !strings.Contains(err.Error(), "doesn't offer TLS") {
		t.Fatalf("sending without TLS: %v", err)
	}
	if got := said(plain); strings.Contains(got, "AUTH") || strings.Contains(got, "MAIL") {
		t.Errorf("the server without TLS was sent: %s", got)
	}

	// httptest's certificate names example.com, and its client trusts it.
	web := httptest.NewTLSServer(nil)
	t.Cleanup(web.Close)
	roots := web.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	offer := &tls.Config{Certificates: web.TLS.Certificates}

	secure := newFakeSMTP(t, offer)
	if err := submit(dial(secure), via, from, to, Message{Subject: "x", Body: "y"}, &tls.Config{ServerName: via.Host, RootCAs: roots}, true); err != nil {
		t.Fatalf("sending over STARTTLS: %v", err)
	}
	secure.mu.Lock()
	if !secure.secured || secure.auth == "" || !strings.Contains(secure.data, "Subject: x") {
		t.Errorf("over STARTTLS: secured %v, auth %q, data %q", secure.secured, secure.auth, secure.data)
	}
	secure.mu.Unlock()

	// A certificate nobody vouches for, or for another name, ends it before the password.
	for name, trust := range map[string]*tls.Config{
		"an untrusted certificate": {ServerName: via.Host},
		"another server's name":    {ServerName: "smtp.other.example", RootCAs: roots},
	} {
		f := newFakeSMTP(t, offer)
		if err := submit(dial(f), via, from, to, Message{Subject: "x", Body: "y"}, trust, true); err == nil {
			t.Errorf("%s was accepted", name)
		}
		if got := said(f); strings.Contains(got, "AUTH") || strings.Contains(got, "MAIL") {
			t.Errorf("%s: the server was sent: %s", name, got)
		}
	}

	// A relay on this machine usually offers TLS under a certificate of its own making.
	local, relay := via, newFakeSMTP(t, offer)
	local.Username = ""
	if err := submit(dial(relay), local, from, to, Message{Subject: "x", Body: "y"}, &tls.Config{ServerName: via.Host}, false); err != nil {
		t.Fatalf("a local relay with a self-signed certificate: %v", err)
	}
	if got := said(relay); strings.Contains(got, "STARTTLS") || !strings.Contains(got, "DATA") {
		t.Errorf("a local relay was sent: %s", got)
	}
}
