package main

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/mailer"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
)

const testSite = "https://prospector.example"

type sent struct {
	via mailer.Server
	mailer.Message
}

// outbox records what would have been sent, and can be told to fail.
type outbox struct {
	mu   sync.Mutex
	all  []sent
	fail error
}

func (o *outbox) send(_ context.Context, via mailer.Server, m mailer.Message) error {
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
	s.Keyring = mustKeys(t, "1:"+mailTestKey)
	o := mustOrg(t, s, "alpha")
	u := must(s.CreateUser("ada@example.com", "Ada Lovelace", "correct horse battery"))
	if err := s.AddMember(o.ID, u.ID, "owner", u.Name); err != nil {
		t.Fatal(err)
	}
	box := &outbox{}
	a := &App{s: s, base: testSite, send: box.send, later: func(f func()) { f() },
		mail: &mailer.Server{Host: "smtp.site.example", Port: 587, From: "Prospector <site@prospector.example>"}}
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

// The one who asks for a link is most likely the one who reads it.
func TestAResetIsMailedInTheLanguageItWasAskedIn(t *testing.T) {
	t.Parallel()
	_, srv, _, box := mailSite(t, nil)
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/password-resets", strings.NewReader(`{"email": "ada@example.com"}`))
	req.Header.Set("content-type", "application/json")
	req.AddCookie(&http.Cookie{Name: request.LangCookie, Value: "en-XA"})
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != http.StatusAccepted {
		t.Fatalf("asking for a link: %v %v", res, err)
	}
	res.Body.Close()
	mail := box.take(t, 1)[0]
	if want := say.New("Reset your Prospector password").In("en-XA"); mail.Subject != want || !strings.HasPrefix(mail.Body, "[Šöɱéöñé") {
		t.Errorf("not in the language asked in: %q %q", mail.Subject, mail.Body)
	}
	linkIn(t, mail)
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
	if err := s.DB.First(&stored).Error; err != nil || strings.Contains(link, stored.TokenHash) {
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
	if err := s.DB.Model(&PasswordToken{}).Where("user_id = ?", u.ID).Update("expires_at", "2001-01-01").Error; err != nil {
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
	if err := s.RedeemPasswordToken(live, "whatever comes next"); !errors.Is(err, identity.ErrBadLink) {
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
	if err := s.DB.Model(&PasswordToken{}).Where("user_id = ?", alan.ID).Update("created_at", time.Now().Add(-2*identity.ResetGap)).Error; err != nil {
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
	if err := s.DB.Transaction(func(tx *gorm.DB) error { return identity.Claim(tx, alan) }); err != nil {
		t.Fatal(err)
	}
	var left int64
	if s.DB.Model(&PasswordToken{}).Where("user_id = ?", alan.ID).Count(&left); left != 0 {
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
	s, srv, ada, _ := mailSite(t, func(a *App) { a.s.Keyring = nil })
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
	if err := s.DB.Model(&PasswordToken{}).Where("user_id = ?", grace.ID).Update("created_at", time.Now().Add(-2*identity.ResetGap)).Error; err != nil {
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
	if err := s.DB.Model(&User{}).Where("id = ?", dan.ID).Update("password_hash", unusableHash()).Error; err != nil {
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
	s.Keyring = mustKeys(t, "2:"+newKey+",1:"+mailTestKey)
	if moved, err := s.Reseal(); err != nil || moved != 1 {
		t.Fatalf("resealing: %d %v", moved, err)
	}
	under2 := must(s.OrgMail(o.ID)).Password
	if under2 == under1 || sealedVersion(under2) != 2 {
		t.Fatal("the mail password wasn't moved to the new key")
	}
	// The old key retired, the password still opens.
	s.Keyring = mustKeys(t, "2:"+newKey)
	if via, err := (&App{s: s}).MailFor(o.ID); err != nil || via.Password != "s3cret-smtp-pass" {
		t.Fatalf("after the old key is removed: %+v %v", via, err)
	}
	s.Keyring = nil
	if _, err := (&App{s: s}).MailFor(o.ID); !errors.Is(err, errNoKeys) {
		t.Fatalf("a server with no key read a sealed mail password: %v", err)
	}
}
