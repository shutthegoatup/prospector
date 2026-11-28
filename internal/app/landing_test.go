package app

import (
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/store"
	"github.com/shutthegoatup/prospector/internal/tickets"
)

func TestFrontPage(t *testing.T) {
	t.Parallel()
	_, _, srv, member := signedIn(t)
	visitor := &http.Client{}

	code, body := call(t, visitor, "GET", srv.URL+"/", "", nil)
	if code != http.StatusOK || !strings.Contains(body, "One queue a day.") || !strings.Contains(body, `href="/login"`) {
		t.Fatalf("a signed-out visitor at /: %d, without the front page and its sign-in link", code)
	}
	for _, private := range []string{"alpha", "Ada Lovelace", "ada@example.com"} {
		if strings.Contains(body, private) {
			t.Errorf("the front page shows %q from this server's data", private)
		}
	}
	if found := regexp.MustCompile(`(?i)<script|<[^>]*\son[a-z]+\s*=`).FindString(body); found != "" {
		t.Errorf("the front page has a script or an inline handler: %s", found)
	}

	// Everything the page loads must be in the binary and served as what it is: the
	// release image has no system table of file types to fall back on.
	types := map[string]string{".css": "text/css", ".svg": "image/svg+xml", ".webp": "image/webp"}
	assets := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`).FindAllStringSubmatch(body, -1)
	if len(assets) < 8 {
		t.Fatalf("the front page loads only %d assets", len(assets))
	}
	for _, m := range assets {
		res, err := visitor.Get(srv.URL + m[1])
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		want := types[path.Ext(strings.SplitN(m[1], "?", 2)[0])]
		if res.StatusCode != http.StatusOK || want == "" || !strings.HasPrefix(res.Header.Get("Content-Type"), want) {
			t.Errorf("GET %s: %d %q, want %q", m[1], res.StatusCode, res.Header.Get("Content-Type"), want)
		}
		if !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
			t.Errorf("GET %s isn't cached for good: %q", m[1], res.Header.Get("Cache-Control"))
		}
	}

	// Someone signed in never sees it: / still takes them to their queue.
	res, err := member.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	landed, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || strings.Contains(string(landed), "One queue a day.") || res.Request.URL.Path != "/o/alpha/today" || !strings.Contains(string(landed), `<div id="app">`) {
		t.Fatalf("a signed-in member at / didn't land on their organisation: %d at %s", res.StatusCode, res.Request.URL)
	}
}

func TestFrontPageEnquiryForm(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	srv := httptest.NewServer((&App{s: s, frontOrg: "alpha"}).Routes())
	t.Cleanup(srv.Close)
	visitor := &http.Client{}
	// The page carries where its form posts, and the address it works at, for its script.
	formOf := func(body string) (intake, origin string) {
		m := regexp.MustCompile(`<div id="site" data-page="([^"]*)">`).FindStringSubmatch(body)
		if m == nil {
			return "", ""
		}
		var got struct{ Intake, Origin string }
		if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &got); err != nil {
			t.Fatalf("the front page's form: %v", err)
		}
		return got.Intake, got.Origin
	}

	// Named, but the organisation hasn't listed this address: a form would only be refused.
	if _, body := call(t, visitor, "GET", srv.URL+"/", "", nil); strings.Contains(body, "data-page") || strings.Contains(body, "<script") {
		t.Fatal("the front page has a form its organisation wouldn't take enquiries from")
	}
	takeEnquiriesFrom(t, s, o, "https://www.alpha.example\n"+srv.URL)

	res, err := visitor.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	res.Body.Close()
	body := string(page)
	if intake, _ := formOf(body); intake != "/o/alpha/intake" {
		t.Fatal("the front page has no enquiry form")
	}
	if !strings.Contains(res.Header.Get("Content-Security-Policy"), "connect-src 'self'") {
		t.Errorf("the form's page may post elsewhere: %q", res.Header.Get("Content-Security-Policy"))
	}
	if found := regexp.MustCompile(`(?i)<script(?: [^>]*)?>[^<]|<[^>]*\son[a-z]+\s*=`).FindString(body); found != "" {
		t.Errorf("the front page has an inline script or handler: %s", found)
	}
	script := regexp.MustCompile(`<script type="module" src="(/assets/app/site\.js[^"]*)"`).FindStringSubmatch(body)
	if script == nil {
		t.Fatal("the form's script isn't loaded")
	}
	got, err := visitor.Get(srv.URL + script[1])
	if err != nil {
		t.Fatal(err)
	}
	got.Body.Close()
	if got.StatusCode != http.StatusOK || !strings.Contains(got.Header.Get("Content-Type"), "javascript") {
		t.Errorf("GET %s: %d %q", script[1], got.StatusCode, got.Header.Get("Content-Type"))
	}

	// What the form's script sends, as the browser sends it from this same address.
	req, _ := http.NewRequest("POST", srv.URL+"/o/alpha/intake", strings.NewReader(`{"kind":"demo","name":"Hannah Pryce","email":"hannah@quay.example","message":"Could we see it running?","trap":""}`))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	sent, err := visitor.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	sent.Body.Close()
	rows, err := firstOf(s.Enquiries(o.ID, tickets.EnquiryFilter{}, 1))
	if sent.StatusCode != http.StatusOK || err != nil || len(rows) != 1 || rows[0].Kind != "demo" || rows[0].Site != srv.URL {
		t.Fatalf("the form's enquiry: %d, kept %+v, %v", sent.StatusCode, rows, err)
	}

	// A server told its address shows the form for that address alone, named as a browser
	// names it, and the script shows it nowhere else: reached by another name, the
	// route would refuse what it sent.
	told := httptest.NewServer((&App{s: s, frontOrg: "alpha", base: "https://Crm.alpha.example:443"}).Routes())
	t.Cleanup(told.Close)
	if _, body := call(t, visitor, "GET", told.URL+"/", "", nil); strings.Contains(body, "data-page") {
		t.Error("the form shows for an address the organisation hasn't listed")
	}
	takeEnquiriesFrom(t, s, o, "https://crm.alpha.example")
	if _, body := call(t, visitor, "GET", told.URL+"/", "", nil); func() bool { i, o := formOf(body); return i != "/o/alpha/intake" || o != "https://crm.alpha.example" }() {
		t.Error("the form doesn't name the origin it works on")
	}
	script2, _ := os.ReadFile("../../web/src/site/main.tsx")
	if !strings.Contains(string(script2), "got.origin === window.location.origin") {
		t.Error("the script shows the form whatever address the page was reached at")
	}

	// A server that names no organisation, or one that isn't there, serves the page as before.
	for _, slug := range []string{"", "nope"} {
		plain := httptest.NewServer((&App{s: s, frontOrg: slug}).Routes())
		t.Cleanup(plain.Close)
		takeEnquiriesFrom(t, s, o, plain.URL)
		code, body := call(t, visitor, "GET", plain.URL+"/", "", nil)
		if code != http.StatusOK || !strings.Contains(body, "One queue a day.") || strings.Contains(body, "data-page") || strings.Contains(body, "<script") {
			t.Errorf("a server whose front organisation is %q: %d, with a form or without the page", slug, code)
		}
	}
}

