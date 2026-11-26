package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
)

func noRedirects() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func get(t *testing.T, client *http.Client, u string) (*http.Response, string) {
	t.Helper()
	res, err := client.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, string(b)
}

func TestTheAppIsOneShellAtEveryPage(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	c := repliedPerson(t, s, o, "Hannah Pryce")
	task := must(s.CreateTask(o.ID, "Kitchen makers", "Heads of procurement", "Ada Lovelace"))
	pages := []string{"companies/" + itoa(*c.CompanyID), "tasks/" + itoa(task.ID), "add?popup=1&name=Hannah", "today?step=linkedin", "research?f=nonsense&task=x"}
	for _, p := range appPages {
		pages = append(pages, p)
	}
	for _, p := range pages {
		res, body := get(t, client, srv.URL+"/o/alpha/"+p)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d", p, res.StatusCode)
		}
		if !strings.Contains(body, `<div id="app"></div>`) || !strings.Contains(body, `src="/assets/app/main.js?v=`+assetVersion+`"`) {
			t.Errorf("%s isn't the app's shell: %s", p, body)
		}
		if strings.Contains(body, "/assets/js/app.js") {
			t.Errorf("%s loads the templ pages' script, which reloads and rewrites what React draws", p)
		}
		if strings.Contains(body, `aria-label="Main"`) {
			t.Errorf("%s still has the server's sidebar", p)
		}
		if res.Header.Get("Content-Security-Policy") != contentPolicy || res.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s lost the page headers: %v", p, res.Header)
		}
	}
	for _, p := range []string{"nowhere", "settings/more"} {
		if res, _ := get(t, client, srv.URL+"/o/alpha/"+p); res.StatusCode != http.StatusNotFound {
			t.Errorf("%s, which the app doesn't have: %d", p, res.StatusCode)
		}
	}
	for _, p := range []string{"today", "companies/1", "add?popup=1"} {
		res, _ := get(t, noRedirects(), srv.URL+"/o/alpha/"+p)
		if res.StatusCode != http.StatusSeeOther || !strings.HasPrefix(res.Header.Get("Location"), "/login?next=") {
			t.Errorf("%s, signed out: %d %s", p, res.StatusCode, res.Header.Get("Location"))
		}
	}
	if res, _ := get(t, client, srv.URL+"/o/nope/today"); res.StatusCode != http.StatusNotFound {
		t.Errorf("an organisation you aren't in: %d", res.StatusCode)
	}
	if res, _ := get(t, client, srv.URL+"/assets/app/main.js?v="+assetVersion); res.StatusCode != http.StatusOK || !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("the app's script: %d %s", res.StatusCode, res.Header.Get("Cache-Control"))
	}

	// Every page the server serves the shell at is routed in the app too.
	router, err := os.ReadFile("web/src/router.tsx")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range append(appPages, "tasks/$id", "companies/$id") {
		if !strings.Contains(string(router), `path: "`+p+`"`) {
			t.Errorf("the server serves %s but web/src/router.tsx doesn't route it", p)
		}
	}
}

