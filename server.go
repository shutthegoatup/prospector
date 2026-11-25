package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/a-h/templ"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/crypto/bcrypt"
)

// dummyHash keeps a wrong email as slow as a wrong password.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("unused"), bcrypt.DefaultCost)

const sessionCookie = "session"

type App struct {
	s         *Store
	mu        sync.Mutex
	mcp       map[int64]*mcp.Server
	logins    loginGuard
	enquiries enquiryGuard
	// base is the address people reach the app at, when the operator has said: then
	// links and the cookie's Secure mark don't depend on what a proxy forwards.
	base string
	// frontOrg is the organisation the front page's form sends enquiries to; empty is no form.
	frontOrg string
	// mail is the app's own server, for password resets and invitations; nil is none.
	mail *MailServer
	// send and later stand in for SMTP and for a goroutine in tests.
	send  sendFunc
	later func(func())
	// sending counts the mail still on its way out, which stopping waits for.
	sending sync.WaitGroup
	// How long a request's body may take to arrive, and its answer to be written;
	// zero means bodyLimit and answerLimit.
	readLimit, writeLimit time.Duration

	social []socialWay
	idps   idpCache
	// outbound reaches sign-in providers, and txt reads a domain's DNS; nil means the
	// guarded client and the system resolver.
	outbound *http.Client
	txt      func(ctx context.Context, name string) ([]string, error)
	// agentClient reaches an organisation's model endpoint and tool server; nil means
	// one that refuses private addresses unless the operator allows them.
	agentClient *http.Client
	runs        agentRuns
	// sleep stands in for waiting in tests; nil is the clock.
	sleep func(ctx context.Context, d time.Duration) error
}

const (
	// bodyLimit lets the largest import through on a slow link and drops a client
	// that trickles a body in.
	bodyLimit = 5 * time.Minute
	// answerLimit drops a client that never reads its answer.
	answerLimit = 2 * time.Minute
)

// setDeadlines bounds this request alone. The server has no read or write timeout of
// its own, because those would also cut an agent's MCP stream, which stays open. A
// request with no body gets no read deadline: there is nothing to wait for, and on
// an idle connection an expired one would be taken for the client hanging up.
func (a *App) setDeadlines(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	read, write := a.readLimit, a.writeLimit
	if read == 0 {
		read = bodyLimit
	}
	if write == 0 {
		write = answerLimit
	}
	if r.ContentLength != 0 {
		rc.SetReadDeadline(time.Now().Add(read))
	}
	rc.SetWriteDeadline(time.Now().Add(write))
}

// streaming lifts the answer deadline for a route whose answer stays open. It sits
// behind the key check, so only a request that has shown a key is left unbounded.
func streaming(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.NewResponseController(w).SetWriteDeadline(time.Time{})
		next.ServeHTTP(w, r)
	}
}

// Req is who is asking and in which organisation: a signed-in member, or an API key.
type Req struct {
	User    *User
	Session *Session
	Org     *Org
	Member  *Membership
	Orgs    []OrgMembership
	Key     *APIKey
	// due is worked out once a request, for the page and the navigation both.
	due map[string][]Contact
}

type reqKey struct{}

func reqOf(r *http.Request) *Req { return r.Context().Value(reqKey{}).(*Req) }

// Actor is the name recorded against what this request does. A person is their full
// name, so two with the same first name stay distinct in the history, or their email
// when the name is missing or would read as an agent, the CLI or an import.
func (q *Req) Actor() string {
	if q.Key != nil {
		return "agent:" + q.Key.Name
	}
	name := strings.TrimSpace(q.User.Name)
	if name == "" || strings.HasPrefix(name, "agent:") || name == "cli" || name == "import" {
		return q.User.Email
	}
	return name
}

func (q *Req) Agent() Actor {
	return Actor{Name: q.Actor(), KeyID: q.Key.ID, ReadOnly: !q.Key.Has("research:write")}
}