func TestFrontOrgIsMadeOnce(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	mustOrg(t, s, "alpha")
	const address = "https://crm.alpha.example"
	if err := s.EnsureFrontOrg(address + ":443"); err != nil {
		t.Fatal(err)
	}
	o, err := s.OrgBySlug(identity.FrontOrgSlug)
	if err != nil || o.Settings.EnquirySites != address {
		t.Fatalf("the front organisation: %+v, %v", o, err)
	}
	srv := httptest.NewServer((&App{s: s, frontOrg: identity.FrontOrgSlug, base: address}).Routes())
	t.Cleanup(srv.Close)
	if _, body := call(t, &http.Client{}, "GET", srv.URL+"/", "", nil); !strings.Contains(body, `&quot;intake&quot;:&quot;/o/prospector/intake&quot;,&quot;origin&quot;:&quot;`+address+`&quot;`) {
		t.Error("a server told its address starts without the front page's form")
	}

	// What its owners change afterwards isn't put back at the next start.
	takeEnquiriesFrom(t, s, o, "")
	if err := s.EnsureFrontOrg(address); err != nil {
		t.Fatal(err)
	}
	if o, _ := s.OrgBySlug(identity.FrontOrgSlug); o.Settings.EnquirySites != "" {
		t.Errorf("a restart listed %q again", o.Settings.EnquirySites)
	}
	var orgs int64
	if s.DB.Model(&store.Org{}).Count(&orgs); orgs != 2 {
		t.Errorf("%d organisations after two starts", orgs)
	}

	// A server that doesn't know its address has the organisation and no form.
	bare := tempStore(t)
	if err := bare.EnsureFrontOrg(""); err != nil {
		t.Fatal(err)
	}
	if o, err := bare.OrgBySlug(identity.FrontOrgSlug); err != nil || o.Settings.EnquirySites != "" {
		t.Errorf("without an address: %+v, %v", o, err)
	}
}

// A server with no form on its front page says why at start-up, and how to put it right.
func TestTheServerSaysWhyTheFrontPageHasNoForm(t *testing.T) {
	t.Parallel()
	const address = "https://crm.alpha.example"
	for name, c := range map[string]struct {
		base, sites, want string
	}{
		"no address and no sites":      {"", "", "PROSPECTOR_URL isn't set"},
		"another address listed":       {address, "https://www.alpha.example", "lists only https://www.alpha.example, not " + address},
		"an organisation made earlier": {address, "", "lists no enquiry sites, not " + address},
		"its own address listed":       {address + ":443", address, ""},
		"no address but sites listed":  {"", address, ""},
	} {
		s := tempStore(t)
		o := mustOrg(t, s, identity.FrontOrgSlug)
		takeEnquiriesFrom(t, s, o, c.sites)
		why := s.FrontFormOff(c.base)
		if c.want == "" && why != "" || c.want != "" && (!strings.Contains(why, c.want) || !strings.Contains(why, "member add prospector")) {
			t.Errorf("%s: %q", name, why)
		}
	}
}