func TestTheOldAppAddressesMoveToTheRealOnes(t *testing.T) {
	t.Parallel()
	_, _, srv, _ := signedIn(t)
	client := signInAs(t, srv, "ada@example.com", "correct horse battery")
	client.CheckRedirect = noRedirects().CheckRedirect
	for from, to := range map[string]string{
		"app/research?task=3": "/o/alpha/research?task=3",
		"app/companies/7":     "/o/alpha/companies/7",
		"app/add?popup=1&url=https%3A%2F%2Fwww.linkedin.com%2Fin%2Fhannah": "/o/alpha/add?popup=1&url=https%3A%2F%2Fwww.linkedin.com%2Fin%2Fhannah",
	} {
		res, _ := get(t, client, srv.URL+"/o/alpha/"+from)
		if res.StatusCode != http.StatusFound || res.Header.Get("Location") != to {
			t.Errorf("%s: %d %s, want %s", from, res.StatusCode, res.Header.Get("Location"), to)
		}
	}
	if res, _ := get(t, noRedirects(), srv.URL+"/o/alpha/app/research"); res.StatusCode != http.StatusSeeOther || !strings.HasPrefix(res.Header.Get("Location"), "/login") {
		t.Errorf("an old address, signed out: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
}

func TestTheNavSaysWhatTheSidebarShows(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	repliedPerson(t, s, o, "Hannah Pryce")
	must(s.AddEnquiry(o.ID, "form", EnquiryIn{Kind: "support", Name: "Edsger Dijkstra", Email: "edsger@eindhoven.example", Message: "Colours look pale on phones"}))
	beta := must(s.CreateOrgWithOwner("", "Beta", must(s.UserByEmail("ada@example.com"))))

	read := func(c *http.Client, header ...string) api.Nav {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+"/o/alpha/api/v1/nav", nil)
		if len(header) == 2 {
			req.Header.Set(header[0], header[1])
		}
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var n api.Nav
		if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&n) != nil {
			t.Fatalf("the nav: %d", res.StatusCode)
		}
		return n
	}

	n := read(client)
	if n.User == nil || n.User.Name != "Ada Lovelace" || n.User.Email != "ada@example.com" {
		t.Errorf("who is asking: %+v", n.User)
	}
	if n.Org.Slug != "alpha" || n.Org.Name != "Alpha" || n.Org.Color == "" || n.Org.Id != o.ID {
		t.Errorf("the organisation: %+v", n.Org)
	}
	if len(n.Orgs) != 2 || n.Orgs[0].Role != "owner" || (n.Orgs[0].Slug != beta.Slug && n.Orgs[1].Slug != beta.Slug) {
		t.Errorf("every organisation: %+v", n.Orgs)
	}
	if n.Role != "owner" || !n.Owner || !n.Sales || !n.Support || n.Work != "owner" {
		t.Errorf("an owner works everything: %+v", n)
	}
	want := (&App{s: s}).nav(&Req{Org: o})
	if n.Counts.Today != want.Today || n.Counts.Research != want.Research || n.Counts.Conversations != want.Conversations || n.Counts.Enquiries != 1 || n.Counts.Companies != want.Companies || n.Counts.Companies == 0 {
		t.Errorf("the counts %+v, the sidebar's %+v", n.Counts, want)
	}

	mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	grace := must(s.UserByEmail("grace@example.com"))
	if err := s.SetAreas(o.ID, grace.ID, []string{"support"}); err != nil {
		t.Fatal(err)
	}
	g := read(signInAs(t, srv, "grace@example.com", "correct horse battery"))
	if g.Role != "member" || g.Owner || g.Sales || !g.Support || len(g.Areas) != 1 || g.Areas[0] != "support" || g.Work != "member · support" {
		t.Errorf("a support member: %+v", g)
	}
	if len(g.Orgs) != 1 || g.Orgs[0].Role != "member" {
		t.Errorf("a member of one organisation: %+v", g.Orgs)
	}

	key, _, err := s.CreateKey(o.ID, "scout", []string{"read"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	k := read(http.DefaultClient, "Authorization", "Bearer "+key)
	if k.User != nil || len(k.Orgs) != 0 || k.Role != "" || k.Owner || k.Org.Slug != "alpha" || k.Counts.Enquiries != 1 {
		t.Errorf("a key gets the organisation and its counts, and no one: %+v", k)
	}
	if code, _ := call(t, http.DefaultClient, "GET", srv.URL+"/o/alpha/api/v1/nav", "", nil); code != http.StatusUnauthorized {
		t.Errorf("signed out: %d", code)
	}
}

func TestStartingAnOrganisationThroughTheAPI(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	code, body := call(t, client, "POST", srv.URL+"/api/v1/orgs", "application/json", map[string]string{"name": "Gamma Works"})
	var made api.OrgSummary
	if code != http.StatusCreated || json.Unmarshal([]byte(body), &made) != nil || made.Slug != "gamma-works" || made.Name != "Gamma Works" {
		t.Fatalf("starting one: %d %s", code, body)
	}
	org := must(s.OrgBySlug("gamma-works"))
	if m, err := s.Membership(org.ID, must(s.UserByEmail("ada@example.com")).ID); err != nil || m.Role != "owner" {
		t.Fatalf("whoever starts one owns it: %+v %v", m, err)
	}
	if code, body := call(t, client, "POST", srv.URL+"/api/v1/orgs", "application/json", map[string]string{"name": "Gamma Works"}); code != http.StatusConflict {
		t.Errorf("a name whose address is taken: %d %s", code, body)
	}
	if code, _ := call(t, client, "POST", srv.URL+"/api/v1/orgs", "application/json", map[string]string{"name": ""}); code != http.StatusBadRequest {
		t.Errorf("no name: %d", code)
	}
	if code, _ := call(t, http.DefaultClient, "POST", srv.URL+"/api/v1/orgs", "application/json", map[string]string{"name": "Delta"}); code != http.StatusUnauthorized {
		t.Errorf("signed out: %d", code)
	}

	key, _, err := s.CreateKey(o.ID, "scout", []string{"read", "team:write"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/orgs", strings.NewReader(`{"name":"Delta"}`))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("a key can't start an organisation: %d", res.StatusCode)
	}
	if _, err := s.OrgBySlug("delta"); err == nil {
		t.Error("a key started an organisation")
	}
}

func TestReactAddPerson(t *testing.T) {
	t.Parallel()
	_, _, srv, client := signedIn(t)

	// What a LinkedIn profile gives the form, as the templ page read it.
	q := url.Values{"url": {"https://www.linkedin.com/in/hannah-pryce?trk=nav"}, "name": {"(3) Hannah Pryce | LinkedIn"}, "headline": {"Head of Sales at Larkmoor Yachts | Sailing"}}
	var draft api.PersonDraft
	code, out := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/people/draft?"+q.Encode(), "", nil)
	if code != http.StatusOK || json.Unmarshal([]byte(out), &draft) != nil {
		t.Fatalf("drafting a person: %d %s", code, out)
	}
	if draft.Linkedin != "https://www.linkedin.com/in/hannah-pryce" || draft.Name != "Hannah Pryce" || draft.Title != "Head of Sales" || draft.Company != "Larkmoor Yachts" {
		t.Errorf("the profile read as %+v", draft)
	}
	if !strings.HasPrefix(draft.Bookmarklet, "javascript:") || !strings.Contains(draft.Bookmarklet, srv.URL+"/o/alpha/add?popup=1&url=") {
		t.Errorf("the bookmarklet should open the add popup at its real address: %s", draft.Bookmarklet)
	}
	q.Set("company", "Quay Marine")
	if _, out := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/people/draft?"+q.Encode(), "", nil); !strings.Contains(out, `"company":"Quay Marine"`) {
		t.Errorf("a company named in the address should win over the headline's: %s", out)
	}
}

func TestCampaignsSayWhyNothingCanBeSent(t *testing.T) {
	t.Parallel()
	_, _, srv, client := signedIn(t)
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/campaigns", "", nil); !strings.Contains(body, `"not_ready":"`) || strings.Contains(body, `"not_ready":""`) {
		t.Errorf("campaigns should say why nothing can be sent yet: %s", body)
	}
}