func (q *Req) Voice() Voice {
	caller := q.Member.CallerName
	if caller == "" {
		caller = q.User.Name
	}
	return Voice{Caller: caller, Org: q.Org.Name, Pitch: q.Org.Settings.withDefaults().Pitch, Signature: q.Member.Signature}
}

func (q *Req) Owner() bool { return q.Member != nil && q.Member.Role == "owner" }

func (q *Req) Base() string { return "/o/" + q.Org.Slug + "/" }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("content-type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Print(err)
	}
}

func fail(w http.ResponseWriter, err error, code int) {
	code, text := telling("a browser's request", err, code)
	http.Error(w, text, code)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(v); err != nil {
		if hasKey(r) {
			writeProblem(w, http.StatusBadRequest, "bad request body: "+err.Error())
		} else {
			fail(w, fmt.Errorf("bad request body: %w", err), http.StatusBadRequest)
		}
		return false
	}
	// The answer's clock starts once the body is in, so a slow upload doesn't use it up.
	http.NewResponseController(w).SetWriteDeadline(time.Now().Add(answerLimit))
	return true
}

func (a *App) secure(r *http.Request) bool {
	return strings.HasPrefix(a.base, "https://") || r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// publicURL checks PROSPECTOR_URL: a scheme and host, nothing after.
func publicURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", fmt.Errorf("PROSPECTOR_URL should look like https://prospector.example.com, not %q", raw)
	}
	return u.Scheme + "://" + u.Host, nil
}

func (a *App) session(r *http.Request) (*Session, *User) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil, nil
	}
	sess, u, err := a.s.SessionFor(c.Value)
	if err != nil {
		return nil, nil
	}
	return sess, u
}

func (a *App) sessionUser(r *http.Request) *User {
	_, u := a.session(r)
	return u
}

func (a *App) setSession(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: a.secure(r),
		SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(sessionLife)})
}

// member resolves the signed-in user and, for /o/{org}/ routes, their membership.
func (a *App) member(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		signedOut := func() {
			if r.Method == http.MethodGet && r.Header.Get("X-Fragment") == "" {
				http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			} else {
				http.Error(w, "signed out", http.StatusUnauthorized)
			}
		}
		sess, u := a.session(r)
		if u == nil {
			signedOut()
			return
		}
		// Someone still on a password their inviter saw can only go and choose their own.
		if u.MustChangePassword && r.URL.Path != "/password" {
			if r.Method == http.MethodGet && r.Header.Get("X-Fragment") == "" {
				http.Redirect(w, r, "/password", http.StatusSeeOther)
			} else {
				http.Error(w, "choose your own password first", http.StatusForbidden)
			}
			return
		}
		q := &Req{User: u, Session: sess}
		orgs, _ := a.s.OrgsFor(u.ID)
		for _, o := range orgs {
			if sess.Opens(o.ID) {
				q.Orgs = append(q.Orgs, o)
			}
		}
		if slug := r.PathValue("org"); slug != "" {
			org, err := a.s.OrgBySlug(slug)
			var m *Membership
			if err == nil {
				m, err = a.s.Membership(org.ID, u.ID)
			}
			if err != nil {
				http.NotFound(w, r)
				return
			}
			// A session another organisation's provider began is no sign-in here.
			if !sess.Opens(org.ID) {
				signedOut()
				return
			}
			if a.ssoRequired(org, u, m, sess) {
				if r.Method == http.MethodGet && r.Header.Get("X-Fragment") == "" {
					http.Redirect(w, r, ssoPageURL(org, u.Email, r.URL.RequestURI()), http.StatusSeeOther)
				} else {
					http.Error(w, org.Name+" requires its single sign-on: reload the page to go through it", http.StatusForbidden)
				}
				return
			}
			q.Org, q.Member = org, m
		}
		next(w, r.WithContext(context.WithValue(r.Context(), reqKey{}, q)))
	}
}

