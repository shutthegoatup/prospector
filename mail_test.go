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
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
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

// mailSite is signedIn with mail configured: Ada owns alpha and is signed in.
func mailSite(t *testing.T, configure func(*App)) (*Store, *httptest.Server, *http.Client, *outbox) {
	t.Helper()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	u := must(s.CreateUser("ada@example.com", "Ada Lovelace", "correct horse battery"))
	if err := s.AddMember(o.ID, u.ID, "owner", u.Name); err != nil {
		t.Fatal(err)
	}
	box := &outbox{}
	a := &App{s: s, base: testSite, send: box.send, later: func(f func()) { f() },
		mail: &MailServer{Host: "smtp.site.example", Port: 587, From: "Prospector <site@prospector.example>"}}
	a.seal = must(newSealer(strings.Repeat("ab", 32)))
	if configure != nil {
		configure(a)
	}
	srv := httptest.NewServer(a.Routes())
	t.Cleanup(srv.Close)
	client := newBrowser()
	if !signIn(t, client, srv, "ada@example.com", "correct horse battery") {
		t.Fatal("Ada can't sign in")
	}
	return s, srv, client, box
}

func newBrowser() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

func signIn(t *testing.T, c *http.Client, srv *httptest.Server, email, password string) bool {
	t.Helper()
	res, err := c.PostForm(srv.URL+"/login", url.Values{"email": {email}, "password": {password}})
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode == http.StatusOK
}

func postForm(t *testing.T, c *http.Client, u string, form url.Values) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", u, strings.NewReader(form.Encode()))
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var b strings.Builder
	bufio.NewReader(res.Body).WriteTo(&b)
	return res.StatusCode, b.String()
}

func TestMailIsOffUntilConfigured(t *testing.T) {
	for name, configure := range map[string]func(*App){
		"no server":         func(a *App) { a.mail = nil },
		"no PROSPECTOR_URL": func(a *App) { a.base = "" },
	} {
		_, srv, client, box := mailSite(t, configure)
		if code, _ := call(t, http.DefaultClient, "GET", srv.URL+"/forgot", "", nil); code != http.StatusNotFound {
			t.Errorf("%s: GET /forgot: %d", name, code)
		}
		if code, _ := postForm(t, http.DefaultClient, srv.URL+"/forgot", url.Values{"email": {"ada@example.com"}}); code != http.StatusNotFound {
			t.Errorf("%s: POST /forgot: %d", name, code)
		}
		if _, body := call(t, http.DefaultClient, "GET", srv.URL+"/login", "", nil); strings.Contains(body, "/forgot") {
			t.Errorf("%s: the sign-in page offers a reset it can't send", name)
		}
		// A new member's first password is still shown, as before there was mail.
		code, body := call(t, client, "POST", srv.URL+"/o/alpha/ui/member", "application/json", map[string]string{"email": "grace@example.com", "name": "Grace"})
		if code != http.StatusOK || !strings.Contains(body, "first password") {
			t.Errorf("%s: adding a member: %d %s", name, code, body)
		}
		box.take(t, 0)
	}
}

