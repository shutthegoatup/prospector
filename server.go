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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/shutthegoatup/prospector/api"
	"golang.org/x/crypto/bcrypt"
)

// dummyHash keeps a wrong email as slow as a wrong password.
// passwordCost is how hard a password hash is to compute; the tests lower it.
var passwordCost = bcrypt.DefaultCost

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("unused"), passwordCost)

const sessionCookie = "session"

type App struct {
	s         *Store
	mu        sync.Mutex
	mcp       map[int64]builtMCP
	logins    loginGuard
	enquiries enquiryGuard
	rates     rateGuard
	// base is the address people reach the app at, when the operator has said: then
	// links and the cookie's Secure mark don't depend on what a proxy forwards.
	base string
	// frontOrg is the organisation the front page's form sends enquiries to; empty is no form.
	frontOrg string
	// securityContact is security.txt's Contact; empty serves no security.txt.
	securityContact string
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
	// vendorClient and vendorBase stand in for enrichment vendors in tests: a client,
	// and an address every vendor is reached at in place of its own.
	vendorClient *http.Client
	vendorBase   string
	// pageClient reads the pages an agent asks for; tests stand in their own.
	pageClient *http.Client
	runs       agentRuns
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
	if name == "" || strings.HasPrefix(name, "agent:") || name == "cli" || name == "import" || name == customerActor {
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
	http.SetCookie(w, a.sessionCookie(r, token))
}

func (a *App) sessionCookie(r *http.Request, token string) *http.Cookie {
	return &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: a.secure(r),
		SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(sessionLife)}
}

// member resolves the signed-in user and, for /o/{org}/ routes, their membership.
func (a *App) member(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// A page is sent where it can go on; an API caller is told, with a type to act on.
		refuse := func(status int, kind api.ProblemType, text, to string) {
			switch {
			case forAPI(r):
				writeTyped(w, status, kind, text)
			case r.Method == http.MethodGet:
				http.Redirect(w, r, to, http.StatusSeeOther)
			default:
				http.Error(w, text, status)
			}
		}
		signedOut := func(kind api.ProblemType) {
			refuse(http.StatusUnauthorized, kind, "signed out", "/login?next="+url.QueryEscape(r.URL.RequestURI()))
		}
		sess, u := a.session(r)
		if u == nil {
			signedOut(api.ProblemTypeSignedOut)
			return
		}
		// Someone still on a password their inviter saw can only go and choose their own.
		if u.MustChangePassword && r.URL.Path != "/password" {
			refuse(http.StatusForbidden, api.ProblemTypeMustChangePassword, "choose your own password first", "/password")
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
				refuseAs(w, r, http.StatusNotFound, "no organisation "+slug+" of yours")
				return
			}
			// A session another organisation's provider began is no sign-in here.
			if !sess.Opens(org.ID) {
				signedOut(api.ProblemTypeSessionLimited)
				return
			}
			if a.ssoRequired(org, u, m, sess) {
				refuse(http.StatusForbidden, api.ProblemTypeSsoRequired, org.Name+" requires its single sign-on: reload the page to go through it",
					ssoPageURL(org, u.Email, r.URL.RequestURI()))
				return
			}
			q.Org, q.Member = org, m
		}
		if forAPI(r) && !a.limited(w, r, userBucket(u), memberRate) {
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), reqKey{}, q)))
	}
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
		if !a.limited(w, r, keyBucket(k), keyRate) {
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), reqKey{}, &Req{Org: org, Key: k})))
	}
}

// contentPolicy lets pages load only what this app serves, so text that slipped past
// escaping still couldn't run and nothing is fetched from anywhere else. Styles also
// allow inline attributes, which templ writes.
const contentPolicy = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"font-src 'self'; img-src 'self' data:; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

