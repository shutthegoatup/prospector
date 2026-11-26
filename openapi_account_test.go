package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
)

// asBrowser calls the API as the React app does: JSON, with whatever cookies it holds.
func asBrowser(t *testing.T, c *http.Client, method, url string, body any, header ...string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		r = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, url, r)
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func problemOf(t *testing.T, res *http.Response) api.Problem {
	t.Helper()
	var p api.Problem
	if err := json.NewDecoder(res.Body).Decode(&p); err != nil {
		t.Fatalf("%s: not a problem: %v", res.Status, err)
	}
	return p
}

func freshBrowser() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func TestSigningInAndOutThroughTheAPI(t *testing.T) {
	s, o, srv, _ := signedIn(t)
	b := freshBrowser()
	session := srv.URL + "/api/v1/session"

	if res := asBrowser(t, b, "GET", session, nil); res.StatusCode != http.StatusUnauthorized || problemOf(t, res).Code != api.SignedOut {
		t.Fatalf("asking who is signed in, before signing in: %s", res.Status)
	}
	for _, wrong := range []api.SignIn{{Email: "ada@example.com", Password: "wrong"}, {Email: "nobody@example.com", Password: "wrong"}} {
		res := asBrowser(t, b, "POST", session, wrong)
		if p := problemOf(t, res); res.StatusCode != http.StatusUnauthorized || p.Code != api.WrongLogin || p.Detail != errWrongLogin.Error() {
			t.Fatalf("signing in as %s with the wrong password: %s %+v", wrong.Email, res.Status, p)
		}
	}

	res := asBrowser(t, b, "POST", session, api.SignIn{Email: "ada@example.com", Password: "correct horse battery"})
	var in api.Session
	if json.NewDecoder(res.Body).Decode(&in); res.StatusCode != http.StatusOK || in.User.Email != "ada@example.com" || in.MustChangePassword || in.Limited ||
		len(in.Orgs) != 1 || in.Orgs[0].Slug != "alpha" || in.Orgs[0].Role != api.SessionOrgRoleOwner || in.Orgs[0].SsoRequired {
		t.Fatalf("signing in: %s %+v", res.Status, in)
	}
	var sessionSet, deviceSet bool
	for _, c := range res.Cookies() {
		switch c.Name {
		case sessionCookie:
			sessionSet = c.HttpOnly && c.Path == "/"
		case deviceCookie:
			deviceSet = c.HttpOnly && c.Path == "/"
		}
	}
	if !sessionSet || !deviceSet {
		t.Fatalf("signing in should set the session and device cookies, HttpOnly, for every path: %v", res.Cookies())
	}
	if res := asBrowser(t, b, "GET", session, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("asking who is signed in: %s", res.Status)
	}
	if res := asBrowser(t, b, "GET", srv.URL+"/o/alpha/api/v1/settings", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("the API session should open the organisation's API: %s", res.Status)
	}

	if res := asBrowser(t, b, "DELETE", session, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("signing out: %s", res.Status)
	}
	if res := asBrowser(t, b, "GET", srv.URL+"/o/alpha/api/v1/settings", nil); res.StatusCode != http.StatusUnauthorized || problemOf(t, res).Code != api.SignedOut {
		t.Fatalf("the organisation's API after signing out: %s", res.Status)
	}
	if res := asBrowser(t, b, "DELETE", session, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("signing out when signed out already: %s", res.Status)
	}

	secret, _, err := s.CreateKey(o.ID, "robot", []string{"read"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "POST", "DELETE"} {
		if code, body := asKey(t, method, session, secret, api.SignIn{Email: "ada@example.com", Password: "correct horse battery"}); code != http.StatusForbidden {
			t.Errorf("%s /api/v1/session with a key: %d %s", method, code, body)
		}
	}
}

// Another site can't sign a browser in to an account of its choosing.
func TestSigningInFromAnotherSiteIsRefused(t *testing.T) {
	_, _, srv, _ := signedIn(t)
	b := freshBrowser()
	res := asBrowser(t, b, "POST", srv.URL+"/api/v1/session", api.SignIn{Email: "ada@example.com", Password: "correct horse battery"},
		"Origin", "https://intruder.example", "Sec-Fetch-Site", "cross-site")
	if res.StatusCode != http.StatusForbidden || len(res.Cookies()) != 0 {
		t.Fatalf("a sign-in posted from another site: %s %v", res.Status, res.Cookies())
	}
}

func TestSigningInThroughTheAPIIsThrottled(t *testing.T) {
	_, _, srv, _ := signedIn(t)
	b := freshBrowser()
	var res *http.Response
	for range freeAttempts + 1 {
		res = asBrowser(t, b, "POST", srv.URL+"/api/v1/session", api.SignIn{Email: "ada@example.com", Password: "wrong"})
	}
	if p := problemOf(t, res); res.StatusCode != http.StatusTooManyRequests || p.Code != api.Throttled || res.Header.Get("Retry-After") == "" {
		t.Fatalf("one try too many: %s %+v %q", res.Status, p, res.Header.Get("Retry-After"))
	}
	// The password form counts the same tries.
	if code, _ := postForm(t, http.DefaultClient, srv.URL+"/login", map[string][]string{"email": {"ada@example.com"}, "password": {"correct horse battery"}}); code != http.StatusTooManyRequests {
		t.Fatalf("the form, after the API was throttled: %d", code)
	}
}

// A browser still holding the device cookie from when it was kept to /login sends that
// one first; once a password change has made it worthless, the newer one still counts.
func TestABrowserWithAnOldDeviceCookieStillCountsByItself(t *testing.T) {
	s, _, srv, _ := signedIn(t)
	ada := must(s.UserByEmail("ada@example.com"))
	stale := newDeviceToken(ada)
	if err := s.ChangePassword(ada, "correct horse battery", "a password of her own", ""); err != nil {
		t.Fatal(err)
	}
	fresh := newDeviceToken(must(s.UserByEmail("ada@example.com")))
	for range freeAttempts + 3 {
		postForm(t, http.DefaultClient, srv.URL+"/login", map[string][]string{"email": {"ada@example.com"}, "password": {"a wrong guess"}})
	}
	try := func(devices ...string) int {
		t.Helper()
		form := strings.NewReader("email=ada%40example.com&password=a+password+of+her+own")
		req, _ := http.NewRequest("POST", srv.URL+"/login", form)
		req.Header.Set("content-type", "application/x-www-form-urlencoded")
		for _, d := range devices {
			req.AddCookie(&http.Cookie{Name: deviceCookie, Value: d})
		}
		res, err := freshBrowser().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := try(stale); code != http.StatusTooManyRequests {
		t.Fatalf("the old cookie alone, after the password changed: %d", code)
	}
	if code := try(stale, fresh); code != http.StatusSeeOther {
		t.Fatalf("the old cookie sent first, then the new one: %d", code)
	}
}

// Someone on a password their inviter saw can ask who they are and choose their own,
// and the organisation's API says why it won't answer rather than redirecting.
func TestAFirstPasswordIsChangedThroughTheAPI(t *testing.T) {
	s, o, srv, _ := signedIn(t)
	grace := mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	if err := s.db.Model(grace).Update("must_change_password", true).Error; err != nil {
		t.Fatal(err)
	}
	b := freshBrowser()
	res := asBrowser(t, b, "POST", srv.URL+"/api/v1/session", api.SignIn{Email: "grace@example.com", Password: "correct horse battery"})
	var in api.Session
	if json.NewDecoder(res.Body).Decode(&in); res.StatusCode != http.StatusOK || !in.MustChangePassword {
		t.Fatalf("signing in on a first password: %s %+v", res.Status, in)
	}
	if res := asBrowser(t, b, "GET", srv.URL+"/api/v1/session", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("asking who is signed in, on a first password: %s", res.Status)
	}
	res = asBrowser(t, b, "GET", srv.URL+"/o/alpha/api/v1/settings", nil)
	if p := problemOf(t, res); res.StatusCode != http.StatusForbidden || p.Code != api.MustChangePassword {
		t.Fatalf("the organisation's API, on a first password: %s %+v", res.Status, p)
	}

	password := srv.URL + "/api/v1/password"
	if res := asBrowser(t, b, "PUT", password, map[string]string{"current": "not it", "new": "a password of my own"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("the wrong current password: %s", res.Status)
	}
	if res := asBrowser(t, b, "PUT", password, map[string]string{"current": "correct horse battery", "new": "a password of my own"}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("choosing a password: %s", res.Status)
	}
	if res := asBrowser(t, b, "GET", srv.URL+"/o/alpha/api/v1/settings", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("the organisation's API, once a password is chosen: %s", res.Status)
	}
	if res := asBrowser(t, freshBrowser(), "PUT", password, map[string]string{"current": "a password of my own", "new": "another one again"}); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("changing a password signed out: %s", res.Status)
	}
}

func TestResettingAPasswordThroughTheAPI(t *testing.T) {
	_, srv, ada, box := mailSite(t, nil)
	asked := srv.URL + "/api/v1/password-resets"
	for _, email := range []string{"nobody@example.com", "Ada@Example.com"} {
		if res := asBrowser(t, freshBrowser(), "POST", asked, map[string]string{"email": email}); res.StatusCode != http.StatusAccepted {
			t.Fatalf("asking for a link for %s: %s", email, res.Status)
		}
	}
	link := linkIn(t, box.take(t, 1)[0])
	token := strings.TrimPrefix(link, "/reset/")

	res := asBrowser(t, freshBrowser(), "GET", asked+"/"+token, nil)
	var whose struct{ Email string }
	if json.NewDecoder(res.Body).Decode(&whose); res.StatusCode != http.StatusOK || whose.Email != "ada@example.com" {
		t.Fatalf("whose password the link resets: %s %+v", res.Status, whose)
	}
	if res := asBrowser(t, freshBrowser(), "POST", asked+"/"+token, map[string]string{"password": "short"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("too short a password: %s", res.Status)
	}
	if res := asBrowser(t, freshBrowser(), "POST", asked+"/"+token, map[string]string{"password": "a brand new password"}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("choosing a new password: %s", res.Status)
	}
	for _, method := range []string{"GET", "POST"} {
		res := asBrowser(t, freshBrowser(), method, asked+"/"+token, map[string]string{"password": "yet another password"})
		if p := problemOf(t, res); res.StatusCode != http.StatusGone || p.Code != api.LinkUsed {
			t.Fatalf("%s a used link: %s %+v", method, res.Status, p)
		}
	}
	if code, _ := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/settings", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("the session from before the reset still works: %d", code)
	}
	if res := asBrowser(t, freshBrowser(), "POST", srv.URL+"/api/v1/session", api.SignIn{Email: "ada@example.com", Password: "a brand new password"}); res.StatusCode != http.StatusOK {
		t.Fatalf("signing in with the new password: %s", res.Status)
	}
}

func TestNoPasswordResetWithoutMail(t *testing.T) {
	_, _, srv, _ := signedIn(t)
	if res := asBrowser(t, freshBrowser(), "POST", srv.URL+"/api/v1/password-resets", map[string]string{"email": "ada@example.com"}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("asking for a link from a server that sends no mail: %s", res.Status)
	}
	res := asBrowser(t, freshBrowser(), "GET", srv.URL+"/api/v1/sign-in", nil)
	var ways api.SignInWays
	if json.NewDecoder(res.Body).Decode(&ways); res.StatusCode != http.StatusOK || !ways.Password || ways.Forgot || ways.Sso || len(ways.Social) != 0 {
		t.Fatalf("the ways to sign in on a server with no mail, address or providers: %s %+v", res.Status, ways)
	}
}

func TestFindingSingleSignOnThroughTheAPI(t *testing.T) {
	s := tempStore(t)
	alpha := mustOrg(t, s, "alpha")
	mustUser(t, s, alpha, "ada@alpha.example", "Ada Lovelace", "owner")
	bob := mustUser(t, s, alpha, "bob@alpha.example", "Bob Stone", "member")
	okta := newIdP(t, "", false)
	dns := map[string][]string{}
	app, srv := ssoServer(t, s, dns, okta, okta)
	app.social = []socialWay{okta.way("okta")}
	connected(t, srv, dns, s, "alpha", "alpha.example", okta, map[string]any{"required": true})

	res := asBrowser(t, freshBrowser(), "GET", srv.URL+"/api/v1/sign-in", nil)
	var ways api.SignInWays
	if json.NewDecoder(res.Body).Decode(&ways); !ways.Sso || len(ways.Social) != 1 || ways.Social[0].StartUrl != "/login/with/okta" {
		t.Fatalf("the ways to sign in: %+v", ways)
	}

	b := newBrowser(t, srv.URL)
	find := func(email string) []api.SingleSignOnOrg {
		t.Helper()
		res := asBrowser(t, b.c, "POST", srv.URL+"/api/v1/sign-in/sso", map[string]string{"email": email, "next": "//elsewhere.example"})
		var out struct{ Orgs []api.SingleSignOnOrg }
		if json.NewDecoder(res.Body).Decode(&out); res.StatusCode != http.StatusOK {
			t.Fatalf("finding the sign-on for %s: %s", email, res.Status)
		}
		return out.Orgs
	}
	// Whether an account exists makes no difference: only the domain does.
	if got := find("nobody@alpha.example"); len(got) != 1 || got[0].Slug != "alpha" || strings.Contains(got[0].StartUrl, "elsewhere") {
		t.Fatalf("an address at alpha's domain: %+v", got)
	}
	if got := find("bob@elsewhere.example"); len(got) != 0 {
		t.Fatalf("an address at nobody's domain: %+v", got)
	}

	// Bob signs in with his password; alpha says it wants its own sign-on, which the
	// link the API found walks him through.
	res = asBrowser(t, b.c, "POST", srv.URL+"/api/v1/session", api.SignIn{Email: bob.Email, Password: "correct horse battery"})
	var in api.Session
	if json.NewDecoder(res.Body).Decode(&in); len(in.Orgs) != 1 || !in.Orgs[0].SsoRequired {
		t.Fatalf("a password session where single sign-on is required: %+v", in)
	}
	res = asBrowser(t, b.c, "GET", srv.URL+"/o/alpha/api/v1/settings", nil)
	if p := problemOf(t, res); res.StatusCode != http.StatusForbidden || p.Code != api.SsoRequired {
		t.Fatalf("the organisation's API before its sign-on: %s %+v", res.Status, p)
	}
	start := find(bob.Email)[0].StartUrl
	okta.says(map[string]any{"sub": "o-bob", "email": bob.Email})
	code, _, location := b.get(start)
	if code != http.StatusSeeOther || !strings.HasPrefix(location, okta.srv.URL+"/authorize") {
		t.Fatalf("following the start link the API gave: %d %q", code, location)
	}
	if code, _, _ := b.get("/login/callback?" + okta.approve(location).Encode()); code != http.StatusSeeOther {
		t.Fatalf("coming back from the provider: %d", code)
	}
	res = asBrowser(t, b.c, "GET", srv.URL+"/api/v1/session", nil)
	if json.NewDecoder(res.Body).Decode(&in); len(in.Orgs) != 1 || in.Orgs[0].SsoRequired {
		t.Fatalf("after going through the sign-on: %+v", in)
	}

	// The start link alone, without having asked, goes nowhere near the provider.
	if code, _, location := newBrowser(t, srv.URL).get(start); strings.HasPrefix(location, okta.srv.URL) {
		t.Fatalf("a start link followed without asking: %d %q", code, location)
	}
}
