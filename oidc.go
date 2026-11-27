package main

import (
	"context"
	"crypto/hmac"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	errNotStartedHere = errors.New("that sign-in wasn't started in this browser, or it took too long: start again")
	errWrongIssuer    = errors.New("the answer came from another sign-in provider")
	errWrongNonce     = errors.New("the provider's answer was issued for another sign-in")
	errWrongTenant    = errors.New("the provider's answer names an issuer that isn't its tenant's")
)

// tenantPlaceholder is how Microsoft's endpoint for every tenant writes its issuer:
// each ID token then names its own tenant's.
const tenantPlaceholder = "{tenantid}"

var errManyTenants = errors.New("that issuer signs in every tenant under one registration, so it pins nobody to yours: use your own tenant's issuer")

type idp struct {
	spec        idpSpec
	client      *http.Client
	provider    *oidc.Provider
	oauth       *oauth2.Config
	verifier    *oidc.IDTokenVerifier
	tenanted    string
	namesItself bool
}

func discover(ctx context.Context, client *http.Client, spec idpSpec) (*idp, error) {
	template, err := discoveredIssuer(ctx, client, spec.Issuer)
	if err != nil {
		return nil, err
	}
	// The key set keeps fetching through this context for as long as the provider is
	// used, so it mustn't be one request's.
	ctx = oidc.ClientContext(context.Background(), client)
	tenanted := ""
	if strings.Contains(template, tenantPlaceholder) {
		if !spec.ManyTenants {
			return nil, errManyTenants
		}
		// The template is accepted here, and every token's issuer is checked against it in finish.
		tenanted = template
		ctx = oidc.InsecureIssuerURLContext(ctx, template)
	}
	provider, err := oidc.NewProvider(ctx, spec.Issuer)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", spec.Issuer, err)
	}
	endpoint := provider.Endpoint()
	if spec.Plain {
		endpoint.AuthStyle = oauth2.AuthStyleInParams
	}
	p := &idp{spec: spec, client: client, provider: provider, tenanted: tenanted,
		oauth: &oauth2.Config{ClientID: spec.ClientID, ClientSecret: spec.Secret, RedirectURL: spec.Redirect,
			Endpoint: endpoint, Scopes: []string{oidc.ScopeOpenID, "email", "profile"}},
		verifier: provider.Verifier(&oidc.Config{ClientID: spec.ClientID, SkipIssuerCheck: tenanted != ""}),
	}
	var meta struct {
		NamesItself bool `json:"authorization_response_iss_parameter_supported"`
	}
	if err := provider.Claims(&meta); err != nil {
		return nil, fmt.Errorf("reading %s: %w", spec.Issuer, err)
	}
	p.namesItself = meta.NamesItself
	return p, nil
}

