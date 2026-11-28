package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/privacy"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// The Settings page offers the owner's parts only to an owner, by the nav's word.
func TestTheSettingsPageKnowsWhoIsAnOwner(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	asGrace := signInAs(t, srv, "grace@example.com", "correct horse battery")
	for want, c := range map[string]*http.Client{`"owner":true`: ada, `"owner":false`: asGrace} {
		if code, body := call(t, c, "GET", srv.URL+"/o/alpha/api/v1/nav", "", nil); code != http.StatusOK || !strings.Contains(body, want) {
			t.Errorf("the nav should say %s: %d %s", want, code, body)
		}
	}
}

func TestTheTeamThroughTheAPI(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	grace := mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	asGrace := signInAs(t, srv, "grace@example.com", "correct horse battery")
	base := srv.URL + "/o/alpha/api/v1/members"
	key := func(scopes ...string) string {
		return keySecret(s.CreateKey(o.ID, "robot", scopes, "Ada Lovelace"))
	}
	reader, team := key("read"), key("team:write")

	code, body := asKey(t, "GET", base, reader, nil)
	var list struct{ Items []api.Member }
	if json.Unmarshal([]byte(body), &list); code != http.StatusOK || len(list.Items) != 2 {
		t.Fatalf("listing the team: %d %s", code, body)
	}
	for _, m := range list.Items {
		if m.Email == "grace@example.com" && (m.Role != "member" || len(m.Areas) != 2) {
			t.Fatalf("Grace as listed: %+v", m)
		}
	}

	// Only an owner, or a key given team:write, changes the team.
	newcomer := map[string]any{"email": "edsger@example.com", "name": "Edsger Dijkstra"}
	if code, body := call(t, asGrace, "POST", base, "application/json", newcomer); code != http.StatusForbidden || !strings.Contains(body, "owner") {
		t.Fatalf("a member who isn't an owner added someone: %d %s", code, body)
	}
	if code, body := asKey(t, "POST", base, reader, newcomer); code != http.StatusForbidden || !strings.Contains(body, "team:write") {
		t.Fatalf("a read key added someone: %d %s", code, body)
	}
	if code, _ := asKey(t, "POST", base, keySecret(s.CreateKey(o.ID, "settings", []string{"settings:write"}, "Ada Lovelace")), newcomer); code != http.StatusForbidden {
		t.Fatalf("settings:write was enough to add someone: %d", code)
	}
	code, body = call(t, ada, "POST", base, "application/json", newcomer)
	if code != http.StatusCreated || !strings.Contains(body, "edsger@example.com added to") || !strings.Contains(body, "first password, shown once") {
		t.Fatalf("an owner adding someone: %d %s", code, body)
	}
	if code, body := asKey(t, "POST", base, team, map[string]any{"email": "barbara@example.com", "role": "owner"}); code != http.StatusCreated {
		t.Fatalf("a team:write key adding someone: %d %s", code, body)
	}

	member := base + "/" + itoa(grace.ID)
	if code, body := call(t, asGrace, "PATCH", member, "application/json", map[string]any{"areas": []string{}}); code != http.StatusForbidden {
		t.Fatalf("a member changed their own areas: %d %s", code, body)
	}
	code, body = call(t, ada, "PATCH", member, "application/json", map[string]any{"areas": []string{"support"}})
	var got api.Member
	if json.Unmarshal([]byte(body), &got); code != http.StatusOK || len(got.Areas) != 1 || got.Areas[0] != "support" {
		t.Fatalf("giving Grace only support: %d %s", code, body)
	}
	code, body = asKey(t, "PATCH", member, team, map[string]any{"areas": []string{}})
	if json.Unmarshal([]byte(body), &got); code != http.StatusOK || len(got.Areas) != 0 {
		t.Fatalf("leaving Grace reading only: %d %s", code, body)
	}
	if code, body := call(t, ada, "PATCH", member, "application/json", map[string]any{"role": "admin"}); code != http.StatusBadRequest {
		t.Fatalf("a role there isn't: %d %s", code, body)
	}
	if code, body := call(t, ada, "PATCH", base+"/999999", "application/json", map[string]any{"role": "owner"}); code != http.StatusNotFound {
		t.Fatalf("someone not in the team: %d %s", code, body)
	}

	// The last owner stays.
	only := mustOrg(t, s, "gamma")
	ownerOfOne := mustUser(t, s, only, "solo@example.com", "Solo Owner", "owner")
	soloKey := keySecret(s.CreateKey(only.ID, "robot", []string{"team:write"}, "Solo Owner"))
	if code, body := asKey(t, "PATCH", srv.URL+"/o/gamma/api/v1/members/"+itoa(ownerOfOne.ID), soloKey, map[string]any{"role": "member"}); code != http.StatusBadRequest || !strings.Contains(body, "needs an owner") {
		t.Fatalf("the last owner was made a member: %d %s", code, body)
	}
	if code, body := asKey(t, "DELETE", srv.URL+"/o/gamma/api/v1/members/"+itoa(ownerOfOne.ID), soloKey, nil); code != http.StatusBadRequest || !strings.Contains(body, "needs an owner") {
		t.Fatalf("the last owner was removed: %d %s", code, body)
	}

	// Nobody reaches into another organisation's team.
	if code, _ := asKey(t, "DELETE", member, soloKey, nil); code == http.StatusNoContent {
		t.Fatal("another organisation's key removed Grace")
	}
	if code, _ := call(t, ada, "DELETE", member, "", nil); code != http.StatusNoContent {
		t.Fatalf("removing Grace: %d", code)
	}
	if must(s.Members(o.ID)); strings.Contains(fmtMembers(must(s.Members(o.ID))), "grace@example.com") {
		t.Fatal("Grace is still in the team")
	}
}