// The app shares the front page's light palette: a page left on the old dark class
// would turn the components' dark variants back on.
func TestAppUsesTheLightPalette(t *testing.T) {
	t.Parallel()
	_, _, srv, member := signedIn(t)
	for _, page := range []string{"/o/alpha/today", "/o/alpha/settings"} {
		_, body := call(t, member, "GET", srv.URL+page, "", nil)
		if strings.Contains(body, `class="dark"`) || !strings.Contains(body, `<meta name="color-scheme" content="light"`) {
			t.Errorf("%s isn't on the light palette", page)
		}
	}
	// The app draws the sidebar, logo and all.
	layout, err := os.ReadFile("../../web/src/components/app/layout.tsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(layout), `<img src="/assets/img/mark.svg"`) || strings.Contains(string(layout), `"dark"`) {
		t.Error("the app's sidebar doesn't show the logo, or turns the dark palette on")
	}
}

// The front page says what a key may do. It names only scopes that exist, and every
// scope that lets a key send mail is on it, so the page cannot promise no key sends.
func TestTheFrontPageTellsTheTruthAboutKeys(t *testing.T) {
	t.Parallel()
	_, _, srv, _ := signedIn(t)
	_, body := call(t, &http.Client{}, "GET", srv.URL+"/", "", nil)
	named := regexp.MustCompile(`<code>([a-z]+(?::[a-z]+)?)</code>`).FindAllStringSubmatch(body, -1)
	if len(named) < 4 {
		t.Fatalf("the front page names only %d scopes", len(named))
	}
	for _, m := range named {
		if !identity.ValidScope(m[1]) {
			t.Errorf("the front page names a scope that does not exist: %s", m[1])
		}
	}
	for _, sends := range []string{"enquiries:reply", "customers:invite", "campaigns:send"} {
		if !strings.Contains(body, "<code>"+sends+"</code>") {
			t.Errorf("the front page leaves out %s, which lets a key send mail", sends)
		}
	}
	if strings.Contains(body, "Send anything") {
		t.Error("the front page still says no key can send anything")
	}
	if strings.Contains(body, "organisation's settings or members") || !strings.Contains(body, "<code>settings:write</code>") {
		t.Error("the front page still says no key can change settings")
	}
}

// The front page prices the hosted plans: free for the first five users, £5 a user
// after, and a conversation for larger teams. Self-hosting has a section of its own,
// not a plan. Each plan's button leads to the enquiry form, so the built page leaves it
// to the form's script.
func TestFrontPagePricing(t *testing.T) {
	t.Parallel()
	_, _, srv, _ := signedIn(t)
	_, body := call(t, &http.Client{}, "GET", srv.URL+"/", "", nil)
	start := strings.Index(body, `id="pricing"`)
	if start < 0 {
		t.Fatal("the front page has no pricing section")
	}
	pricing := body[start : start+strings.Index(body[start:], "</section>")]
	for _, want := range []string{"Free for your first 5 users.", "up to 5 users", `£</small>0</b>`, `£</small>5</b>`, "a user a month", "Larger teams", "prices exclude VAT",
		`data-plan-cta="Start free"`, `data-plan-cta="Talk to us"`} {
		if !strings.Contains(pricing, want) {
			t.Errorf("the front page's pricing lacks %q", want)
		}
	}
	if strings.Contains(pricing, "Self-host") || !strings.Contains(body, `id="start"`) {
		t.Error("self-hosting is priced as a plan, or has lost its own section")
	}
	if strings.Contains(body, `href="#contact"`) {
		t.Error("the built front page links to an enquiry form it may not have")
	}
	// Where the form's script mounts the form and the Contact link; renamed, the form
	// would silently not appear.
	for _, mount := range []string{`id="enquiry"></div>`, `id="site-links"`} {
		if !strings.Contains(body, mount) {
			t.Errorf("the front page lost the form script's mount %s", mount)
		}
	}
}

// The site's sections, cards and API operations are headings, so they can be navigated
// as such: built from components that draw divs, they would quietly stop being.
func TestSitePagesKeepTheirHeadings(t *testing.T) {
	t.Parallel()
	_, _, srv, _ := signedIn(t)
	visitor := &http.Client{}
	_, front := call(t, visitor, "GET", srv.URL+"/", "", nil)
	if n := strings.Count(front, "<h3"); n < 20 {
		t.Errorf("the front page has %d h3 headings, want a card or step title each", n)
	}
	_, ref := call(t, visitor, "GET", srv.URL+"/api", "", nil)
	if ops, n := strings.Count(ref, `id="op-`), strings.Count(ref, "<h3"); ops == 0 || n < ops {
		t.Errorf("/api has %d h3 headings for %d operations", n, ops)
	}
}
