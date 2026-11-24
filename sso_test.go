package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// fakeIdP is a working OpenID Connect provider: discovery, keys, and a token endpoint
// that checks PKCE and signs an ID token echoing the nonce. With tenant set it is
// Microsoft's endpoint for every tenant; with plain set it is LinkedIn.
type fakeIdP struct {
	t      *testing.T
	srv    *httptest.Server
	key    *rsa.PrivateKey
	tenant string
	plain  bool

	mu       sync.Mutex
	claims   map[string]any
	userinfo map[string]any
	grants   map[string]url.Values
	nonce    string
	// asked counts calls to the token endpoint.
	asked int
}

func newIdP(t *testing.T, tenant string, plain bool) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeIdP{t: t, key: key, tenant: tenant, plain: plain, grants: map[string]url.Values{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		issuer := p.srv.URL
		if p.tenant != "" {
			issuer += "/{tenantid}/v2.0"
		}
		writeJSON(w, map[string]any{"issuer": issuer, "jwks_uri": p.srv.URL + "/jwks", "authorization_endpoint": p.srv.URL + "/authorize",
			"token_endpoint": p.srv.URL + "/token", "userinfo_endpoint": p.srv.URL + "/userinfo",
			"id_token_signing_alg_values_supported": []string{"RS256"}, "authorization_response_iss_parameter_supported": !p.plain})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"keys": []map[string]any{{"kty": "RSA", "kid": "k", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		p.mu.Lock()
		p.asked++
		asked, ok := p.grants[r.PostForm.Get("code")]
		delete(p.grants, r.PostForm.Get("code"))
		p.mu.Unlock()
		digest := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		switch {
		case !ok:
		case p.plain:
			ok = r.PostForm.Get("client_secret") == "secret" && r.PostForm.Get("code_verifier") == ""
		default:
			ok = base64.RawURLEncoding.EncodeToString(digest[:]) == asked.Get("code_challenge")
		}
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": "invalid_grant"})
			return
		}
		writeJSON(w, map[string]string{"access_token": "at", "token_type": "Bearer", "id_token": p.idToken(asked)})
	})
	mux.HandleFunc("GET /userinfo", func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		writeJSON(w, p.userinfo)
	})
	p.srv = httptest.NewTLSServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakeIdP) issuer() string {
	if p.tenant != "" {
		return p.srv.URL + "/" + p.tenant + "/v2.0"
	}
	return p.srv.URL
}

// says sets what the provider's next ID tokens claim about who signed in.
func (p *fakeIdP) says(claims map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.claims = claims
}

func (p *fakeIdP) idToken(asked url.Values) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	claims := map[string]any{"iss": p.issuer(), "aud": asked.Get("client_id"), "nonce": asked.Get("nonce"),
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()}
	if p.tenant != "" {
		claims["tid"] = p.tenant
	}
	if p.nonce != "" {
		claims["nonce"] = p.nonce
	}
	for k, v := range p.claims {
		claims[k] = v
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: p.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k"))
	if err != nil {
		p.t.Fatal(err)
	}
	token, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		p.t.Fatal(err)
	}
	return token
}

// approve plays the person signing in at the provider, and is the answer sent back.
func (p *fakeIdP) approve(sentTo string) url.Values {
	p.t.Helper()
	u, err := url.Parse(sentTo)
	if err != nil {
		p.t.Fatal(err)
	}
	q := u.Query()
	if !strings.HasSuffix(q.Get("redirect_uri"), "/login/callback") || !strings.Contains(q.Get("scope"), "openid") {
		p.t.Fatalf("asked the provider with the wrong redirect or scope: %s", sentTo)
	}
	if guarded := q.Get("code_challenge_method") == "S256" && q.Get("nonce") != ""; guarded == p.plain {
		p.t.Fatalf("PKCE and a nonce should be sent to every provider but LinkedIn: %s", sentTo)
	}
	code := randomToken(8)
	p.mu.Lock()
	p.grants[code] = q
	p.mu.Unlock()
	answer := url.Values{"code": {code}, "state": {q.Get("state")}}
	if !p.plain {
		answer.Set("iss", p.issuer())
	}
	return answer
}

func (p *fakeIdP) way(key string) socialWay {
	return socialWay{Key: key, Label: key, Issuer: p.srv.URL, ClientID: "client", Secret: "secret", Plain: p.plain, ManyTenants: p.tenant != ""}
}

// ssoServer is the app at a known address, reaching the fake providers and a fake DNS.
func ssoServer(t *testing.T, s *Store, dns map[string][]string, idps ...*fakeIdP) (*App, *httptest.Server) {
	t.Helper()
	pool := x509.NewCertPool()
	for _, p := range idps {
		pool.AddCert(p.srv.Certificate())
	}
	app := &App{s: s, outbound: &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}},
		txt: func(_ context.Context, name string) ([]string, error) { return dns[name], nil }}
	var routes http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { routes.ServeHTTP(w, r) }))
	t.Cleanup(srv.Close)
	app.base = srv.URL
	routes = app.Routes()
	return app, srv
}

// browser keeps cookies and never follows a redirect by itself.
type browser struct {
	t    *testing.T
	c    *http.Client
	base string
}