func discoveredIssuer(ctx context.Context, client *http.Client, issuer string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", issuer, err)
	}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", issuer, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("reading %s: its discovery document answered %s", issuer, res.Status)
	}
	var doc struct {
		Issuer string `json:"issuer"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&doc); err != nil {
		return "", fmt.Errorf("reading %s: its discovery document isn't JSON", issuer)
	}
	return doc.Issuer, nil
}

// attempt is one sign-in between leaving for the provider and coming back. State ties
// the answer to the browser that asked, Verifier is PKCE and Nonce ties the ID token
// to this attempt.
type attempt struct {
	State    string `json:"s"`
	Verifier string `json:"v"`
	Nonce    string `json:"n"`
	// Way is a social provider's key, or "sso" with the organisation in Org.
	Way  string `json:"w"`
	Org  int64  `json:"o,omitempty"`
	Next string `json:"x,omitempty"`
	// At is when it began. The signature outlives the browser's cookie, so the age is checked here.
	At int64 `json:"t"`
}

const (
	attemptCookie = "signin"
	attemptLife   = 10 * time.Minute
)

// processKey signs the attempt cookie on a server given no secret key. It lasts as long
// as the process, so a restart costs the sign-ins in flight.
var processKey = []byte(randomToken(32))

// signAttempt signs the attempt cookie, so a callback nobody started here can't make
// the server call a provider.
func (a *App) signAttempt(value string) string {
	if a.s.Keyring != nil {
		return a.s.Keyring.Sign(attempts, value)
	}
	return mac(processKey, value)
}

func (a *App) attemptSigned(value, signature string) bool {
	if a.s.Keyring != nil {
		return a.s.Keyring.Verify(attempts, value, signature)
	}
	return hmac.Equal([]byte(mac(processKey, value)), []byte(signature))
}

func (a *App) beginAttempt(w http.ResponseWriter, r *http.Request, p *idp, att attempt, options ...oauth2.AuthCodeOption) {
	att.State, att.Verifier, att.Nonce = randomToken(32), oauth2.GenerateVerifier(), randomToken(32)
	att.At = time.Now().Unix()
	if len(att.Next) > 512 {
		att.Next = ""
	}
	raw, _ := json.Marshal(att)
	value := base64.RawURLEncoding.EncodeToString(raw)
	// Lax, because the answer is a top-level navigation from the provider's site.
	http.SetCookie(w, &http.Cookie{Name: attemptCookie, Value: value + "." + a.signAttempt(value), Path: "/login",
		MaxAge: 600, HttpOnly: true, Secure: a.secure(r), SameSite: http.SameSiteLaxMode})
	if !p.spec.Plain {
		options = append(options, oauth2.S256ChallengeOption(att.Verifier), oidc.Nonce(att.Nonce))
	}
	http.Redirect(w, r, p.oauth.AuthCodeURL(att.State, options...), http.StatusSeeOther)
}

// resumeAttempt forgets the attempt as it reads it, so an answer is taken once.
func (a *App) resumeAttempt(w http.ResponseWriter, r *http.Request) (attempt, error) {
	http.SetCookie(w, &http.Cookie{Name: attemptCookie, Value: "", Path: "/login", MaxAge: -1, HttpOnly: true})
	var att attempt
	c, err := r.Cookie(attemptCookie)
	if err != nil {
		return att, errNotStartedHere
	}
	value, signature, _ := strings.Cut(c.Value, ".")
	if !a.attemptSigned(value, signature) {
		return att, errNotStartedHere
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || json.Unmarshal(raw, &att) != nil || att.State == "" || att.Way == "" {
		return att, errNotStartedHere
	}
	if age := time.Since(time.Unix(att.At, 0)); age < -time.Minute || age > attemptLife {
		return att, errNotStartedHere
	}
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(att.State)) != 1 {
		return att, errNotStartedHere
	}
	return att, nil
}

// looseBool reads true, "true" and "1": providers disagree on how a flag is written.
type looseBool bool

func (b *looseBool) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	*b = s == "true" || s == "1"
	return nil
}

// outsider is who a provider says has signed in.
type outsider struct {
	Issuer, Subject    string
	Email, Name        string
	Username, HostedBy string
	// Vouched is the provider saying the address is this person's own. Disowned is
	// it saying outright that it isn't known to be.
	Vouched, Disowned bool
}

type outsiderClaims struct {
	Email    string     `json:"email"`
	Verified *looseBool `json:"email_verified"`
	// Microsoft's email claim is whatever a tenant's admin typed; xms_edov is the only
	// one that says the domain's owner was checked.
	DomainOwner *looseBool `json:"xms_edov"`
	Name        string     `json:"name"`
	Username    string     `json:"preferred_username"`
	HostedBy    string     `json:"hd"`
	Tenant      string     `json:"tid"`
}

// finish checks the provider's answer to an attempt and swaps its code for who signed in.
func (p *idp) finish(ctx context.Context, att attempt, answer url.Values) (*outsider, error) {
	// RFC 9207: a provider that names itself in its answers must, and one naming another is refused.
	if iss, named := answer["iss"]; (p.namesItself && !named) || (named && iss[0] != p.spec.Issuer && !p.ofTemplate(iss[0])) {
		return nil, errWrongIssuer
	}
	if code := answer.Get("error"); code != "" {
		return nil, fmt.Errorf("the provider refused the sign-in (%.60s)", code)
	}
	ctx = oidc.ClientContext(ctx, p.client)
	var options []oauth2.AuthCodeOption
	if !p.spec.Plain {
		options = append(options, oauth2.VerifierOption(att.Verifier))
	}
	token, err := p.oauth.Exchange(ctx, answer.Get("code"), options...)
	if err != nil {
		return nil, fmt.Errorf("exchanging the code: %w", err)
	}
	raw, _ := token.Extra("id_token").(string)
	id, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("checking the ID token: %w", err)
	}
	if !p.spec.Plain && subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(att.Nonce)) != 1 {
		return nil, errWrongNonce
	}
	var c outsiderClaims
	if err := id.Claims(&c); err != nil {
		return nil, fmt.Errorf("reading the ID token: %w", err)
	}
	if p.tenanted != "" {
		if c.Tenant == "" || id.Issuer != strings.Replace(p.tenanted, tenantPlaceholder, c.Tenant, 1) {
			return nil, errWrongTenant
		}
	}
	if p.spec.Plain && (c.Email == "" || c.Verified == nil) {
		info, err := p.provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
		if err != nil {
			return nil, fmt.Errorf("asking the provider who signed in: %w", err)
		}
		if info.Subject != id.Subject {
			return nil, errors.New("the provider described somebody else")
		}
		if err := info.Claims(&c); err != nil {
			return nil, fmt.Errorf("reading who signed in: %w", err)
		}
	}
	who := &outsider{Issuer: id.Issuer, Subject: id.Subject, Email: strings.ToLower(strings.TrimSpace(c.Email)),
		Name: strings.TrimSpace(c.Name), Username: strings.ToLower(strings.TrimSpace(c.Username)), HostedBy: strings.ToLower(c.HostedBy)}
	said := c.Verified
	if p.tenanted != "" {
		said = c.DomainOwner
	}
	who.Vouched = said != nil && bool(*said)
	who.Disowned = c.Verified != nil && !bool(*c.Verified)
	return who, nil
}

func (p *idp) ofTemplate(issuer string) bool {
	if p.tenanted == "" {
		return false
	}
	prefix, suffix, _ := strings.Cut(p.tenanted, tenantPlaceholder)
	tenant, ok := strings.CutPrefix(issuer, prefix)
	if !ok {
		return false
	}
	tenant, ok = strings.CutSuffix(tenant, suffix)
	return ok && tenant != "" && !strings.ContainsAny(tenant, "/?#")
}

// idpCache discovers each provider once, the first time somebody signs in through it,
// so the server starts whether or not a provider can be reached.
type idpCache struct {
	mu sync.Mutex
	m  map[string]*idpEntry
}

// idpEntry is one provider being discovered, or done. Its lock makes those asking at
// once wait for one discovery, and a failure is remembered for retryAfter, so sign-in
// links nobody need be signed in to follow can't be made to hammer a provider.
type idpEntry struct {
	mu     sync.Mutex
	p      *idp
	err    error
	failed time.Time
}

const retryAfter = 30 * time.Second

// probe reads a provider without remembering it: for a connection not yet saved.
func (a *App) probe(ctx context.Context, spec idpSpec) (*idp, error) {
	spec.Redirect = a.callbackURL()
	client := a.outbound
	if client == nil {
		client = guardedClient
	}
	return discover(ctx, client, spec)
}

func (a *App) idp(ctx context.Context, spec idpSpec) (*idp, error) {
	key := hashToken(strings.Join([]string{spec.Issuer, spec.ClientID, spec.Secret, a.callbackURL()}, "\x00"))
	a.idps.mu.Lock()
	if a.idps.m == nil {
		a.idps.m = map[string]*idpEntry{}
	}
	e := a.idps.m[key]
	if e == nil {
		e = &idpEntry{}
		a.idps.m[key] = e
	}
	a.idps.mu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.p != nil {
		return e.p, nil
	}
	if e.err != nil && time.Since(e.failed) < retryAfter {
		return nil, e.err
	}
	e.p, e.err = a.probe(ctx, spec)
	e.failed = time.Now()
	return e.p, e.err
}

// notPublic is address space no sign-in provider lives in: this host, private
// networks, link-local (where cloud metadata services sit) and the reserved ranges.
var notPublic = func() []netip.Prefix {
	var out []netip.Prefix
	for _, p := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4",
		"240.0.0.0/4", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20"} {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}()

var publicV6 = netip.MustParsePrefix("2000::/3")

func publicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || (addr.Is6() && !publicV6.Contains(addr)) {
		return false
	}
	for _, p := range notPublic {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

// guardedClient reaches an issuer an organisation's owner typed in. The address is
// checked as the connection is made, so a name that resolves somewhere else the
// second time gets no further, and a redirect, being a second destination, isn't followed.
var guardedClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{Timeout: 5 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || !publicAddr(ap.Addr()) {
				return fmt.Errorf("%s isn't a public address", address)
			}
			return nil
		}}).DialContext,
		TLSHandshakeTimeout: 5 * time.Second,
		IdleConnTimeout:     90 * time.Second,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}
