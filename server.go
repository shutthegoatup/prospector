package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/crypto/bcrypt"
)

// dummyHash keeps a wrong email as slow as a wrong password.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("unused"), bcrypt.DefaultCost)

const sessionCookie = "session"

type App struct {
	s      *Store
	mu     sync.Mutex
	mcp    map[int64]*mcp.Server
	logins loginGuard
}

// Req is who is asking and in which organisation: a signed-in member, or an API key.
type Req struct {
	User   *User
	Org    *Org
	Member *Membership
	Orgs   []OrgMembership
	Key    *APIKey
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
	http.Error(w, err.Error(), code)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(v); err != nil {
		fail(w, fmt.Errorf("bad request body: %w", err), http.StatusBadRequest)
		return false
	}
	return true
}

func secure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func (a *App) sessionUser(r *http.Request) *User {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	u, err := a.s.SessionUser(c.Value)
	if err != nil {
		return nil
	}
	return u
}

// member resolves the signed-in user and, for /o/{org}/ routes, their membership.
func (a *App) member(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := a.sessionUser(r)
		if u == nil {
			if r.Method == http.MethodGet && r.Header.Get("X-Fragment") == "" {
				http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			} else {
				http.Error(w, "signed out", http.StatusUnauthorized)
			}
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
		q := &Req{User: u}
		q.Orgs, _ = a.s.OrgsFor(u.ID)
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
			w.Header().Set("WWW-Authenticate", `Bearer realm="prospector"`)
			http.Error(w, "an API key is needed: Authorization: Bearer pk_…", http.StatusUnauthorized)
			return
		}
		k, err := a.s.KeyFor(strings.TrimSpace(secret))
		if err != nil {
			http.Error(w, "unknown or revoked API key", http.StatusUnauthorized)
			return
		}
		org, err := a.s.OrgBySlug(r.PathValue("org"))
		if err != nil || org.ID != k.OrgID {
			http.Error(w, "this key belongs to another organisation", http.StatusForbidden)
			return
		}
		if scope != "" && !k.Has(scope) {
			http.Error(w, "this key lacks the "+scope+" scope", http.StatusForbidden)
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

// contentPolicy lets pages load only this app's own scripts, so text that slipped past
// escaping still couldn't run. Styles are looser: templ writes style attributes, and the
// fonts come from Google.
const contentPolicy = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"font-src https://fonts.gstatic.com; img-src 'self' data:; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	mux.Handle("GET /assets/", assetHandler())
	mux.HandleFunc("GET /login", a.loginPage)
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("GET /{$}", a.member(a.home))
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

	mux.HandleFunc("POST /o/{org}/ui/mark", a.member(a.uiMark))
	mux.HandleFunc("POST /o/{org}/ui/update", a.member(a.uiUpdate))
	mux.HandleFunc("POST /o/{org}/ui/company", a.member(a.uiCompany))
	mux.HandleFunc("POST /o/{org}/ui/pick", a.member(a.uiPick))
	mux.HandleFunc("POST /o/{org}/ui/add", a.member(a.uiAdd))
	mux.HandleFunc("POST /o/{org}/ui/voice", a.member(a.uiVoice))
	mux.HandleFunc("POST /o/{org}/ui/settings", a.owner(a.uiSettings))
	mux.HandleFunc("POST /o/{org}/ui/member", a.owner(a.uiMember))
	mux.HandleFunc("POST /o/{org}/ui/member/remove", a.owner(a.uiRemoveMember))
	mux.HandleFunc("POST /o/{org}/ui/keys", a.owner(a.uiKey))
	mux.HandleFunc("POST /o/{org}/ui/keys/revoke", a.owner(a.uiRevoke))

	a.apiRoutes(mux)

	sameOrigin := http.NewCrossOriginProtection()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A browser says where a request came from. One from another site is refused, which
		// also covers the sign-in and sign-out forms that the JSON rule below can't. Agents
		// and the CLI send neither header and carry a key, so they pass.
		if r.Header.Get("Authorization") == "" {
			if err := sameOrigin.Check(r); err != nil {
				http.Error(w, "this request came from another site", http.StatusForbidden)
				return
			}
		}
		// Signed-in browsers send cookies; a cross-site form can't send JSON without a
		// CORS preflight, which is never answered. Login and the API key routes are exempt.
		if r.Method == http.MethodPost && r.URL.Path != "/login" && r.URL.Path != "/logout" &&
			r.Header.Get("Authorization") == "" && !strings.HasPrefix(r.Header.Get("content-type"), "application/json") {
			http.Error(w, "POST bodies must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", contentPolicy)
		if secure(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		mux.ServeHTTP(w, r)
	})
}

func (a *App) loginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "text/html; charset=utf-8")
	LoginPage(r.URL.Query().Get("next"), "").Render(r.Context(), w)
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
		LoginPage(r.PostForm.Get("next"), problem).Render(r.Context(), w)
	}
	select {
	case hashSlots <- struct{}{}:
		defer func() { <-hashSlots }()
	case <-r.Context().Done():
		return
	}
	if wait := a.logins.begin(email); wait > 0 {
		seconds := int(wait.Seconds()) + 1
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		refuse(http.StatusTooManyRequests, "Too many attempts. Try again in "+plural(seconds, "second")+".")
		return
	}
	token, _, err := a.s.Login(email, r.PostForm.Get("password"))
	// A failure that isn't a wrong password (the database, say) doesn't count against anyone.
	if err == nil || errors.Is(err, errWrongLogin) {
		a.logins.record(email, err == nil)
	}
	if err != nil {
		refuse(http.StatusUnauthorized, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: secure(r),
		SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(sessionLife)})
	http.Redirect(w, r, safeNext(r.PostForm.Get("next")), http.StatusSeeOther)
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.s.Logout(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	if len(q.Orgs) > 0 {
		http.Redirect(w, r, "/o/"+q.Orgs[0].Slug+"/today", http.StatusSeeOther)
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
	if wait := a.logins.begin(u.Email); wait > 0 {
		seconds := int(wait.Seconds()) + 1
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		http.Error(w, "too many attempts: try again in "+plural(seconds, "second"), http.StatusTooManyRequests)
		return
	}
	err := a.s.ChangePassword(u, in.Current, in.New, keep)
	if err == nil || errors.Is(err, errWrongCurrent) {
		a.logins.record(u.Email, err == nil)
	}
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
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
	a.s.db.Model(&User{}).Count(&users)
	if users == 0 {
		log.Print("no users yet: create one with `prospector user add <email> <name>`")
	}
	srv := &http.Server{Addr: addr, Handler: a.Routes(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	log.Printf("listening on http://%s", addr)
	return srv.ListenAndServe()
}