func (a *App) owner(next http.HandlerFunc) http.HandlerFunc {
	return a.member(func(w http.ResponseWriter, r *http.Request) {
		if !reqOf(r).Owner() {
			http.Error(w, "only an owner of this organisation can change that", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

// agent authenticates an API key for the organisation in the URL and checks its scope.
func (a *App) agent(scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secret, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			challenge(w, http.StatusUnauthorized, "", "", "an API key is needed: Authorization: Bearer pk_…")
			return
		}
		k, err := a.s.KeyFor(strings.TrimSpace(secret))
		if err != nil {
			challenge(w, http.StatusUnauthorized, "invalid_token", "", "unknown or revoked API key")
			return
		}
		org, err := a.s.OrgBySlug(r.PathValue("org"))
		if err != nil || org.ID != k.OrgID {
			writeProblem(w, http.StatusForbidden, "this key belongs to another organisation")
			return
		}
		if scope != "" && !k.Has(scope) {
			challenge(w, http.StatusForbidden, "insufficient_scope", scope, "this key lacks the "+scope+" scope")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), reqKey{}, &Req{Org: org, Key: k})))
	}
}

func idParam(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n
}

// page renders a whole page, or only its content when the page script refreshes it.
func (a *App) page(w http.ResponseWriter, r *http.Request, active string, content templ.Component) {
	w.Header().Set("content-type", "text/html; charset=utf-8")
	view := content
	if r.Header.Get("X-Fragment") == "" {
		view = Layout(a.nav(reqOf(r), active), content)
	}
	if err := view.Render(r.Context(), w); err != nil {
		log.Print(err)
	}
}