// pdfPolicy is the policy where pdf.js draws a document: it decodes JPEG 2000 images
// with WebAssembly, which compiling needs allowed. Evaluating script still isn't.
var pdfPolicy = strings.Replace(contentPolicy, "script-src 'self';", "script-src 'self' 'wasm-unsafe-eval';", 1)

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	mux.Handle("GET /assets/", assetHandler())
	a.signInRoutes(mux)
	mux.HandleFunc("GET /login/with/{way}", a.loginWith)
	mux.HandleFunc("GET /login/sso/start", a.ssoStart)
	mux.HandleFunc("GET /login/callback", a.loginCallback)
	mux.HandleFunc("GET /u/{token}", a.unsubscribePage)
	mux.HandleFunc("POST /u/{token}", a.unsubscribe)
	mux.HandleFunc("POST /hooks/postmark", a.postmarkHook)
	mux.HandleFunc("GET /c", a.customerPage)
	mux.HandleFunc("POST /c/leave", a.customerLeave)
	mux.HandleFunc("POST /c/steps/{id}/done", a.customerStep(true))
	mux.HandleFunc("POST /c/steps/{id}/undone", a.customerStep(false))
	mux.HandleFunc("GET /c/{token}", a.customerLinkPage)
	mux.HandleFunc("POST /c/{token}", a.customerOpen)
	mux.HandleFunc("POST /c/{token}/again", a.customerAgain)
	mux.HandleFunc("GET /{$}", a.front)
	mux.HandleFunc("GET /api", a.apiDocPage)
	mux.HandleFunc("GET /api/problems/{name}", a.apiProblem())

	mux.HandleFunc("GET /o/{org}/{$}", a.member(func(w http.ResponseWriter, r *http.Request) {
		q := reqOf(r)
		http.Redirect(w, r, q.Base()+landing(q.Member), http.StatusSeeOther)
	}))
	a.appRoutes(mux)
	mux.HandleFunc("OPTIONS "+intakePattern, a.intakePreflight)
	mux.HandleFunc("POST "+intakePattern, a.intake)

	a.apiRoutes(mux)
	a.routeAPI(mux)
	a.wellKnownRoutes(mux)

	sameOrigin := http.NewCrossOriginProtection()
	// Enquiry forms live on other sites. The route checks the origin against the
	// organisation's own list, and reads no session.
	sameOrigin.AddInsecureBypassPattern("POST " + intakePattern)
	// A mail provider asks to unsubscribe from its own servers, and a webmail page may
	// ask from its own origin. The link's secret is what gives the right, and all it
	// can do is stop mail.
	sameOrigin.AddInsecureBypassPattern("POST /u/{token}")
	if a.base != "" {
		// Behind a proxy that rewrites Host, an older browser's Origin wouldn't match it.
		if err := sameOrigin.AddTrustedOrigin(a.base); err != nil {
			log.Print(err)
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.setDeadlines(w, r)
		if !cleanRequest(r) {
			refuseAs(w, r, http.StatusNotFound, "404 page not found")
			return
		}
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
				refuseAs(w, r, http.StatusForbidden, "this request came from another site")
				return
			}
		}
		// Signed-in browsers send cookies; a cross-site form can't send JSON without a
		// CORS preflight, which only the enquiry route answers, and only for the sites an
		// organisation lists. The signed-out forms and the API key routes are exempt.
		if r.Method == http.MethodPost && !signedOutForm(r.URL.Path) &&
			!hasKey(r) && !strings.HasPrefix(r.Header.Get("content-type"), "application/json") {
			refuseAs(w, r, http.StatusUnsupportedMediaType, "POST bodies must be application/json")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// signedOutForm is a plain HTML form somebody with no session posts.
func signedOutForm(path string) bool {
	return strings.HasPrefix(path, "/c/") || strings.HasPrefix(path, "/u/")
}

// signInRoutes serve the React app's sign-in pages, each only where it can work.
func (a *App) signInRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /login", a.shell)
	mux.HandleFunc("GET /forgot", func(w http.ResponseWriter, r *http.Request) {
		if !a.mailOn() {
			http.NotFound(w, r)
			return
		}
		a.shell(w, r)
	})
	mux.HandleFunc("GET /reset/{token}", func(w http.ResponseWriter, r *http.Request) {
		if _, err := a.s.PasswordTokenUser(r.PathValue("token")); err != nil {
			fail(w, errBadLink, http.StatusGone)
			return
		}
		a.shell(w, r)
	})
	mux.HandleFunc("GET /login/sso", func(w http.ResponseWriter, r *http.Request) {
		if a.base == "" {
			http.NotFound(w, r)
			return
		}
		a.shell(w, r)
	})
	// Where a new member lands until they have chosen a password; everyone else changes
	// theirs in Settings.
	mux.HandleFunc("GET /password", a.member(func(w http.ResponseWriter, r *http.Request) {
		if !reqOf(r).User.MustChangePassword {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		a.shell(w, r)
	}))
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

// passwordSignIn checks a password. wait is the seconds to hold off after too many
// attempts; device is set when this browser is new to the account, so its own attempts
// are counted apart from a stranger's.
func (a *App) passwordSignIn(r *http.Request, email, password string) (token string, u *User, device *http.Cookie, wait int, err error) {
	select {
	case hashSlots <- struct{}{}:
		defer func() { <-hashSlots }()
	case <-r.Context().Done():
		return "", nil, nil, 0, r.Context().Err()
	}
	who, known := signInBucket(email), false
	// A browser can hold two: one from when the cookie was kept to /login, which a
	// password change since has made worthless, and the one that took its place.
	for _, c := range r.CookiesNamed(deviceCookie) {
		if device, ok := a.s.knownDevice(email, c.Value); ok {
			who, known = device, true
			break
		}
	}
	if held := a.logins.begin(who); held > 0 {
		return "", nil, nil, int(held.Seconds()) + 1, nil
	}
	token, u, err = a.s.Login(email, password)
	// A failure that isn't a wrong password (the database, say) doesn't count against anyone.
	if err == nil || errors.Is(err, errWrongLogin) {
		a.logins.record(who, err == nil)
	}
	if err != nil {
		return "", nil, nil, 0, err
	}
	if !known {
		device = a.deviceCookie(r, u)
	}
	return token, u, device, 0, nil
}

// deviceCookie vouches for this browser to u's account.
func (a *App) deviceCookie(r *http.Request, u *User) *http.Cookie {
	return &http.Cookie{Name: deviceCookie, Value: newDeviceToken(u), Path: "/", HttpOnly: true, Secure: a.secure(r),
		SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(deviceLife)}
}

func (a *App) endSession(r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := a.s.Logout(c.Value); err != nil {
			log.Print("signing out: ", err)
		}
	}
}

func signedOutCookie() *http.Cookie {
	return &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true}
}