func fmtMembers(ms []identity.Member) string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Email)
	}
	return strings.Join(out, " ")
}

func keySecret(secret string, _ *store.APIKey, err error) string {
	if err != nil {
		panic(err)
	}
	return secret
}

func TestYourOwnVoiceAndPasswordThroughTheAPI(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	me := srv.URL + "/o/alpha/api/v1/me"
	reader := keySecret(s.CreateKey(o.ID, "robot", []string{"read"}, "Ada Lovelace"))

	code, body := call(t, ada, "GET", me, "", nil)
	var got api.Me
	if json.Unmarshal([]byte(body), &got); code != http.StatusOK || got.Email != "ada@example.com" || got.Role != "owner" || !got.HasPassword {
		t.Fatalf("reading yourself: %d %s", code, body)
	}
	if code, body := call(t, ada, "PATCH", me, "application/json", map[string]any{"signature": "  Ada\nAlpha  "}); code != http.StatusOK || !strings.Contains(body, `"signature":"Ada\nAlpha"`) {
		t.Fatalf("changing your signature: %d %s", code, body)
	}
	if code, body := call(t, ada, "PATCH", me, "application/json", map[string]any{"caller_name": "Ada"}); code != http.StatusOK || !strings.Contains(body, `"signature":"Ada\nAlpha"`) || !strings.Contains(body, `"caller_name":"Ada"`) {
		t.Fatalf("what was left out should stay: %d %s", code, body)
	}
	members := must(s.Members(o.ID))
	if members[0].CallerName != "Ada" || members[0].Signature != "Ada\nAlpha" {
		t.Fatalf("saved as %+v", members[0].Membership)
	}

	// A key is nobody, so it has no voice or password of its own.
	for _, try := range []struct {
		method, path string
		body         any
	}{{"GET", "", nil}, {"PATCH", "", map[string]any{"signature": "robot"}}, {"POST", "/password", map[string]any{"current": "x", "new": "a brand new password"}}} {
		if code, body := asKey(t, try.method, me+try.path, reader, try.body); code != http.StatusForbidden || !strings.Contains(body, "signed-in member") {
			t.Errorf("%s me%s with a key: %d %s", try.method, try.path, code, body)
		}
	}

	password := me + "/password"
	if code, body := call(t, ada, "POST", password, "application/json", map[string]any{"current": "wrong", "new": "a brand new password"}); code != http.StatusBadRequest {
		t.Fatalf("a wrong current password: %d %s", code, body)
	}
	req, _ := http.NewRequest(http.MethodPost, password, strings.NewReader(`{"current":"correct horse battery","new":"a brand new password"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := ada.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	device := false
	for _, c := range res.Cookies() {
		device = device || c.Name == deviceCookie && c.Value != ""
	}
	if res.StatusCode != http.StatusOK || !device {
		t.Fatalf("changing the password: %d, device cookie %t", res.StatusCode, device)
	}
	if code, _ := call(t, ada, "GET", me, "", nil); code != http.StatusOK {
		t.Fatalf("the session that changed the password was signed out: %d", code)
	}
	if !signIn(t, newMailClient(), srv, "ada@example.com", "a brand new password") {
		t.Fatal("the new password doesn't sign in")
	}

	// Guessing is slowed as it is on the old form.
	for i := range freeAttempts + 1 {
		req, _ := http.NewRequest(http.MethodPost, password, strings.NewReader(`{"current":"a guess","new":"another new password"}`))
		req.Header.Set("Content-Type", "application/json")
		res, err := ada.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if i < freeAttempts && res.StatusCode != http.StatusBadRequest {
			t.Fatalf("guess %d: %d", i+1, res.StatusCode)
		}
		if i == freeAttempts && (res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") == "") {
			t.Fatalf("after %d wrong guesses: %d, Retry-After %q", freeAttempts, res.StatusCode, res.Header.Get("Retry-After"))
		}
	}
}

func TestOutgoingMailThroughTheAPI(t *testing.T) {
	t.Parallel()
	s, srv, ada, box := mailSite(t, nil)
	o := must(s.OrgBySlug("alpha"))
	mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	asGrace := signInAs(t, srv, "grace@example.com", "correct horse battery")
	url := srv.URL + "/o/alpha/api/v1/mail-server"
	reader := keySecret(s.CreateKey(o.ID, "reader", []string{"read"}, "Ada Lovelace"))
	mailer := keySecret(s.CreateKey(o.ID, "mailer", []string{"mail:configure"}, "Ada Lovelace"))
	server := map[string]any{"host": "smtp.alpha.example", "port": 587, "username": "alpha-user", "password": "s3cret-smtp-pass", "sender": "Alpha <hello@alpha.example>"}

	if code, body := call(t, asGrace, "GET", url, "", nil); code != http.StatusForbidden {
		t.Fatalf("a member who isn't an owner read the mail server: %d %s", code, body)
	}
	if code, _ := call(t, asGrace, "PUT", url, "application/json", server); code != http.StatusForbidden {
		t.Fatalf("a member who isn't an owner set the mail server: %d", code)
	}
	if code, body := asKey(t, "GET", url, reader, nil); code != http.StatusForbidden || !strings.Contains(body, "mail:configure") {
		t.Fatalf("a read key read the mail server: %d %s", code, body)
	}
	if code, body := call(t, ada, "PUT", url, "application/json", map[string]any{"host": "127.0.0.1", "port": 4222, "sender": "a@alpha.example"}); code != http.StatusBadRequest || !strings.Contains(body, "this machine") {
		t.Fatalf("this machine as the server: %d %s", code, body)
	}

	code, body := call(t, ada, "PUT", url, "application/json", server)
	stored := must(s.OrgMail(o.ID))
	if code != http.StatusOK || !strings.Contains(body, `"has_password":true`) || strings.Contains(body, "s3cret") || strings.Contains(body, stored.Password) {
		t.Fatalf("saving the server shows its password, or didn't save: %d %s", code, body)
	}
	if !store.Sealed(stored.Password) {
		t.Fatal("the password is stored unsealed")
	}
	code, body = asKey(t, "GET", url, mailer, nil)
	if code != http.StatusOK || !strings.Contains(body, "smtp.alpha.example") || strings.Contains(body, "s3cret") || strings.Contains(body, stored.Password) {
		t.Fatalf("reading the server: %d %s", code, body)
	}
	// Left empty, the password stays; with a new username it is needed again.
	if code, body := asKey(t, "PUT", url, mailer, map[string]any{"host": "smtp.alpha.example", "port": 587, "username": "alpha-user", "sender": "Alpha <hello@alpha.example>"}); code != http.StatusOK || !strings.Contains(body, `"has_password":true`) {
		t.Fatalf("saving with the password left out: %d %s", code, body)
	}

	test := url + "/test"
	if code, body := call(t, ada, "POST", test, "application/json", nil); code != http.StatusOK || !strings.Contains(body, "Sent to ada@example.com through smtp.alpha.example") {
		t.Fatalf("a test message: %d %s", code, body)
	}
	if got := box.take(t, 1)[0]; got.To != "ada@example.com" || got.via.Password != "s3cret-smtp-pass" {
		t.Fatalf("the test went %+v", got)
	}
	// It goes to whoever asked, so it is in the language they asked in.
	get(t, ada, srv.URL+"/o/alpha/settings?lang=en-XA")
	if code, body := call(t, ada, "POST", test, "application/json", nil); code != http.StatusOK || !strings.Contains(body, "[Šéñţ ţö ada@example.com ţĥŕöûĝĥ smtp.alpha.example.]") {
		t.Fatalf("a test message asked for in another language: %d %s", code, body)
	}
	if got := box.take(t, 1)[0]; got.Subject != say.Errorf("Prospector can send mail for %s", "Alpha").In("en-XA") {
		t.Fatalf("the test's mail isn't in the language it was asked in: %q", got.Subject)
	}
	get(t, ada, srv.URL+"/o/alpha/settings?lang=")
	if code, body := asKey(t, "POST", test, mailer, nil); code != http.StatusForbidden || !strings.Contains(body, "signed-in member") {
		t.Fatalf("a key, which has no inbox, sent a test: %d %s", code, body)
	}
	if code, _ := call(t, asGrace, "POST", test, "application/json", nil); code != http.StatusForbidden {
		t.Fatalf("a member who isn't an owner sent a test: %d", code)
	}

	if code, body := call(t, ada, "PUT", url, "application/json", map[string]any{"host": ""}); code != http.StatusOK || !strings.Contains(body, `"host":""`) {
		t.Fatalf("clearing the server: %d %s", code, body)
	}
	if _, err := s.OrgMail(o.ID); err == nil {
		t.Fatal("the server is still saved")
	}
}

func TestSingleSignOnThroughTheAPI(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	alpha, beta := mustOrg(t, s, "alpha"), mustOrg(t, s, "beta")
	mustUser(t, s, alpha, "ada@alpha.example", "Ada Lovelace", "owner")
	mustUser(t, s, alpha, "bob@alpha.example", "Bob Stone", "member")
	okta := newIdP(t, "", false)
	dns := map[string][]string{}
	_, srv := ssoServer(t, s, dns, okta)
	ada, bob := newBrowser(t, srv.URL), newBrowser(t, srv.URL)
	ada.login("ada@alpha.example", "correct horse battery")
	bob.login("bob@alpha.example", "correct horse battery")
	base := srv.URL + "/o/alpha/api/v1/sso"
	do := func(b *browser, method, u string, body any) (int, string) {
		t.Helper()
		return call(t, b.c, method, u, "application/json", body)
	}

	if code, _ := do(bob, "GET", base, nil); code != http.StatusForbidden {
		t.Fatalf("a member who isn't an owner read single sign-on: %d", code)
	}
	if code, _ := do(bob, "POST", base+"/domains", map[string]any{"domain": "alpha.example"}); code != http.StatusForbidden {
		t.Fatalf("a member who isn't an owner added a domain: %d", code)
	}
	reader := keySecret(s.CreateKey(alpha.ID, "reader", []string{"read"}, "Ada Lovelace"))
	if code, body := asKey(t, "GET", base+"/domains", reader, nil); code != http.StatusForbidden || !strings.Contains(body, "sso:configure") {
		t.Fatalf("a read key listed the domains, with their tokens: %d %s", code, body)
	}

	code, body := do(ada, "POST", base+"/domains", map[string]any{"domain": "@Alpha.Example"})
	var d api.SSODomain
	if json.Unmarshal([]byte(body), &d); code != http.StatusCreated || d.Domain != "alpha.example" || d.Token == "" || d.VerifiedAt != nil {
		t.Fatalf("adding a domain: %d %s", code, body)
	}
	if code, body := do(ada, "POST", base+"/domains", map[string]any{"domain": "alpha.example"}); code != http.StatusConflict {
		t.Fatalf("adding it again: %d %s", code, body)
	}
	if code, body := do(ada, "POST", base+"/domains/alpha.example/verify", nil); code != http.StatusBadRequest || !strings.Contains(body, "no TXT record") {
		t.Fatalf("verifying before the record is there: %d %s", code, body)
	}
	dns[d.Record] = []string{d.Token}
	if code, body := do(ada, "POST", base+"/domains/alpha.example/verify", nil); code != http.StatusOK || !strings.Contains(body, `"verified_at":"`) {
		t.Fatalf("verifying: %d %s", code, body)
	}

	code, body = do(ada, "PUT", base, map[string]any{"issuer": okta.srv.URL, "client_id": "client", "client_secret": "the-client-secret", "required": true})
	var got api.SSO
	if json.Unmarshal([]byte(body), &got); code != http.StatusOK || !got.HasClientSecret || !got.Required || got.RedirectUri == "" || strings.Contains(body, "the-client-secret") {
		t.Fatalf("saving the provider: %d %s", code, body)
	}
	conn, _ := s.SSO(alpha.ID)
	if conn.ClientSecret == "the-client-secret" || !store.Sealed(conn.ClientSecret) {
		t.Fatal("the client secret is stored unsealed")
	}
	if _, body := do(ada, "GET", base, nil); strings.Contains(body, "the-client-secret") || strings.Contains(body, conn.ClientSecret) {
		t.Fatalf("reading single sign-on shows the secret: %s", body)
	}
	if code, body := do(ada, "DELETE", base+"/domains/alpha.example", nil); code != http.StatusBadRequest || !strings.Contains(body, "last verified domain") {
		t.Fatalf("removing the last domain while sign-on is required: %d %s", code, body)
	}

	configurer := keySecret(s.CreateKey(alpha.ID, "configurer", []string{"sso:configure"}, "Ada Lovelace"))
	if code, body := asKey(t, "GET", base+"/domains", configurer, nil); code != http.StatusOK || !strings.Contains(body, "alpha.example") {
		t.Fatalf("an sso:configure key listing domains: %d %s", code, body)
	}
	theirs := keySecret(s.CreateKey(beta.ID, "theirs", []string{"sso:configure"}, "Grace Hopper"))
	if code, _ := asKey(t, "PUT", base, theirs, map[string]any{"issuer": ""}); code == http.StatusOK {
		t.Fatal("another organisation's key turned alpha's sign-on off")
	}
	if code, body := asKey(t, "PUT", base, configurer, map[string]any{"issuer": ""}); code != http.StatusOK || !strings.Contains(body, `"issuer":""`) {
		t.Fatalf("turning it off: %d %s", code, body)
	}
	if code, _ := asKey(t, "DELETE", base+"/domains/alpha.example", configurer, nil); code != http.StatusNoContent {
		t.Fatalf("removing the domain once sign-on is off: %d", code)
	}
}

func TestExportingEverythingThroughTheAPI(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	asGrace := signInAs(t, srv, "grace@example.com", "correct horse battery")
	url := srv.URL + "/o/alpha/api/v1/export"

	if code, _ := call(t, asGrace, "POST", url, "application/json", nil); code != http.StatusForbidden {
		t.Fatalf("a member who isn't an owner exported everything: %d", code)
	}
	if code, body := asKey(t, "POST", url, keySecret(s.CreateKey(o.ID, "reader", []string{"read"}, "Ada Lovelace")), nil); code != http.StatusForbidden || !strings.Contains(body, "org:export") {
		t.Fatalf("a read key exported everything: %d %s", code, body)
	}
	req, _ := http.NewRequest(http.MethodPost, url, nil)
	req.Header.Set("Content-Type", "application/json")
	res, err := ada.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var out privacy.OrgExport
	json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Cache-Control") != "no-store" || out.Organisation.Name == "" || len(out.Members) != 2 {
		t.Fatalf("exporting: %d %q %+v", res.StatusCode, res.Header.Get("Cache-Control"), out.Organisation)
	}
	if code, body := asKey(t, "POST", url, keySecret(s.CreateKey(o.ID, "exporter", []string{"org:export"}, "Ada Lovelace")), nil); code != http.StatusOK || !strings.Contains(body, `"organisation"`) {
		t.Fatalf("an org:export key: %d %.200s", code, body)
	}
}