// contentPolicy lets pages load only what this app serves, so text that slipped past
// escaping still couldn't run and nothing is fetched from anywhere else. Styles also
// allow inline attributes, which templ writes.
const contentPolicy = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"font-src 'self'; img-src 'self' data:; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	mux.Handle("GET /assets/", assetHandler())
	mux.HandleFunc("GET /login", a.loginPage)
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("GET /forgot", a.forgotPage)
	mux.HandleFunc("POST /forgot", a.forgot)
	mux.HandleFunc("GET /reset/{token}", a.resetPage)
	mux.HandleFunc("POST /reset/{token}", a.reset)
	mux.HandleFunc("GET /login/with/{way}", a.loginWith)
	mux.HandleFunc("GET /login/sso", a.ssoPage)
	mux.HandleFunc("GET /login/sso/start", a.ssoStart)
	mux.HandleFunc("GET /login/callback", a.loginCallback)
	mux.HandleFunc("GET /c", a.customerPage)
	mux.HandleFunc("POST /c/leave", a.customerLeave)
	mux.HandleFunc("GET /c/{token}", a.customerLinkPage)
	mux.HandleFunc("POST /c/{token}", a.customerOpen)
	mux.HandleFunc("POST /c/{token}/again", a.customerAgain)
	mux.HandleFunc("GET /{$}", a.front)
	mux.HandleFunc("GET /api", a.apiDocPage())
	mux.HandleFunc("POST /orgs", a.member(a.newOrg))
	mux.HandleFunc("GET /password", a.member(a.passwordPage))
	mux.HandleFunc("POST /password", a.member(a.changePassword))

	mux.HandleFunc("GET /o/{org}/{$}", a.member(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, reqOf(r).Base()+"today", http.StatusSeeOther)
	}))
	mux.HandleFunc("GET /o/{org}/today", a.member(a.today))
	mux.HandleFunc("GET /o/{org}/research", a.member(a.research))
	mux.HandleFunc("GET /o/{org}/conversations", a.member(a.conversations))
	mux.HandleFunc("GET /o/{org}/companies", a.member(a.companies))
	mux.HandleFunc("GET /o/{org}/companies/{id}", a.member(a.company))
	mux.HandleFunc("GET /o/{org}/add", a.member(a.addPage))
	mux.HandleFunc("GET /o/{org}/settings", a.member(a.settings))
	mux.HandleFunc("GET /o/{org}/agents", a.member(a.agents))
	mux.HandleFunc("GET /o/{org}/enquiries", a.member(a.enquiriesPage))
	mux.HandleFunc("GET /o/{org}/subscriptions", a.member(a.subscriptionsPage))
	mux.HandleFunc("GET /o/{org}/onboarding", a.member(a.onboardingPage))
	a.taskRoutes(mux)
	mux.HandleFunc("OPTIONS "+intakePattern, a.intakePreflight)
	mux.HandleFunc("POST "+intakePattern, a.intake)

	mux.HandleFunc("POST /o/{org}/ui/mark", a.member(a.uiMark))
	mux.HandleFunc("POST /o/{org}/ui/update", a.member(a.uiUpdate))
	mux.HandleFunc("POST /o/{org}/ui/deal", a.member(a.uiDeal))
	mux.HandleFunc("POST /o/{org}/ui/deal/start", a.member(a.uiDealStart))
	mux.HandleFunc("POST /o/{org}/ui/deal/remove", a.member(a.uiDealRemove))
	mux.HandleFunc("POST /o/{org}/ui/subscription", a.member(a.uiSubscription))
	mux.HandleFunc("POST /o/{org}/ui/subscription/change", a.member(a.uiSubscriptionChange))
	mux.HandleFunc("POST /o/{org}/ui/subscription/remove", a.member(a.uiSubscriptionRemove))
	mux.HandleFunc("POST /o/{org}/ui/subscription/renew", a.member(a.uiSubscriptionRenew))
	mux.HandleFunc("POST /o/{org}/ui/onboarding", a.member(a.uiOnboardingStart))
	mux.HandleFunc("POST /o/{org}/ui/onboarding/step", a.member(a.uiOnboardingStep))
	mux.HandleFunc("POST /o/{org}/ui/onboarding/remove", a.member(a.uiOnboardingRemove))
	mux.HandleFunc("POST /o/{org}/ui/onboarding/invite", a.member(a.uiCustomerInvite))
	mux.HandleFunc("POST /o/{org}/ui/onboarding/revoke", a.member(a.uiCustomerRevoke))
	mux.HandleFunc("POST /o/{org}/ui/company", a.member(a.uiCompany))
	mux.HandleFunc("POST /o/{org}/ui/pick", a.member(a.uiPick))
	mux.HandleFunc("POST /o/{org}/ui/suggestion", a.member(a.uiSuggestion))
	mux.HandleFunc("POST /o/{org}/ui/add", a.member(a.uiAdd))
	mux.HandleFunc("POST /o/{org}/ui/voice", a.member(a.uiVoice))
	mux.HandleFunc("POST /o/{org}/ui/settings", a.owner(a.uiSettings))
	mux.HandleFunc("POST /o/{org}/ui/member", a.owner(a.uiMember))
	mux.HandleFunc("POST /o/{org}/ui/member/remove", a.owner(a.uiRemoveMember))
	mux.HandleFunc("POST /o/{org}/ui/mail", a.owner(a.uiMail))
	mux.HandleFunc("POST /o/{org}/ui/mail/test", a.owner(a.uiMailTest))
	mux.HandleFunc("POST /o/{org}/ui/sso", a.owner(a.uiSSO))
	mux.HandleFunc("POST /o/{org}/ui/sso/domain", a.owner(a.uiSSODomain))
	mux.HandleFunc("POST /o/{org}/ui/sso/domain/remove", a.owner(a.uiSSODomainRemove))
	mux.HandleFunc("POST /o/{org}/ui/sso/verify", a.owner(a.uiSSOVerify))
	mux.HandleFunc("POST /o/{org}/ui/export", a.owner(a.uiExport))
	mux.HandleFunc("POST /o/{org}/ui/export-org", a.owner(a.uiExportOrg))
	mux.HandleFunc("POST /o/{org}/ui/erase", a.owner(a.uiErase))
	mux.HandleFunc("POST /o/{org}/ui/enquiry/delete", a.owner(a.uiDeleteEnquiry))
	mux.HandleFunc("POST /o/{org}/ui/enquiry/update", a.member(a.uiSetEnquiry))
	mux.HandleFunc("POST /o/{org}/ui/enquiry/converse", a.member(a.uiConverseEnquiry))
	mux.HandleFunc("POST /o/{org}/ui/keys", a.owner(a.uiKey))
	mux.HandleFunc("POST /o/{org}/ui/keys/revoke", a.owner(a.uiRevoke))

	a.apiRoutes(mux)
	a.routeAPI(mux)

	sameOrigin := http.NewCrossOriginProtection()
	// Enquiry forms live on other sites. The route checks the origin against the
	// organisation's own list, and reads no session.
	sameOrigin.AddInsecureBypassPattern("POST " + intakePattern)
	if a.base != "" {
		// Behind a proxy that rewrites Host, an older browser's Origin wouldn't match it.
		if err := sameOrigin.AddTrustedOrigin(a.base); err != nil {
			log.Print(err)
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.setDeadlines(w, r)
		if wantsPage(r) {
			page := &problemPage{ResponseWriter: w, r: r}
			defer page.finish()
			w = page
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", contentPolicy)
		// Pages and answers hold people's details: a browser shouldn't keep them for the
		// back button after signing out, nor anything in between. Assets set their own.
		w.Header().Set("Cache-Control", "no-store")
		if a.secure(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		// A browser says where a request came from. One from another site is refused, which
		// also covers the sign-in and sign-out forms that the JSON rule below can't. Agents
		// and the CLI send neither header and carry a key, so they pass.
		if !hasKey(r) {
			if err := sameOrigin.Check(r); err != nil {
				http.Error(w, "this request came from another site", http.StatusForbidden)
				return
			}
		}
		// Signed-in browsers send cookies; a cross-site form can't send JSON without a
		// CORS preflight, which only the enquiry route answers, and only for the sites an
		// organisation lists. The signed-out forms and the API key routes are exempt.
		if r.Method == http.MethodPost && !signedOutForm(r.URL.Path) &&
			!hasKey(r) && !strings.HasPrefix(r.Header.Get("content-type"), "application/json") {
			http.Error(w, "POST bodies must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// signedOutForm is a plain HTML form somebody with no session posts.
func signedOutForm(path string) bool {
	return path == "/login" || path == "/logout" || path == "/forgot" || strings.HasPrefix(path, "/reset/") || strings.HasPrefix(path, "/c/")
}

func (a *App) loginPage(w http.ResponseWriter, r *http.Request) {
	v := a.loginView(r.URL.Query().Get("next"), "")
	if r.URL.Query().Has("reset") {
		v.Notice = "Password saved. Sign in with it."
	}
	w.Header().Set("content-type", "text/html; charset=utf-8")
	LoginPage(v).Render(r.Context(), w)
}

// safeNext keeps a post-login redirect on this site. Browsers drop tabs and newlines
// from a URL and read \ as /, so either could turn a path into //another.host.
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") ||
		strings.ContainsFunc(next, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\\' }) {
		return "/"
	}
	return next
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	email := r.PostForm.Get("email")
	refuse := func(code int, problem string) {
		w.Header().Set("content-type", "text/html; charset=utf-8")
		w.WriteHeader(code)
		LoginPage(a.loginView(r.PostForm.Get("next"), problem)).Render(r.Context(), w)
	}
	select {
	case hashSlots <- struct{}{}:
		defer func() { <-hashSlots }()
	case <-r.Context().Done():
		return
	}
	// A browser that has signed in to this account before is counted by itself.
	who, known := signInBucket(email), false
	if c, err := r.Cookie(deviceCookie); err == nil {
		if device, ok := a.s.knownDevice(email, c.Value); ok {
			who, known = device, true
		}
	}
	if wait := a.logins.begin(who); wait > 0 {
		seconds := int(wait.Seconds()) + 1
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		refuse(http.StatusTooManyRequests, "Too many attempts. Try again in "+plural(seconds, "second")+".")
		return
	}
	token, u, err := a.s.Login(email, r.PostForm.Get("password"))
	// A failure that isn't a wrong password (the database, say) doesn't count against anyone.
	if err == nil || errors.Is(err, errWrongLogin) {
		a.logins.record(who, err == nil)
	}
	if err != nil {
		refuse(telling("sign-in", err, http.StatusUnauthorized))
		return
	}
	if !known {
		http.SetCookie(w, &http.Cookie{Name: deviceCookie, Value: newDeviceToken(u), Path: "/login", HttpOnly: true, Secure: a.secure(r),
			SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(deviceLife)})
	}
	a.setSession(w, r, token)
	http.Redirect(w, r, safeNext(r.PostForm.Get("next")), http.StatusSeeOther)
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.s.Logout(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// front is the one page a signed-out visitor sees besides the sign-in form.
func (a *App) front(w http.ResponseWriter, r *http.Request) {
	if a.sessionUser(r) != nil {
		a.member(a.home)(w, r)
		return
	}
	w.Header().Set("content-type", "text/html; charset=utf-8")
	if err := LandingPage(a.frontIntake(r)).Render(r.Context(), w); err != nil {
		log.Print(err)
	}
}

// frontIntake is where the front page's form posts, and the origin a browser must be
// on for it. The intake route answers only the sites an organisation lists, so until
// this address is among them there is no form.
func (a *App) frontIntake(r *http.Request) (intake, origin string) {
	if a.frontOrg == "" {
		return "", ""
	}
	origin = browserOrigin(a.baseURL(r))
	org, err := a.s.OrgBySlug(a.frontOrg)
	if err != nil || !org.Settings.takesEnquiriesFrom(origin) {
		return "", ""
	}
	return "/o/" + org.Slug + "/intake", origin
}

// browserOrigin is an address as a browser names it in Origin: lower case, and
// without the port when it is the scheme's own.
func browserOrigin(address string) string {
	u, err := url.Parse(address)
	if err != nil {
		return address
	}
	host := u.Host
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		host = strings.TrimSuffix(host, ":"+u.Port())
	}
	return strings.ToLower(u.Scheme + "://" + host)
}

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	if len(q.Orgs) > 0 {
		http.Redirect(w, r, "/o/"+q.Orgs[0].Slug+"/today", http.StatusSeeOther)
		return
	}
	// A session an organisation's provider began, whose day has run out, opens nothing.
	if q.Session.Limited {
		http.Redirect(w, r, "/login/sso?"+url.Values{"email": {q.User.Email}}.Encode(), http.StatusSeeOther)
		return
	}
	w.Header().Set("content-type", "text/html; charset=utf-8")
	NoOrgs(q.User).Render(r.Context(), w)
}

// passwordPage is where a new member lands until they have chosen a password;
// everyone else changes theirs in Settings.
func (a *App) passwordPage(w http.ResponseWriter, r *http.Request) {
	u := reqOf(r).User
	if !u.MustChangePassword {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	w.Header().Set("content-type", "text/html; charset=utf-8")
	ChoosePassword(u).Render(r.Context(), w)
}

func (a *App) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &in) {
		return
	}
	keep := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		keep = c.Value
	}
	// Guessing the current password from a borrowed session is slowed like guessing at sign-in.
	u := reqOf(r).User
	select {
	case hashSlots <- struct{}{}:
		defer func() { <-hashSlots }()
	case <-r.Context().Done():
		return
	}
	// Counted apart from sign-in, so guesses made there by someone else don't hold this up.
	if wait := a.logins.begin(changeBucket(u)); wait > 0 {
		seconds := int(wait.Seconds()) + 1
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		http.Error(w, "too many attempts: try again in "+plural(seconds, "second"), http.StatusTooManyRequests)
		return
	}
	err := a.s.ChangePassword(u, in.Current, in.New, keep)
	if err == nil || errors.Is(err, errWrongCurrent) {
		a.logins.record(changeBucket(u), err == nil)
	}
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	// The old password vouched for this browser's device cookie; the new one does now.
	if changed, err := a.s.UserByEmail(u.Email); err == nil {
		http.SetCookie(w, &http.Cookie{Name: deviceCookie, Value: newDeviceToken(changed), Path: "/login", HttpOnly: true, Secure: a.secure(r),
			SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(deviceLife)})
	}
	if u.MustChangePassword {
		writeJSON(w, map[string]string{"go": "/"})
		return
	}
	writeJSON(w, map[string]string{"message": "Password changed. Your other sessions are signed out."})
}

func (a *App) newOrg(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name string }
	if !decode(w, r, &in) {
		return
	}
	q := reqOf(r)
	if q.Session.Limited {
		fail(w, errors.New("you're signed in through one organisation's single sign-on, which opens only that organisation: sign in with a password to start another"), http.StatusForbidden)
		return
	}
	o, err := a.s.CreateOrgWithOwner("", in.Name, q.User)
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"go": "/o/" + o.Slug + "/settings"})
}