func newBrowser(t *testing.T, base string) *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{t: t, base: base, c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (b *browser) get(path string) (code int, body, location string) {
	b.t.Helper()
	req, _ := http.NewRequest("GET", b.base+path, nil)
	res, err := b.c.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer res.Body.Close()
	text, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(text), res.Header.Get("Location")
}

func (b *browser) post(path string, body any) (int, string) {
	b.t.Helper()
	return call(b.t, b.c, "POST", b.base+path, "application/json", body)
}

func (b *browser) login(email, password string) {
	b.t.Helper()
	res, err := b.c.PostForm(b.base+"/login", url.Values{"email": {email}, "password": {password}})
	if err != nil || res.StatusCode != http.StatusSeeOther {
		b.t.Fatalf("login as %s: %v %v", email, res.Status, err)
	}
	res.Body.Close()
}

// through walks a sign-in from its first link, through the provider and back.
func (b *browser) through(p *fakeIdP, start string) (code int, body, location string) {
	b.t.Helper()
	if strings.HasPrefix(start, "/login/sso/start") {
		// The link is followed from the page that offers it.
		b.get("/login/sso")
	}
	code, body, location = b.get(start)
	if code != http.StatusSeeOther || !strings.HasPrefix(location, p.srv.URL+"/authorize") {
		return code, body, location
	}
	return b.get("/login/callback?" + p.approve(location).Encode())
}

func mustUser(t *testing.T, s *Store, o *Org, email, name, role string) *User {
	t.Helper()
	u, err := s.CreateUser(email, name, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if o != nil {
		if err := s.AddMember(o.ID, u.ID, role, name); err != nil {
			t.Fatal(err)
		}
	}
	return u
}

func identities(t *testing.T, s *Store) int64 {
	t.Helper()
	var n int64
	if err := s.db.Model(&Identity{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSocialSignIn(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	mustUser(t, s, o, "ada@example.com", "Ada Lovelace", "owner")
	google := newIdP(t, "", false)
	app, srv := ssoServer(t, s, nil, google)
	app.social = []socialWay{google.way("google")}

	if _, body, _ := newBrowser(t, srv.URL).get("/login?next=/o/alpha/research"); !strings.Contains(body, `href="/login/with/google?next=%2Fo%2Falpha%2Fresearch"`) {
		t.Fatal("the sign-in page doesn't offer the provider, or drops where the person was going")
	}

	// An address the provider hasn't verified matches no account.
	google.says(map[string]any{"sub": "g-1", "email": "ada@example.com", "email_verified": false})
	b := newBrowser(t, srv.URL)
	if code, body, _ := b.through(google, "/login/with/google"); code != http.StatusForbidden || !strings.Contains(body, "didn&#39;t confirm") {
		t.Fatalf("an unverified address: %d %s", code, body)
	}
	if identities(t, s) != 0 {
		t.Fatal("an unverified address was linked to an account")
	}

	// Nobody gets an account by signing in.
	google.says(map[string]any{"sub": "g-2", "email": "stranger@example.com", "email_verified": true})
	if code, body, _ := b.through(google, "/login/with/google"); code != http.StatusForbidden || !strings.Contains(body, "no account here") {
		t.Fatalf("a stranger: %d %s", code, body)
	}
	if _, err := s.UserByEmail("stranger@example.com"); err == nil {
		t.Fatal("a social sign-in made an account")
	}

	google.says(map[string]any{"sub": "g-1", "email": "ada@example.com", "email_verified": true})
	code, _, location := b.through(google, "/login/with/google?next=/o/alpha/research")
	if code != http.StatusSeeOther || location != "/o/alpha/research" {
		t.Fatalf("a verified address: %d to %q", code, location)
	}
	if code, body, _ := b.get("/o/alpha/research"); code != http.StatusOK || !strings.Contains(body, "Ada Lovelace") {
		t.Fatalf("the app after a social sign-in: %d", code)
	}

	// The link is the provider's subject: a changed or unverified address there still signs the same person in.
	google.says(map[string]any{"sub": "g-1", "email": "renamed@example.com", "email_verified": false})
	if code, _, _ := newBrowser(t, srv.URL).through(google, "/login/with/google"); code != http.StatusSeeOther || identities(t, s) != 1 {
		t.Fatalf("a linked subject under a new address: %d", code)
	}

	t.Run("answers that aren't believed", func(t *testing.T) {
		google.says(map[string]any{"sub": "g-1", "email": "ada@example.com", "email_verified": true})
		start := func(b *browser) url.Values {
			_, _, location := b.get("/login/with/google")
			return google.approve(location)
		}
		refused := func(what string, b *browser, answer url.Values) {
			t.Helper()
			if code, _, _ := b.get("/login/callback?" + answer.Encode()); code != http.StatusForbidden && code != http.StatusBadRequest {
				t.Errorf("%s: %d", what, code)
			}
			if code, _, _ := b.get("/o/alpha/today"); code != http.StatusSeeOther {
				t.Errorf("%s signed somebody in", what)
			}
		}
		b := newBrowser(t, srv.URL)
		answer := start(b)
		refused("an answer in a browser that never asked", newBrowser(t, srv.URL), answer)

		b = newBrowser(t, srv.URL)
		answer = start(b)
		answer.Set("state", "another")
		refused("an answer with another state", b, answer)

		b = newBrowser(t, srv.URL)
		answer = start(b)
		answer.Set("iss", "https://elsewhere.example")
		refused("an answer naming another issuer", b, answer)

		b = newBrowser(t, srv.URL)
		answer = start(b)
		answer.Del("iss")
		refused("an answer naming no issuer from a provider that always does", b, answer)

		b = newBrowser(t, srv.URL)
		answer = start(b)
		answer.Set("code", "guessed")
		refused("an answer with a code the provider never issued", b, answer)

		google.nonce = "from-another-sign-in"
		b = newBrowser(t, srv.URL)
		refused("an ID token issued for another sign-in", b, start(b))
		google.nonce = ""

		// A cookie made up elsewhere can't make the server go to the provider with it.
		b = newBrowser(t, srv.URL)
		made, _ := json.Marshal(attempt{State: "made-up", Verifier: "v", Nonce: "n", Way: "google"})
		home, _ := url.Parse(srv.URL)
		b.c.Jar.SetCookies(home, []*http.Cookie{{Name: attemptCookie, Value: base64.RawURLEncoding.EncodeToString(made), Path: "/login"}})
		before := google.asked
		refused("an attempt cookie this server didn't write", b, url.Values{"state": {"made-up"}, "code": {"x"}, "iss": {google.issuer()}})
		if google.asked != before {
			t.Error("a made-up attempt reached the provider's token endpoint")
		}

		// An answer is taken once.
		b = newBrowser(t, srv.URL)
		answer = start(b)
		if code, _, _ := b.get("/login/callback?" + answer.Encode()); code != http.StatusSeeOther {
			t.Fatalf("a good answer: %d", code)
		}
		if code, _, _ := b.get("/login/callback?" + answer.Encode()); code != http.StatusBadRequest {
			t.Errorf("the same answer a second time: %d", code)
		}
	})
}

func TestMicrosoftVouchesOnlyThroughItsDomainOwnerClaim(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	mustUser(t, s, o, "ada@example.com", "Ada Lovelace", "owner")
	microsoft := newIdP(t, "tenant-1", false)
	app, srv := ssoServer(t, s, nil, microsoft)
	app.social = []socialWay{microsoft.way("microsoft")}

	// A tenant's admin can put any address in email, and email_verified beside it.
	microsoft.says(map[string]any{"sub": "m-1", "email": "ada@example.com", "email_verified": true})
	if code, _, _ := newBrowser(t, srv.URL).through(microsoft, "/login/with/microsoft"); code != http.StatusForbidden || identities(t, s) != 0 {
		t.Fatalf("an address Microsoft didn't vouch for: %d", code)
	}
	microsoft.says(map[string]any{"sub": "m-1", "email": "ada@example.com", "xms_edov": true, "tid": "another-tenant"})
	if code, _, _ := newBrowser(t, srv.URL).through(microsoft, "/login/with/microsoft"); code != http.StatusForbidden || identities(t, s) != 0 {
		t.Fatalf("a token naming a tenant that didn't issue it: %d", code)
	}
	microsoft.says(map[string]any{"sub": "m-1", "email": "ada@example.com", "xms_edov": true})
	if code, _, location := newBrowser(t, srv.URL).through(microsoft, "/login/with/microsoft"); code != http.StatusSeeOther || location != "/" {
		t.Fatalf("an address Microsoft vouched for: %d to %q", code, location)
	}
}

func TestLinkedInSignIn(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	mustUser(t, s, o, "ada@example.com", "Ada Lovelace", "owner")
	linkedin := newIdP(t, "", true)
	app, srv := ssoServer(t, s, nil, linkedin)
	app.social = []socialWay{linkedin.way("linkedin")}

	// Its ID token carries only who; the address comes from userinfo.
	linkedin.says(map[string]any{"sub": "l-1"})
	linkedin.userinfo = map[string]any{"sub": "l-1", "email": "ada@example.com", "email_verified": false}
	if code, _, _ := newBrowser(t, srv.URL).through(linkedin, "/login/with/linkedin"); code != http.StatusForbidden {
		t.Fatalf("an unverified LinkedIn address: %d", code)
	}
	linkedin.userinfo = map[string]any{"sub": "somebody-else", "email": "ada@example.com", "email_verified": true}
	if code, _, _ := newBrowser(t, srv.URL).through(linkedin, "/login/with/linkedin"); code != http.StatusForbidden {
		t.Fatalf("userinfo describing somebody else: %d", code)
	}
	linkedin.userinfo = map[string]any{"sub": "l-1", "email": "ada@example.com", "email_verified": true}
	if code, _, _ := newBrowser(t, srv.URL).through(linkedin, "/login/with/linkedin"); code != http.StatusSeeOther {
		t.Fatalf("a verified LinkedIn address: %d", code)
	}
}

// connected sets an organisation's single sign-on up the way its owner would in Settings.
func connected(t *testing.T, srv *httptest.Server, dns map[string][]string, s *Store, slug, domain string, p *fakeIdP, rules map[string]any) {
	t.Helper()
	owner := newBrowser(t, srv.URL)
	o, _ := s.OrgBySlug(slug)
	members, _ := s.Members(o.ID)
	for _, m := range members {
		if m.Role == "owner" {
			owner.login(m.Email, "correct horse battery")
			break
		}
	}
	base := "/o/" + slug + "/ui/sso"
	if code, body := owner.post(base+"/domain", map[string]string{"domain": domain}); code != http.StatusOK {
		t.Fatalf("adding %s: %d %s", domain, code, body)
	}
	sso, _ := s.SSO(o.ID)
	for _, d := range sso.Domains {
		dns[d.Record()] = []string{d.Token}
	}
	if code, body := owner.post(base+"/verify", map[string]string{"domain": domain}); code != http.StatusOK {
		t.Fatalf("verifying %s: %d %s", domain, code, body)
	}
	in := map[string]any{"issuer": p.srv.URL, "client_id": "client", "client_secret": "secret"}
	for k, v := range rules {
		in[k] = v
	}
	if code, body := owner.post(base, in); code != http.StatusOK {
		t.Fatalf("saving the connection: %d %s", code, body)
	}
}

func TestOrganisationSingleSignOn(t *testing.T) {
	s := tempStore(t)
	alpha, beta := mustOrg(t, s, "alpha"), mustOrg(t, s, "beta")
	mustUser(t, s, alpha, "ada@alpha.example", "Ada Lovelace", "owner")
	bob := mustUser(t, s, alpha, "bob@alpha.example", "Bob Stone", "member")
	if err := s.AddMember(beta.ID, bob.ID, "member", bob.Name); err != nil {
		t.Fatal(err)
	}
	okta := newIdP(t, "", false)
	dns := map[string][]string{}
	_, srv := ssoServer(t, s, dns, okta)
	connected(t, srv, dns, s, "alpha", "alpha.example", okta, nil)

	b := newBrowser(t, srv.URL)
	_, body, _ := b.get("/login/sso?email=Bob@Alpha.example")
	link := regexp.MustCompile(`href="(/login/sso/start\?[^"]+)"`).FindStringSubmatch(body)
	if link == nil || !strings.Contains(body, "Continue to Alpha") {
		t.Fatalf("the organisation isn't offered to an address at its domain: %s", body)
	}
	start := strings.ReplaceAll(link[1], "&amp;", "&")
	if _, body, _ := b.get("/login/sso?email=bob@elsewhere.example"); strings.Contains(body, "Continue to") {
		t.Fatal("an organisation was offered to an address at no domain of its own")
	}
	if code, _, location := b.get("/login/sso/start?org=alpha&email=bob@elsewhere.example"); code != http.StatusBadRequest || location != "" {
		t.Fatalf("starting for an address the organisation doesn't speak for: %d %q", code, location)
	}
	// Nobody is sent to an owner's chosen provider, and into the account it names, on a
	// link alone: a browser that hasn't seen the page is shown it first.
	if code, _, location := newBrowser(t, srv.URL).get(start); code != http.StatusSeeOther || !strings.HasPrefix(location, "/login/sso?") {
		t.Fatalf("the link to the provider, followed from somewhere else: %d %q", code, location)
	}
	res, err := http.Get(srv.URL + "/login/sso")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if cookies := res.Cookies(); len(cookies) != 1 || cookies[0].SameSite != http.SameSiteStrictMode || !cookies[0].HttpOnly {
		t.Fatalf("the mark that the page was seen must not travel with a link from another site: %+v", cookies)
	}

	// The provider may speak only for the organisation's own domains.
	okta.says(map[string]any{"sub": "o-eve", "email": "bob@elsewhere.example", "email_verified": true})
	if code, body, _ := b.through(okta, start); code != http.StatusForbidden || !strings.Contains(body, "shown is its own") {
		t.Fatalf("the provider vouching for another domain: %d %s", code, body)
	}
	okta.says(map[string]any{"sub": "o-eve", "email": "bob@alpha.example", "email_verified": false})
	if code, _, _ := b.through(okta, start); code != http.StatusForbidden {
		t.Fatalf("an address the provider says is unverified: %d", code)
	}
	okta.says(map[string]any{"sub": "o-new", "email": "newcomer@alpha.example"})
	if code, body, _ := b.through(okta, start); code != http.StatusForbidden || !strings.Contains(body, "no account here") {
		t.Fatalf("somebody with no account, when people can't join: %d %s", code, body)
	}

	okta.says(map[string]any{"sub": "o-bob", "email": "bob@alpha.example"})
	if code, _, location := b.through(okta, start); code != http.StatusSeeOther || location != "/o/alpha/today" {
		t.Fatalf("a member signing in: %d to %q", code, location)
	}
	if code, body, _ := b.get("/o/alpha/today"); code != http.StatusOK || !strings.Contains(body, "Bob Stone") {
		t.Fatalf("the organisation after its own sign-in: %d", code)
	}
	// Alpha's provider vouched for Bob at Alpha, and nowhere else he belongs.
	if code, _, location := b.get("/o/beta/today"); code != http.StatusSeeOther || !strings.HasPrefix(location, "/login?") {
		t.Fatalf("another organisation, on a session one organisation's provider began: %d %q", code, location)
	}
	if code, body, _ := b.get("/o/alpha/today"); code != http.StatusOK || strings.Contains(body, "/o/beta/") {
		t.Fatal("the organisation switcher offers one this session can't open")
	}
	if code, _ := b.post("/orgs", map[string]string{"name": "Mine"}); code != http.StatusForbidden {
		t.Fatalf("starting an organisation on a limited session: %d", code)
	}
	if code, _ := b.post("/o/beta/ui/add", QuickAdd{Name: "Hannah Pryce"}); code != http.StatusUnauthorized {
		t.Fatalf("acting in another organisation on a limited session: %d", code)
	}

	// A linked subject whose account has since moved off the organisation's domains isn't its to vouch for.
	if err := s.db.Model(&User{}).Where("id = ?", bob.ID).Update("email", "bob@elsewhere.example").Error; err != nil {
		t.Fatal(err)
	}
	if code, body, _ := newBrowser(t, srv.URL).through(okta, start); code != http.StatusForbidden || !strings.Contains(body, "isn&#39;t at this organisation") {
		t.Fatalf("a linked account now at another domain: %d %s", code, body)
	}
}

func TestRequiredSingleSignOn(t *testing.T) {
	s := tempStore(t)
	alpha, beta := mustOrg(t, s, "alpha"), mustOrg(t, s, "beta")
	mustUser(t, s, alpha, "ada@alpha.example", "Ada Lovelace", "owner")
	bob := mustUser(t, s, alpha, "bob@alpha.example", "Bob Stone", "member")
	mustUser(t, s, alpha, "carol@contractor.example", "Carol Reyes", "member")
	if err := s.AddMember(beta.ID, bob.ID, "member", bob.Name); err != nil {
		t.Fatal(err)
	}
	okta := newIdP(t, "", false)
	dns := map[string][]string{}
	_, srv := ssoServer(t, s, dns, okta)
	connected(t, srv, dns, s, "alpha", "alpha.example", okta, map[string]any{"required": true})

	b := newBrowser(t, srv.URL)
	b.login("bob@alpha.example", "correct horse battery")
	code, _, location := b.get("/o/alpha/research")
	// He is sent to a page with a link, not on to the provider: he may have just come from
	// the password form, and a form's redirects may not end at another site.
	if code != http.StatusSeeOther || !strings.HasPrefix(location, "/login/sso?") {
		t.Fatalf("a password session where single sign-on is required: %d %q", code, location)
	}
	_, asked, _ := b.get(location)
	link := regexp.MustCompile(`href="(/login/sso/start\?[^"]+)"`).FindAllStringSubmatch(asked, -1)
	if len(link) != 1 || !strings.Contains(asked, "Alpha asks its people") {
		t.Fatalf("the page a required sign-on sends a member to: %s", asked)
	}
	location = strings.ReplaceAll(link[0][1], "&amp;", "&")
	if code, body := b.post("/o/alpha/ui/add", QuickAdd{Name: "Hannah Pryce"}); code != http.StatusForbidden || !strings.Contains(body, "requires its single sign-on") {
		t.Fatalf("acting on a password session: %d %s", code, body)
	}
	if code, _, _ := b.get("/o/beta/today"); code != http.StatusOK {
		t.Fatalf("an organisation that requires nothing: %d", code)
	}

	// Going through it adds the proof to the session he has, which keeps opening the rest.
	okta.says(map[string]any{"sub": "o-bob", "email": "bob@alpha.example"})
	if code, _, location := b.through(okta, location); code != http.StatusSeeOther || location != "/o/alpha/research" {
		t.Fatalf("going through the required sign-in: %d to %q", code, location)
	}
	for _, page := range []string{"/o/alpha/research", "/o/beta/today"} {
		if code, _, _ := b.get(page); code != http.StatusOK {
			t.Fatalf("%s after going through the required sign-in: %d", page, code)
		}
	}

	// Somebody else's sign-in at the provider proves nothing for the session already here.
	other := newBrowser(t, srv.URL)
	other.login("carol@contractor.example", "correct horse battery")
	if code, _, _ := other.get("/o/alpha/today"); code != http.StatusOK {
		t.Fatalf("a member at no domain of the organisation's, who its provider can't sign in: %d", code)
	}
	owner := newBrowser(t, srv.URL)
	owner.login("ada@alpha.example", "correct horse battery")
	if code, _, _ := owner.get("/o/alpha/settings"); code != http.StatusOK {
		t.Fatalf("an owner's password, kept so a broken provider can be put right: %d", code)
	}
	secret, _, err := s.CreateKey(alpha.ID, "bot", []string{"read"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/o/alpha/api/v1/due?step=linkedin", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("an API key where single sign-on is required: %v %v", res.Status, err)
	}

	// The command line is the way back in when the provider is gone and no owner can sign in.
	if err := ssoCommand(s, []string{"off", "alpha"}); err != nil {
		t.Fatal(err)
	}
	fresh := newBrowser(t, srv.URL)
	fresh.login("bob@alpha.example", "correct horse battery")
	if code, _, _ := fresh.get("/o/alpha/today"); code != http.StatusOK {
		t.Fatalf("a password after `sso off`: %d", code)
	}
	if sso, _ := s.SSO(alpha.ID); !sso.On() || sso.ClientSecret != "secret" {
		t.Fatal("`sso off` lost the connection itself")
	}
}

func TestPeopleJoinThroughSingleSignOn(t *testing.T) {
	s := tempStore(t)
	alpha := mustOrg(t, s, "alpha")
	mustUser(t, s, alpha, "ada@alpha.example", "Ada Lovelace", "owner")
	okta := newIdP(t, "", false)
	dns := map[string][]string{}
	_, srv := ssoServer(t, s, dns, okta)
	connected(t, srv, dns, s, "alpha", "alpha.example", okta, map[string]any{"auto_join": true})
	start := "/login/sso/start?org=alpha&email=dan@alpha.example"

	okta.says(map[string]any{"sub": "o-dan", "email": "Dan@Alpha.example", "name": "Dan Okafor", "role": "owner"})
	b := newBrowser(t, srv.URL)
	if code, _, _ := b.through(okta, start); code != http.StatusSeeOther {
		t.Fatalf("a newcomer at the organisation's domain: %d", code)
	}
	dan, err := s.UserByEmail("dan@alpha.example")
	if err != nil || dan.Name != "Dan Okafor" || dan.HasPassword() || dan.MustChangePassword {
		t.Fatalf("the account made for a newcomer: %+v %v", dan, err)
	}
	if m, err := s.Membership(alpha.ID, dan.ID); err != nil || m.Role != "member" {
		t.Fatalf("a newcomer's membership: %+v %v", m, err)
	}
	if _, _, err := s.Login("dan@alpha.example", ""); err == nil {
		t.Fatal("an account with no password signs in with an empty one")
	}
	if _, body, _ := b.get("/o/alpha/settings"); strings.Contains(body, "Your password") || !strings.Contains(body, "Your sign-in") {
		t.Fatal("Settings offers a password change to somebody who has no password")
	}

	// An owner who signs in this way stays an owner.
	okta.says(map[string]any{"sub": "o-ada", "email": "ada@alpha.example"})
	if code, _, _ := newBrowser(t, srv.URL).through(okta, start); code != http.StatusSeeOther {
		t.Fatalf("an owner through single sign-on: %d", code)
	}
	ada, _ := s.UserByEmail("ada@alpha.example")
	if m, _ := s.Membership(alpha.ID, ada.ID); m.Role != "owner" {
		t.Fatalf("an owner was made a %s by signing in", m.Role)
	}

	// The directory is the authority: removed here but still let in there, they join again.
	if _, err := s.RemoveMember(alpha.ID, dan.ID, "test"); err != nil {
		t.Fatal(err)
	}
	okta.says(map[string]any{"sub": "o-dan", "email": "dan@alpha.example"})
	if code, _, _ := newBrowser(t, srv.URL).through(okta, start); code != http.StatusSeeOther {
		t.Fatalf("rejoining: %d", code)
	}
	if _, err := s.Membership(alpha.ID, dan.ID); err != nil {
		t.Fatal("somebody the provider still lets in didn't join again")
	}
}

func TestInvitersPasswordEndsAtAProviderSignIn(t *testing.T) {
	s := tempStore(t)
	alpha := mustOrg(t, s, "alpha")
	mustUser(t, s, alpha, "ada@alpha.example", "Ada Lovelace", "owner")
	okta := newIdP(t, "", false)
	dns := map[string][]string{}
	_, srv := ssoServer(t, s, dns, okta)
	connected(t, srv, dns, s, "alpha", "alpha.example", okta, nil)

	bob, err := s.InviteUser("bob@alpha.example", "Bob Stone", "seen by the inviter")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddMember(alpha.ID, bob.ID, "member", bob.Name); err != nil {
		t.Fatal(err)
	}
	inviter, _, err := s.Login("bob@alpha.example", "seen by the inviter")
	if err != nil {
		t.Fatal(err)
	}
	okta.says(map[string]any{"sub": "o-bob", "email": "bob@alpha.example"})
	b := newBrowser(t, srv.URL)
	if code, _, _ := b.through(okta, "/login/sso/start?org=alpha&email=bob@alpha.example"); code != http.StatusSeeOther {
		t.Fatalf("an invited member's first sign-in through the provider: %d", code)
	}
	if code, body, _ := b.get("/o/alpha/today"); code != http.StatusOK || strings.Contains(body, "Choose your own password") {
		t.Fatalf("asked to change a password they were never told: %d", code)
	}
	if _, _, err := s.Login("bob@alpha.example", "seen by the inviter"); err == nil {
		t.Fatal("the password the inviter saw still signs in")
	}
	if _, err := s.SessionUser(inviter); err == nil {
		t.Fatal("a session opened with the inviter's password is still open")
	}
}

func TestSingleSignOnSettings(t *testing.T) {
	s := tempStore(t)
	alpha := mustOrg(t, s, "alpha")
	mustUser(t, s, alpha, "ada@alpha.example", "Ada Lovelace", "owner")
	mustUser(t, s, alpha, "bob@alpha.example", "Bob Stone", "member")
	okta := newIdP(t, "", false)
	dns := map[string][]string{}
	_, srv := ssoServer(t, s, dns, okta)
	owner := newBrowser(t, srv.URL)
	owner.login("ada@alpha.example", "correct horse battery")
	good := map[string]any{"issuer": okta.srv.URL, "client_id": "client", "client_secret": "s3cret-value"}
	with := func(k string, v any) map[string]any {
		out := map[string]any{k: v}
		for key, value := range good {
			if key != k {
				out[key] = value
			}
		}
		return out
	}
	for what, c := range map[string]struct {
		in   map[string]any
		want string
	}{
		"requiring it before a domain is verified": {with("required", true), "verify a domain first"},
		"joining before a domain is verified":      {with("auto_join", true), "verify a domain first"},
		"an issuer that isn't https":               {with("issuer", "http://login.alpha.example"), "an issuer looks like"},
		"Microsoft's sign-in for every tenant":     {with("issuer", "https://login.microsoftonline.com/common/v2.0"), "every tenant"},
		"no client secret":                         {with("client_secret", ""), "both needed"},
		"an issuer that can't be read":             {with("issuer", okta.srv.URL+"/nowhere"), "couldn't be read"},
	} {
		if code, body := owner.post("/o/alpha/ui/sso", c.in); code != http.StatusBadRequest || !strings.Contains(body, c.want) {
			t.Errorf("%s: %d %s", what, code, body)
		}
	}
	if sso, _ := s.SSO(alpha.ID); sso.On() {
		t.Fatal("a refused connection was saved")
	}

	if code, body := owner.post("/o/alpha/ui/sso/domain", map[string]string{"domain": "not a domain"}); code != http.StatusBadRequest {
		t.Errorf("a domain that isn't one: %d %s", code, body)
	}
	if code, body := owner.post("/o/alpha/ui/sso/domain", map[string]string{"domain": "@Alpha.Example"}); code != http.StatusOK {
		t.Fatalf("adding a domain: %d %s", code, body)
	}
	if code, body := owner.post("/o/alpha/ui/sso/verify", map[string]string{"domain": "alpha.example"}); code != http.StatusBadRequest || !strings.Contains(body, "_prospector-challenge.alpha.example") {
		t.Fatalf("verifying a domain whose DNS says nothing: %d %s", code, body)
	}
	dns["_prospector-challenge.alpha.example"] = []string{"somebody else's value"}
	if code, _ := owner.post("/o/alpha/ui/sso/verify", map[string]string{"domain": "alpha.example"}); code != http.StatusBadRequest {
		t.Fatalf("verifying a domain whose DNS holds another value: %d", code)
	}
	sso, _ := s.SSO(alpha.ID)
	if sso.Proves("ada@alpha.example") {
		t.Fatal("an unverified domain is believed")
	}
	dns["_prospector-challenge.alpha.example"] = []string{sso.Domains[0].Token}
	if code, body := owner.post("/o/alpha/ui/sso/verify", map[string]string{"domain": "alpha.example"}); code != http.StatusOK {
		t.Fatalf("verifying a domain: %d %s", code, body)
	}
	sso, _ = s.SSO(alpha.ID)
	if !sso.Proves("Ada@Alpha.Example") || sso.Proves("ada@mail.alpha.example") || sso.Proves("ada@notalpha.example") || sso.Proves("alpha.example") {
		t.Fatal("a verified domain should cover its own addresses and no others")
	}

	if code, body := owner.post("/o/alpha/ui/sso", with("required", true)); code != http.StatusOK {
		t.Fatalf("saving the connection: %d %s", code, body)
	}
	// Saving again without retyping the secret keeps it.
	if code, body := owner.post("/o/alpha/ui/sso", with("client_secret", "")); code != http.StatusOK {
		t.Fatalf("saving without the secret: %d %s", code, body)
	}
	if sso, _ := s.SSO(alpha.ID); sso.ClientSecret != "s3cret-value" || sso.Required {
		t.Fatalf("after saving again: %+v", sso.SSOConnection)
	}
	// The kept secret was typed for this provider and client, and is sent to no other.
	other := newIdP(t, "", false)
	for what, in := range map[string]map[string]any{
		"another client": {"issuer": okta.srv.URL, "client_id": "another"},
		"another issuer": {"issuer": other.srv.URL, "client_id": "client"},
	} {
		if code, body := owner.post("/o/alpha/ui/sso", in); code != http.StatusBadRequest || !strings.Contains(body, "secret again") {
			t.Errorf("the kept secret with %s: %d %s", what, code, body)
		}
	}
	// A provider that signs in every tenant under one registration pins nobody to this organisation.
	if code, body := owner.post("/o/alpha/ui/sso", with("issuer", newIdP(t, "any-tenant", false).srv.URL)); code != http.StatusBadRequest || !strings.Contains(body, "every tenant") {
		t.Errorf("an issuer for every tenant, at an address of its own: %d %s", code, body)
	}
	if sso, _ := s.SSO(alpha.ID); sso.Issuer != okta.srv.URL || sso.ClientID != "client" {
		t.Fatalf("a refused change was saved: %+v", sso.SSOConnection)
	}
	_, page, _ := owner.get("/o/alpha/settings")
	if !strings.Contains(page, srv.URL+"/login/callback") || !strings.Contains(page, "Kept. Type a new one") {
		t.Fatal("Settings doesn't show the redirect URI to register, or that the secret is kept")
	}
	var events []Event
	s.in(alpha.ID).Where("action = ?", "sso:change").Find(&events)
	if len(events) != 2 || events[0].User != "Ada Lovelace" {
		t.Fatalf("changes to the connection in the history: %+v", events)
	}
	for _, e := range events {
		page += deref(e.Value)
	}
	if strings.Contains(page, "s3cret-value") {
		t.Fatal("the client secret is shown back, or kept in the history")
	}
	tag, handler := regexp.MustCompile(`(?i)<script[^>]*>`), regexp.MustCompile(`(?i)<[^>]*\son[a-z]+\s*=`)
	_, ssoPage, _ := newBrowser(t, srv.URL).get("/login/sso?email=ada@alpha.example")
	for _, body := range []string{page, ssoPage} {
		for _, script := range tag.FindAllString(body, -1) {
			if !strings.Contains(script, "src=") {
				t.Errorf("an inline script the content policy would block: %s", script)
			}
		}
		if found := handler.FindString(body); found != "" {
			t.Errorf("an inline handler the content policy would block: %s", found)
		}
	}

	if code, body := owner.post("/o/alpha/ui/sso", with("required", true)); code != http.StatusOK {
		t.Fatalf("requiring it: %d %s", code, body)
	}
	if code, body := owner.post("/o/alpha/ui/sso/domain/remove", map[string]string{"domain": "alpha.example"}); code != http.StatusBadRequest || !strings.Contains(body, "last verified domain") {
		t.Fatalf("removing the last verified domain while it is required: %d %s", code, body)
	}

	member := newBrowser(t, srv.URL)
	member.login("bob@alpha.example", "correct horse battery")
	if err := ssoCommand(s, []string{"off", "alpha"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "/domain", "/domain/remove", "/verify"} {
		if code, _ := member.post("/o/alpha/ui/sso"+path, map[string]string{"domain": "alpha.example"}); code != http.StatusForbidden {
			t.Errorf("a member changing single sign-on at %q: %d", path, code)
		}
	}
	if _, body, _ := member.get("/o/alpha/settings"); strings.Contains(body, "Single sign-on") {
		t.Error("a member is shown the single sign-on settings")
	}

	if code, body := owner.post("/o/alpha/ui/sso", map[string]any{"issuer": ""}); code != http.StatusOK {
		t.Fatalf("turning it off: %d %s", code, body)
	}
	if sso, _ := s.SSO(alpha.ID); sso.On() || len(sso.Domains) != 1 {
		t.Fatalf("after turning it off: %+v", sso)
	}
	if _, body, _ := newBrowser(t, srv.URL).get("/login/sso?email=ada@alpha.example"); strings.Contains(body, "Continue to") {
		t.Fatal("an organisation with single sign-on off is still offered")
	}
}

func TestGoogleWorkspaceMustHoldTheAddress(t *testing.T) {
	c := &SSO{Domains: []SSODomain{{Domain: "alpha.example", VerifiedAt: new(time.Time)}}}
	if !c.Proves("ada@alpha.example") {
		t.Fatal("a verified domain isn't believed")
	}
	s := tempStore(t)
	alpha := mustOrg(t, s, "alpha")
	mustUser(t, s, alpha, "ada@alpha.example", "Ada Lovelace", "owner")
	google := newIdP(t, "", false)
	dns := map[string][]string{}
	_, srv := ssoServer(t, s, dns, google)
	connected(t, srv, dns, s, "alpha", "alpha.example", google, nil)
	old := googleIssuer
	googleIssuer = google.srv.URL
	t.Cleanup(func() { googleIssuer = old })
	start := "/login/sso/start?org=alpha&email=ada@alpha.example"

	// Anyone can open a personal Google account under a work address.
	google.says(map[string]any{"sub": "g-1", "email": "ada@alpha.example", "email_verified": true})
	if code, _, _ := newBrowser(t, srv.URL).through(google, start); code != http.StatusForbidden {
		t.Fatalf("a Google account outside the organisation's Workspace: %d", code)
	}
	google.says(map[string]any{"sub": "g-1", "email": "ada@alpha.example", "email_verified": true, "hd": "alpha.example"})
	if code, _, _ := newBrowser(t, srv.URL).through(google, start); code != http.StatusSeeOther {
		t.Fatalf("a Google account in the organisation's Workspace: %d", code)
	}
}

func TestSessionsSurviveTheSingleSignOnColumns(t *testing.T) {
	s := tempStore(t)
	if _, err := s.CreateUser("ada@example.com", "Ada Lovelace", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Login("ada@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	// A database from before the columns existed, with somebody signed in.
	for _, column := range []string{"sso_orgs", "limited"} {
		if err := s.db.Migrator().DropColumn(&Session{}, column); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.db.AutoMigrate(&Session{}); err != nil {
		t.Fatalf("adding the columns to a table with rows: %v", err)
	}
	sess, u, err := s.SessionFor(token)
	if err != nil || u.Email != "ada@example.com" || sess.Limited || sess.SSOOrgs != "" || !sess.Opens(7) || sess.Proves(7) {
		t.Fatalf("a session from before the upgrade: %+v %v", sess, err)
	}
	if err := s.AddProof(sess, 7); err != nil {
		t.Fatal(err)
	}
	if err := s.AddProof(sess, 71); err != nil {
		t.Fatal(err)
	}
	if sess, _, _ = s.SessionFor(token); !sess.Proves(7) || !sess.Proves(71) || sess.Proves(1) || sess.Limited {
		t.Fatalf("proofs added to a session: %+v", sess)
	}
}

func TestSingleSignOnIsAskedAgainAfterADay(t *testing.T) {
	sess := &Session{Limited: true}
	sess.prove(3, time.Now().Add(-proofLife-time.Minute))
	sess.prove(7, time.Now().Add(-time.Hour))
	if sess.Proves(3) || sess.Opens(3) {
		t.Fatal("going through the provider more than a day ago still counts")
	}
	if !sess.Proves(7) || !sess.Opens(7) {
		t.Fatal("going through the provider an hour ago doesn't count")
	}
	sess.prove(3, time.Now())
	if !sess.Proves(3) || !sess.Proves(7) {
		t.Fatalf("going through it again: %q", sess.SSOOrgs)
	}
}

func TestAnAccountWithNoPasswordIsRefusedAsSlowlyAsAnyOther(t *testing.T) {
	s := tempStore(t)
	if err := s.db.Create(&User{Email: "dan@example.com", Name: "Dan Okafor", PasswordHash: unusableHash()}).Error; err != nil {
		t.Fatal(err)
	}
	timed := func(email string) time.Duration {
		start := time.Now()
		if _, _, err := s.Login(email, "a guess at it"); err != errWrongLogin {
			t.Fatalf("signing in as %s: %v", email, err)
		}
		return time.Since(start)
	}
	// Otherwise the time taken says which addresses sign in through a provider.
	if unknown, none := timed("nobody@example.com"), timed("dan@example.com"); none < unknown/4 {
		t.Fatalf("an account with no password was refused in %v, an unknown address in %v", none, unknown)
	}
}

func TestSocialProvidersComeFromTheEnvironment(t *testing.T) {
	env := map[string]string{"PROSPECTOR_GOOGLE_CLIENT_ID": "g", "PROSPECTOR_GOOGLE_CLIENT_SECRET": "gs",
		"PROSPECTOR_LINKEDIN_CLIENT_ID": "l", "PROSPECTOR_LINKEDIN_CLIENT_SECRET": "ls"}
	ways, err := socialFromEnv(func(k string) string { return env[k] })
	if err != nil || len(ways) != 2 || ways[0].Key != "google" || ways[0].Secret != "gs" || ways[1].Key != "linkedin" || !ways[1].Plain {
		t.Fatalf("providers from the environment: %+v %v", ways, err)
	}
	delete(env, "PROSPECTOR_GOOGLE_CLIENT_SECRET")
	if _, err := socialFromEnv(func(k string) string { return env[k] }); err == nil || !strings.Contains(err.Error(), "PROSPECTOR_GOOGLE_CLIENT_SECRET") {
		t.Fatalf("an ID without its secret: %v", err)
	}
	if err := serve(&App{s: tempStore(t), social: ways}, "127.0.0.1:0"); err == nil || !strings.Contains(err.Error(), "PROSPECTOR_URL") {
		t.Fatalf("social sign-in without an address to send people back to: %v", err)
	}
	// With no provider configured and no address, the sign-in page offers only the password.
	srv := httptest.NewServer((&App{s: tempStore(t)}).Routes())
	t.Cleanup(srv.Close)
	b := newBrowser(t, srv.URL)
	if _, body, _ := b.get("/login"); strings.Contains(body, "/login/with/") || strings.Contains(body, "/login/sso") {
		t.Fatal("the sign-in page offers a way in that isn't set up")
	}
	for _, path := range []string{"/login/with/google", "/login/sso"} {
		if code, _, _ := b.get(path); code != http.StatusNotFound {
			t.Errorf("%s when it isn't set up: %d", path, code)
		}
	}
}

func TestProvidersInsideTheNetworkAreNeverReached(t *testing.T) {
	for addr, want := range map[string]bool{"8.8.8.8": true, "2606:4700::1111": true, "127.0.0.1": false, "10.1.2.3": false,
		"169.254.169.254": false, "192.168.1.1": false, "100.64.0.1": false, "::1": false, "fd00::1": false, "fe80::1": false,
		"::ffff:127.0.0.1": false, "0.0.0.0": false, "172.16.0.1": false} {
		if got := publicAddr(netip.MustParseAddr(addr)); got != want {
			t.Errorf("publicAddr(%s) = %v", addr, got)
		}
	}
	// An owner who types in an address on this machine gets a refusal, not a request made.
	asked := false
	inside := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { asked = true }))
	t.Cleanup(inside.Close)
	s := tempStore(t)
	alpha := mustOrg(t, s, "alpha")
	mustUser(t, s, alpha, "ada@alpha.example", "Ada Lovelace", "owner")
	app, srv := ssoServer(t, s, nil)
	app.outbound = nil
	owner := newBrowser(t, srv.URL)
	owner.login("ada@alpha.example", "correct horse battery")
	code, body := owner.post("/o/alpha/ui/sso", map[string]any{"issuer": inside.URL, "client_id": "client", "client_secret": "secret"})
	if code != http.StatusBadRequest || !strings.Contains(body, "isn't a public address") || asked {
		t.Fatalf("an issuer on this machine: %d %s (reached: %v)", code, body, asked)
	}
}

func TestSSOTokensAreJSONBooleansOrStrings(t *testing.T) {
	var c outsiderClaims
	if err := json.Unmarshal([]byte(`{"email_verified":"true","xms_edov":"1"}`), &c); err != nil || !bool(*c.Verified) || !bool(*c.DomainOwner) {
		t.Fatalf("flags written as strings: %+v %v", c, err)
	}
	if err := json.Unmarshal([]byte(`{"email_verified":"false","xms_edov":false}`), &c); err != nil || bool(*c.Verified) || bool(*c.DomainOwner) {
		t.Fatalf("flags that are off: %+v %v", c, err)
	}
}