// front is the one page a signed-out visitor sees besides the sign-in form.
func (a *App) front(w http.ResponseWriter, r *http.Request) {
	if a.sessionUser(r) != nil {
		a.member(a.home)(w, r)
		return
	}
	a.frontPage(w, r)
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
		m, _ := a.s.Membership(q.Orgs[0].ID, q.User.ID)
		http.Redirect(w, r, "/o/"+q.Orgs[0].Slug+"/"+landing(m), http.StatusSeeOther)
		return
	}
	// A session an organisation's provider began, whose day has run out, opens nothing.
	if q.Session.Limited {
		http.Redirect(w, r, "/login/sso?"+url.Values{"email": {q.User.Email}}.Encode(), http.StatusSeeOther)
		return
	}
	writeShell(w, http.StatusOK, map[string]string{"page": "start"}, "Start an organisation at /api/v1/orgs.\n")
}

const passwordChanged = "Password changed. Your other sessions are signed out."

// passwordChange changes u's password, keeping the session r came with, and gives the
// device cookie the new password vouches for. wait is the seconds to hold off when there
// have been too many attempts.
func (a *App) passwordChange(r *http.Request, u *User, current, next string) (device *http.Cookie, wait int, err error) {
	keep := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		keep = c.Value
	}
	// Guessing the current password from a borrowed session is slowed like guessing at sign-in.
	select {
	case hashSlots <- struct{}{}:
		defer func() { <-hashSlots }()
	case <-r.Context().Done():
		return nil, 0, r.Context().Err()
	}
	// Counted apart from sign-in, so guesses made there by someone else don't hold this up.
	if held := a.logins.begin(changeBucket(u)); held > 0 {
		return nil, int(held.Seconds()) + 1, nil
	}
	err = a.s.ChangePassword(u, current, next, keep)
	if err == nil || errors.Is(err, errWrongCurrent) {
		a.logins.record(changeBucket(u), err == nil)
	}
	if err != nil {
		return nil, 0, err
	}
	// The old password vouched for this browser's device cookie; the new one does now.
	if changed, err := a.s.UserByEmail(u.Email); err == nil {
		device = a.deviceCookie(r, changed)
	}
	return device, 0, nil
}

// serve starts even with no users: every page needs a sign-in, so nobody gets in
// until `prospector user add` has run against the same database.
func serve(a *App, addr string) error {
	if users, err := a.s.Users(); err == nil && users == 0 {
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
	// Campaigns being sent go on being sent, here or by another server.
	go a.keepSending(context.Background(), sendEvery)
	go a.keepCheckingMail(context.Background(), 10*time.Minute)
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