// serve starts even with no users: every page needs a sign-in, so nobody gets in
// until `prospector user add` has run against the same database.
func serve(a *App, addr string) error {
	var users int64
	logged(a.s.db.Model(&User{}).Count(&users))
	if users == 0 {
		log.Print("no users yet: create one with `prospector user add <email> <name>`")
	}
	if a.base == "" && len(a.social) > 0 {
		return errors.New("signing in with Google, Microsoft or LinkedIn needs PROSPECTOR_URL: it is where the provider sends people back")
	}
	if a.base == "" && !strings.HasPrefix(addr, "127.0.0.1:") && !strings.HasPrefix(addr, "localhost:") {
		log.Print("PROSPECTOR_URL isn't set: the session cookie is marked Secure only when a proxy sends X-Forwarded-Proto: https")
	}
	if a.mail != nil && a.base == "" {
		log.Print("PROSPECTOR_URL isn't set: mail is off, because an emailed link needs the address to point at")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	// Runs a stopped server left going are carried on, here or by another server.
	go a.keepRunsGoing(context.Background(), 30*time.Second)
	log.Printf("listening on http://%s", addr)
	// A release is installed by stopping this process, so every merge sends one of these.
	asked, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// Asked once, it stops listening for signals, so asking again stops it at once.
	context.AfterFunc(asked, stop)
	return a.serveUntil(asked, ln, stopWithin)
}

// stopWithin is how long requests in hand and mail on its way out are given to
// finish. An agent's MCP stream never finishes by itself, so it is cut when this runs out.
const stopWithin = 10 * time.Second

// serveUntil answers requests until asked is done, then takes no new ones and lets
// those in hand finish.
func (a *App) serveUntil(asked context.Context, ln net.Listener, within time.Duration) error {
	srv := &http.Server{Handler: a.Routes(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	failed := make(chan error, 1)
	go func() { failed <- srv.Serve(ln) }()
	select {
	case err := <-failed:
		return err
	case <-asked.Done():
	}
	log.Print("stopping: finishing the requests in hand")
	grace, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	if err := srv.Shutdown(grace); err != nil {
		srv.Close()
	}
	sent := make(chan struct{})
	go func() { a.sending.Wait(); close(sent) }()
	select {
	case <-sent:
	case <-grace.Done():
		log.Print("stopped with mail still on its way out")
	}
	return nil
}