func TestPasswordReset(t *testing.T) {
	s, srv, ada, box := mailSite(t, nil)
	if _, body := call(t, http.DefaultClient, "GET", srv.URL+"/login", "", nil); !strings.Contains(body, `href="/forgot"`) {
		t.Fatal("the sign-in page doesn't offer a reset")
	}

	// A stranger learns nothing: the answer is the same, and nothing is sent.
	_, unknown := postForm(t, http.DefaultClient, srv.URL+"/forgot", url.Values{"email": {"nobody@example.com"}})
	box.take(t, 0)

	// The link points at PROSPECTOR_URL whatever host the request named.
	req, _ := http.NewRequest("POST", srv.URL+"/forgot", strings.NewReader("email=Ada%40Example.com"))
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Host = "evil.example"
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("POST /forgot: %v %v", res, err)
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
	postForm(t, http.DefaultClient, srv.URL+"/forgot", url.Values{"email": {"ada@example.com"}})
	box.take(t, 0)

	// The token is stored hashed.
	var stored PasswordToken
	if err := s.db.First(&stored).Error; err != nil || strings.Contains(link, stored.TokenHash) {
		t.Fatalf("stored token: %+v %v", stored, err)
	}

	// Opening the link, as a mail scanner does, doesn't spend it.
	for range 2 {
		if code, body := call(t, http.DefaultClient, "GET", srv.URL+link, "", nil); code != http.StatusOK || !strings.Contains(body, "ada@example.com") {
			t.Fatalf("GET %s: %d", link, code)
		}
	}
	if code, body := postForm(t, http.DefaultClient, srv.URL+link, url.Values{"password": {"short"}}); code != http.StatusBadRequest || !strings.Contains(body, "10 characters") {
		t.Fatalf("a short password: %d", code)
	}
	if code, _ := postForm(t, http.DefaultClient, srv.URL+link+"x", url.Values{"password": {"a brand new password"}}); code != http.StatusGone {
		t.Fatalf("a made-up link: %d", code)
	}

	stay := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ = http.NewRequest("POST", srv.URL+link, strings.NewReader("password=a+brand+new+password"))
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	res, err = stay.Do(req)
	if err != nil || res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/login?reset=1" {
		t.Fatalf("choosing a password: %v %v", res, err)
	}
	res.Body.Close()

	if _, body := call(t, ada, "GET", srv.URL+"/o/alpha/settings", "", nil); strings.Contains(body, "Your password") {
		t.Error("the session from before the reset still works")
	}
	if signIn(t, newBrowser(), srv, "ada@example.com", "correct horse battery") {
		t.Error("the old password still works")
	}
	if !signIn(t, newBrowser(), srv, "ada@example.com", "a brand new password") {
		t.Error("the new password doesn't work")
	}
	if code, _ := postForm(t, http.DefaultClient, srv.URL+link, url.Values{"password": {"yet another password"}}); code != http.StatusGone {
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
	s, srv, _, box := mailSite(t, nil)
	token := must(s.InviteToken(must(s.UserByEmail("ada@example.com")).ID))
	for _, path := range []string{"/forgot", "/reset/" + token} {
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader("email=ada%40example.com&password=chosen+by+another+site"))
		req.Header.Set("content-type", "application/x-www-form-urlencoded")
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
	if !signIn(t, newBrowser(), srv, "ada@example.com", "correct horse battery") {
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
	s, srv, ada, box := mailSite(t, nil)
	code, body := call(t, ada, "POST", srv.URL+"/o/alpha/ui/member", "application/json", map[string]string{"email": "grace@example.com", "name": "Grace Hopper"})
	if code != http.StatusOK || strings.Contains(body, "first password") || !strings.Contains(body, "emailed a link") {
		t.Fatalf("adding a member: %d %s", code, body)
	}
	mail := box.take(t, 1)[0]
	if mail.To != "grace@example.com" || !strings.Contains(mail.Subject, "Ada Lovelace added you to Alpha") {
		t.Fatalf("invitation: %+v", mail)
	}
	if code, _ := postForm(t, http.DefaultClient, srv.URL+linkIn(t, mail), url.Values{"password": {"grace's own password"}}); code != http.StatusOK {
		t.Fatalf("choosing a password from the invitation: %d", code)
	}
	grace := newBrowser()
	if !signIn(t, grace, srv, "grace@example.com", "grace's own password") {
		t.Fatal("Grace can't sign in with the password she chose")
	}
	if _, body := call(t, grace, "GET", srv.URL+"/o/alpha/today", "", nil); strings.Contains(body, "Choose your own password") {
		t.Error("Grace is asked to change a password nobody else has seen")
	}
	if u := must(s.UserByEmail("grace@example.com")); u.MustChangePassword {
		t.Error("the account is still marked as on a first password")
	}

	// Someone added again before choosing a password is sent the link again.
	if code, _ := call(t, ada, "POST", srv.URL+"/o/alpha/ui/member", "application/json", map[string]string{"email": "alan@example.com", "name": "Alan"}); code != http.StatusOK {
		t.Fatal("adding Alan")
	}
	first := linkIn(t, box.take(t, 1)[0])
	code, body = call(t, ada, "POST", srv.URL+"/o/alpha/ui/member", "application/json", map[string]string{"email": "alan@example.com"})
	if code != http.StatusOK || !strings.Contains(body, "emailed a link") || strings.Contains(body, "first password") {
		t.Fatalf("adding Alan again: %d %s", code, body)
	}
	if again := linkIn(t, box.take(t, 1)[0]); again == first {
		t.Error("the second invitation reused the first link")
	}

	// Someone who already has an account is added without mail.
	other := mustOrg(t, s, "beta")
	if err := s.AddMember(other.ID, must(s.UserByEmail("ada@example.com")).ID, "owner", "Ada"); err != nil {
		t.Fatal(err)
	}
	if code, _ := call(t, ada, "POST", srv.URL+"/o/beta/ui/member", "application/json", map[string]string{"email": "grace@example.com"}); code != http.StatusOK {
		t.Fatalf("adding an existing account: %d", code)
	}
	box.take(t, 0)

	// When the mail can't be sent the owner is told why and given the password instead.
	box.fail = errors.New("smtp.site.example refused the username and password")
	code, body = call(t, ada, "POST", srv.URL+"/o/alpha/ui/member", "application/json", map[string]string{"email": "edsger@example.com", "name": "Edsger"})
	if code != http.StatusOK || !strings.Contains(body, "refused the username") || !strings.Contains(body, "first password") {
		t.Fatalf("adding a member when mail fails: %d %s", code, body)
	}
}

func TestOrganisationMailServer(t *testing.T) {
	s, srv, ada, box := mailSite(t, nil)
	o := must(s.OrgBySlug("alpha"))
	save := func(c *http.Client, in map[string]any) (int, string) {
		t.Helper()
		return call(t, c, "POST", srv.URL+"/o/alpha/ui/mail", "application/json", in)
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
	if strings.Contains(stored.Password, "s3cret") || !strings.HasPrefix(stored.Password, "v1:") {
		t.Fatalf("the password is stored unsealed: %q", stored.Password)
	}
	_, page := call(t, ada, "GET", srv.URL+"/o/alpha/settings", "", nil)
	if strings.Contains(page, "s3cret") || strings.Contains(page, stored.Password) || !strings.Contains(page, "smtp.alpha.example") {
		t.Fatal("Settings shows the password, or doesn't show the server")
	}

	// Invitations to this organisation now leave through its server, as its address.
	if code, body := call(t, ada, "POST", srv.URL+"/o/alpha/ui/member", "application/json", map[string]string{"email": "grace@example.com", "name": "Grace"}); code != http.StatusOK {
		t.Fatalf("adding a member: %d %s", code, body)
	}
	via := box.take(t, 1)[0].via
	if via.Host != "smtp.alpha.example" || via.Password != "s3cret-smtp-pass" || via.From != "Alpha <hello@alpha.example>" {
		t.Fatalf("sent through %+v", via)
	}
	// A reset is the account's, not an organisation's, so it stays with the site.
	postForm(t, http.DefaultClient, srv.URL+"/forgot", url.Values{"email": {"ada@example.com"}})
	if via := box.take(t, 1)[0].via; via.Host != "smtp.site.example" {
		t.Fatalf("a reset was sent through %+v", via)
	}

	if code, body := call(t, ada, "POST", srv.URL+"/o/alpha/ui/mail/test", "application/json", map[string]any{}); code != http.StatusOK || !strings.Contains(body, "ada@example.com") {
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
	member := newBrowser()
	signIn(t, member, srv, grace.Email, "grace's own password")
	if code, _ := save(member, server); code != http.StatusForbidden {
		t.Errorf("a member saving the mail server: %d", code)
	}
	if code, body := call(t, member, "GET", srv.URL+"/o/alpha/settings", "", nil); code != http.StatusOK || strings.Contains(body, "smtp.elsewhere.example") {
		t.Errorf("a member's Settings shows the mail server (%d)", code)
	}

	if code, _ := save(ada, map[string]any{"host": ""}); code != http.StatusOK {
		t.Fatal("clearing the server")
	}
	if _, err := s.OrgMail(o.ID); err == nil {
		t.Error("the cleared server is still stored")
	}
}

func TestMailPasswordNeedsAKey(t *testing.T) {
	s, srv, ada, _ := mailSite(t, func(a *App) { a.seal = nil })
	code, body := call(t, ada, "POST", srv.URL+"/o/alpha/ui/mail", "application/json",
		map[string]any{"host": "smtp.alpha.example", "port": 587, "username": "u", "password": "s3cret-smtp-pass", "sender": "a@alpha.example"})
	if code != http.StatusBadRequest || !strings.Contains(body, "PROSPECTOR_SECRET_KEY") {
		t.Fatalf("saving a password with nothing to seal it: %d %s", code, body)
	}
	if _, err := s.OrgMail(must(s.OrgBySlug("alpha")).ID); err == nil {
		t.Error("it was stored anyway")
	}

	for _, bad := range []string{"short", strings.Repeat("zz", 32)} {
		if _, err := newSealer(bad); err == nil {
			t.Errorf("a key of %q was accepted", bad)
		}
	}
	a, b := must(newSealer(strings.Repeat("01", 32))), must(newSealer(strings.Repeat("02", 32)))
	if got, err := a.open(a.seal("hunter2hunter2")); err != nil || got != "hunter2hunter2" {
		t.Errorf("a sealed password came back as %q, %v", got, err)
	}
	if _, err := b.open(a.seal("hunter2hunter2")); err == nil {
		t.Error("another key opened it")
	}
}

func TestMailFromEnv(t *testing.T) {
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
}