func TestWhoTheNeverContactListHoldsBackThroughTheAPI(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	for _, name := range []string{"Hannah Pryce", "Ivo Marsh"} {
		must(s.QuickAddPerson(o, crm.QuickAdd{Name: name, Company: "Larkmoor Yachts", Email: strings.ToLower(strings.Fields(name)[0]) + "@larkmoor.example"}, "Ada Lovelace"))

	}
	url := srv.URL + "/o/alpha/api/v1/never-contact/covers"
	if code, body := call(t, ada, "GET", url, "", nil); code != http.StatusOK || !strings.Contains(body, `"items":[]`) || !strings.Contains(body, `"total":0`) {
		t.Fatalf("with nothing on the list: %d %s", code, body)
	}
	if code, body := call(t, ada, "PATCH", srv.URL+"/o/alpha/api/v1/settings", "application/json", map[string]any{"never_contact": "larkmoor.example"}); code != http.StatusOK {
		t.Fatalf("listing the domain: %d %s", code, body)
	}
	code, body := call(t, ada, "GET", url+"?limit=1", "", nil)
	if code != http.StatusOK || !strings.Contains(body, `"total":2`) || !strings.Contains(body, "Hannah Pryce") || !strings.Contains(body, `"next_cursor"`) {
		t.Fatalf("a page of one of two held back: %d %s", code, body)
	}
}
