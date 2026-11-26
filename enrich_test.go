package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeHunter answers Email Finder from a table of "first last@domain" to address.
type fakeHunter struct {
	srv   *httptest.Server
	mu    sync.Mutex
	asked []url.Values
	found map[string]string
}

const hunterTestKey = "hunter-test-key"

func newFakeHunter(t *testing.T) *fakeHunter {
	f := &fakeHunter{found: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.asked = append(f.asked, r.URL.Query())
		f.mu.Unlock()
		w.Header().Set("content-type", "application/json")
		if r.URL.Path != "/v2/email-finder" || r.Header.Get("X-API-KEY") != hunterTestKey || r.URL.Query().Has("api_key") {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"errors":[{"id":"authentication_failed","code":401,"details":"No user found for the API key supplied"}]}`))
			return
		}
		q := r.URL.Query()
		email, ok := f.found[q.Get("full_name")+"@"+q.Get("domain")]
		if !ok {
			w.Write([]byte(`{"data":{"email":null,"score":null,"verification":{"status":null}}}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"email": email, "score": 94, "verification": map[string]any{"status": "valid"}}})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeHunter) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.asked)
}

// enrichSignedIn is signedIn with the vendors reached at a fake.
func enrichSignedIn(t *testing.T) (*Store, *Org, string, *http.Client, *fakeHunter) {
	t.Helper()
	f := newFakeHunter(t)
	s, o, api, client := vendorsAt(t, f.srv)
	return s, o, api, client, f
}

// vendorsAt is signedIn with every vendor reached at one fake.
func vendorsAt(t *testing.T, fake *httptest.Server) (*Store, *Org, string, *http.Client) {
	t.Helper()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	u := must(s.CreateUser("ada@example.com", "Ada Lovelace", "correct horse battery"))
	if err := s.AddMember(o.ID, u.ID, "owner", u.Name); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer((&App{s: s, vendorClient: fake.Client(), vendorBase: fake.URL}).Routes())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	res, err := signInWith(client, srv.URL, "ada@example.com", "correct horse battery")
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("login: %v %v", res.Status, err)
	}
	return s, o, srv.URL + "/o/alpha/api/v1", client
}

type enrichAnswer struct {
	Results []EnrichAsked `json:"results"`
}

func enrich(t *testing.T, client *http.Client, api string, id int64, fields ...string) (int, enrichAnswer) {
	t.Helper()
	code, body := call(t, client, "POST", api+"/contacts/"+itoa(id)+"/enrich", "application/json", map[string]any{"fields": fields})
	var out enrichAnswer
	json.Unmarshal([]byte(body), &out)
	if code != http.StatusOK && len(out.Results) == 0 {
		out.Results = []EnrichAsked{{Detail: body}}
	}
	return code, out
}

func atCompany(t *testing.T, s *Store, o *Org, name, company, domain string) *Contact {
	t.Helper()
	c := person(t, s, o, name, company, "", false)
	if err := s.db.Model(&Company{}).Where("id = ?", *c.CompanyID).Update("domain", domain).Error; err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAVendorsKeyIsSealedAndNeverShown(t *testing.T) {
	s, o, api, client, _ := enrichSignedIn(t)
	if code, body := call(t, client, "PUT", api+"/enrichers", "application/json", map[string]any{"kind": "hunter", "monthly_limit": 10}); code != http.StatusBadRequest || !strings.Contains(body, "api_key is needed") {
		t.Errorf("a vendor without a key: %d %s", code, body)
	}
	if code, body := call(t, client, "PUT", api+"/enrichers", "application/json", map[string]any{"kind": "hunter", "api_key": hunterTestKey, "monthly_limit": 10}); code != http.StatusOK || strings.Contains(body, hunterTestKey) {
		t.Fatalf("saving: %d %s", code, body)
	}
	var e Enricher
	s.db.Where("org_id = ?", o.ID).First(&e)
	if e.APIKey == hunterTestKey || !sealed(e.APIKey) {
		t.Errorf("the key is kept in clear: %q", e.APIKey)
	}
	// Saved again without a key, it keeps the one it has.
	call(t, client, "PUT", api+"/enrichers", "application/json", map[string]any{"kind": "hunter", "monthly_limit": 20})
	code, body := call(t, client, "GET", api+"/enrichers", "", nil)
	if code != http.StatusOK || strings.Contains(body, hunterTestKey) || !strings.Contains(body, `"has_key":true`) || !strings.Contains(body, `"monthly_limit":20`) || !strings.Contains(body, `"fields":["email"]`) {
		t.Errorf("the list: %d %s", code, body)
	}
	if code, _ := call(t, client, "PUT", api+"/enrichers", "application/json", map[string]any{"kind": "nosuch", "api_key": "x", "monthly_limit": 1}); code != http.StatusBadRequest {
		t.Errorf("an unknown vendor: %d", code)
	}
	ex := must(s.ExportOrg(o.ID, "Ada Lovelace"))
	if raw, _ := json.Marshal(ex); strings.Contains(string(raw), e.APIKey) || len(ex.Enrichers) != 1 || ex.Enrichers[0].MonthlyLimit != 20 {
		t.Errorf("the export: %+v", ex.Enrichers)
	}
}

func TestAVendorsFindWaitsForAPersonMarkedWithTheVendor(t *testing.T) {
	s, o, api, client, f := enrichSignedIn(t)
	hannah := atCompany(t, s, o, "Hannah Pryce", "Larkmoor Yachts", "larkmoor.example")
	f.found["Hannah Pryce@larkmoor.example"] = "hannah.pryce@larkmoor.example"

	if code, out := enrich(t, client, api, hannah.ID, "email"); code != http.StatusOK || out.Results[0].Outcome != "skipped" || f.calls() != 0 {
		t.Errorf("with no vendor: %d %+v", code, out)
	}
	call(t, client, "PUT", api+"/enrichers", "application/json", map[string]any{"kind": "hunter", "api_key": hunterTestKey, "monthly_limit": 10})
	code, out := enrich(t, client, api, hannah.ID, "email", "phone")
	if code != http.StatusOK || len(out.Results) != 2 || out.Results[0] != (EnrichAsked{Field: "email", Vendor: "hunter", Outcome: "suggested"}) || out.Results[1].Outcome != "skipped" {
		t.Fatalf("asking: %d %+v", code, out)
	}
	var sg Suggestion
	s.db.Where("contact_id = ?", hannah.ID).First(&sg)
	if sg.Value != "hannah.pryce@larkmoor.example" || sg.Vendor != "hunter" || sg.EmailStatus != "verified" || sg.SourceURL != "" || sg.By != "Ada Lovelace" {
		t.Errorf("the suggestion: %+v", sg)
	}
	if got := deref(reread(t, s, hannah.ID).Email); got != "" {
		t.Errorf("the find was filled in before anyone accepted it: %q", got)
	}
	var events []Event
	s.db.Where("contact_id = ? and action = ?", hannah.ID, "enrich:email").Find(&events)
	if len(events) != 1 || deref(events[0].Value) != "hunter: found" {
		t.Errorf("the history: %+v", events)
	}
	// Asked again soon after, the vendor isn't asked and nothing is spent.
	if _, out := enrich(t, client, api, hannah.ID, "email"); out.Results[0].Outcome != "skipped" || !strings.Contains(out.Results[0].Detail, "Hunter was asked on") {
		t.Errorf("asked twice: %+v", out)
	}
	if used := s.EnricherUsed(o.ID, "hunter", time.Now()); used != 1 || f.calls() != 1 {
		t.Errorf("%d lookups counted and %d made, want 1", used, f.calls())
	}
	// Long enough after, it is.
	s.db.Model(&Event{}).Where("contact_id = ? and action = ?", hannah.ID, "enrich:email").Update("at", time.Now().Add(-enrichAgain-time.Hour))
	if _, out := enrich(t, client, api, hannah.ID, "email"); out.Results[0].Outcome != "has" || f.calls() != 2 {
		t.Errorf("asked a month later: %+v after %d calls", out, f.calls())
	}
	if err := s.DecideSuggestion(o.ID, sg.ID, SuggestionAccepted, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	c := reread(t, s, hannah.ID)
	if deref(c.Email) != "hannah.pryce@larkmoor.example" || c.EmailStatus != "verified" {
		t.Errorf("accepted: %q %q", deref(c.Email), c.EmailStatus)
	}
	// Now they have it, nobody is asked.
	before := f.calls()
	if _, out := enrich(t, client, api, hannah.ID, "email"); out.Results[0].Outcome != "has" || f.calls() != before {
		t.Errorf("someone who has an email: %+v", out)
	}
}

func TestAVendorIsAskedNoMoreThanItsMonthAllows(t *testing.T) {
	s, o, api, client, f := enrichSignedIn(t)
	call(t, client, "PUT", api+"/enrichers", "application/json", map[string]any{"kind": "hunter", "api_key": hunterTestKey, "monthly_limit": 1})
	a := atCompany(t, s, o, "Tom Ellery", "Larkmoor Yachts", "larkmoor.example")
	b := atCompany(t, s, o, "Jo Marsh", "Nordkap Boats", "nordkap.example")
	if _, out := enrich(t, client, api, a.ID, "email"); out.Results[0].Outcome != "none" {
		t.Errorf("a miss: %+v", out)
	}
	_, out := enrich(t, client, api, b.ID, "email")
	if out.Results[0].Outcome != "skipped" || !strings.Contains(out.Results[0].Detail, "used this month's lookups") || f.calls() != 1 {
		t.Errorf("past the limit: %+v after %d calls", out, f.calls())
	}
	var events []Event
	s.db.Where("contact_id = ? and action = ?", a.ID, "enrich:email").Find(&events)
	if len(events) != 1 || deref(events[0].Value) != "hunter: none found" {
		t.Errorf("a miss in the history: %+v", events)
	}
	// A limit of 0 stops it being asked at all.
	call(t, client, "PUT", api+"/enrichers", "application/json", map[string]any{"kind": "hunter", "monthly_limit": 0})
	if _, out := enrich(t, client, api, a.ID, "email"); out.Results[0].Outcome != "skipped" || f.calls() != 1 {
		t.Errorf("a limit of 0: %+v", out)
	}
}

func TestNobodyTheOrganisationMustntContactIsLookedUp(t *testing.T) {
	s, o, api, client, f := enrichSignedIn(t)
	call(t, client, "PUT", api+"/enrichers", "application/json", map[string]any{"kind": "hunter", "api_key": hunterTestKey, "monthly_limit": 10})
	listed := atCompany(t, s, o, "Sven Dahl", "Nordkap Boats", "nordkap.example")
	declined := atCompany(t, s, o, "Said No", "Quayside Ltd", "quayside.example")
	if err := s.Mark(o.ID, declined.ID, "declined", today(), "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	found := atCompany(t, s, o, "Kai Berg", "Bergholm", "bergholm.example")
	f.found["Kai Berg@bergholm.example"] = "kai@bergholm.example"
	mailed := person(t, s, o, "Mia Holt", "Holt Marine", "mia@holt.example", false)
	o.Settings.NeverContact = "nordkap.example\nkai@bergholm.example\nmia@holt.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	for _, who := range []*Contact{listed, declined, mailed} {
		if code, out := enrich(t, client, api, who.ID, "email"); code != http.StatusConflict {
			t.Errorf("%s: %d %+v", deref(who.Person), code, out)
		}
	}
	if f.calls() != 0 {
		t.Errorf("the vendor was asked %d times about people it mustn't hear of", f.calls())
	}
	if _, out := enrich(t, client, api, found.ID, "email"); out.Results[0].Outcome != "skipped" || !strings.Contains(out.Results[0].Detail, "never-contact") {
		t.Errorf("a find on the list: %+v", out)
	}
	var n int64
	if s.db.Model(&Suggestion{}).Count(&n); n != 0 {
		t.Errorf("%d suggestions kept from finds that may not be", n)
	}

	// Research says who can't be looked up, so it offers no lookup the server would refuse.
	s.db.Model(&Company{}).Where("org_id = ?", o.ID).Update("stage", CompanyReview)
	code, body := call(t, client, "GET", api+"/research?filter=review", "", nil)
	var list struct {
		Items []struct {
			People []struct {
				Name       string `json:"name"`
				Unlookable string `json:"unlookable"`
			} `json:"people"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &list); code != http.StatusOK || err != nil {
		t.Fatalf("research: %d %v %s", code, err, body)
	}
	got := map[string]string{}
	for _, c := range list.Items {
		for _, p := range c.People {
			got[p.Name] = p.Unlookable
		}
	}
	if got["Kai Berg"] != "" || !strings.Contains(got["Said No"], "declined") || !strings.Contains(got["Mia Holt"], "never-contact") {
		t.Errorf("who can't be looked up: %q", got)
	}
}

func TestOnlyAKeyGivenTheScopeSpendsOnVendors(t *testing.T) {
	s, o, api, client, _ := enrichSignedIn(t)
	call(t, client, "PUT", api+"/enrichers", "application/json", map[string]any{"kind": "hunter", "api_key": hunterTestKey, "monthly_limit": 10})
	c := atCompany(t, s, o, "Tom Ellery", "Larkmoor Yachts", "larkmoor.example")
	for scope, want := range map[string]int{"research:write": http.StatusForbidden, "enrich:spend": http.StatusOK} {
		secret, _, err := s.CreateKey(o.ID, "bot "+scope, []string{"read", scope}, "Ada Lovelace")
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest("POST", api+"/contacts/"+itoa(c.ID)+"/enrich", strings.NewReader(`{"fields":["email"]}`))
		req.Header.Set("authorization", "Bearer "+secret)
		req.Header.Set("content-type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != want {
			t.Errorf("a key with %s: %d, want %d", scope, res.StatusCode, want)
		}
	}
}

func reread(t *testing.T, s *Store, id int64) *Contact {
	t.Helper()
	var c Contact
	if err := s.db.First(&c, id).Error; err != nil {
		t.Fatal(err)
	}
	return &c
}

func TestAVendorIsToldTheWholeNameOfSomeoneAnAgentAdded(t *testing.T) {
	s, o, api, client, f := enrichSignedIn(t)
	call(t, client, "PUT", api+"/enrichers", "application/json", map[string]any{"kind": "hunter", "api_key": hunterTestKey, "monthly_limit": 10})
	must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, AddCompanyIn{Name: "Bergholm", Domain: "bergholm.example"}))
	added := must(s.AgentAddContact(o, Actor{Name: "agent:scout"}, AddContactIn{Company: "Bergholm", Name: "Dr. Kai van der Berg", Title: "Head of Digital",
		SourceURL: "https://bergholm.example/team"}))
	f.found["Dr. Kai van der Berg@bergholm.example"] = "kai.vanderberg@bergholm.example"
	if _, out := enrich(t, client, api, added.ID, "email"); out.Results[0].Outcome != "suggested" {
		t.Errorf("%+v", out)
	}
	if q := f.asked[0]; q.Get("full_name") != "Dr. Kai van der Berg" || q.Get("domain") != "bergholm.example" || q.Has("first_name") || q.Has("api_key") {
		t.Errorf("Hunter was asked %v", q)
	}
}

func TestAnAgentWithTheScopeCanEnrichThroughMCP(t *testing.T) {
	s, o, api, client, f := enrichSignedIn(t)
	call(t, client, "PUT", api+"/enrichers", "application/json", map[string]any{"kind": "hunter", "api_key": hunterTestKey, "monthly_limit": 10})
	c := atCompany(t, s, o, "Hannah Pryce", "Larkmoor Yachts", "larkmoor.example")
	f.found["Hannah Pryce@larkmoor.example"] = "hannah.pryce@larkmoor.example"
	site := strings.TrimSuffix(api, "/o/alpha/api/v1")
	research, _, _ := s.CreateKey(o.ID, "researcher", []string{"read", "research:write"}, "Ada Lovelace")
	tools := must(mcpSession(t, site, research).ListTools(context.Background(), nil))
	for _, tool := range tools.Tools {
		if tool.Name == "enrich_contact" {
			t.Error("a key without enrich:spend is offered enrich_contact")
		}
	}
	key, _, _ := s.CreateKey(o.ID, "enricher", []string{"read", "enrich:spend"}, "Ada Lovelace")
	res, err := mcpSession(t, site, key).CallTool(context.Background(), &mcp.CallToolParams{Name: "enrich_contact",
		Arguments: map[string]any{"contact_id": c.ID, "fields": []string{"email"}}})
	if err != nil || res.IsError {
		t.Fatalf("calling: %v %+v", err, res)
	}
	var sg Suggestion
	if s.db.Where("contact_id = ?", c.ID).First(&sg); sg.Vendor != "hunter" || sg.By != "agent:enricher" {
		t.Errorf("the suggestion: %+v", sg)
	}
}

func TestADatabaseFromBeforeVendorsOpens(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "old.sqlite")
	if pg := os.Getenv("PROSPECTOR_TEST_PG"); pg != "" {
		dsn = pg
	}
	old := must(Open(dsn))
	if err := old.db.Migrator().DropTable(models...); err != nil {
		t.Fatal(err)
	}
	if err := old.db.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	o := mustOrg(t, old, "alpha")
	c := person(t, old, o, "Hannah Pryce", "Larkmoor Yachts", "", false)
	if err := old.db.Create(&Suggestion{OrgID: o.ID, ContactID: c.ID, Field: "phone", Value: "+44 20 7946 0111", By: "agent:scout"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := old.db.Migrator().DropColumn(&Suggestion{}, "vendor"); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []any{&Enricher{}, &EnrichUsage{}} {
		if err := old.db.Migrator().DropTable(gone); err != nil {
			t.Fatal(err)
		}
	}
	s := must(Open(dsn))
	var sg Suggestion
	if err := s.db.Where("contact_id = ?", c.ID).First(&sg).Error; err != nil || sg.Vendor != "" || sg.Value != "+44 20 7946 0111" {
		t.Errorf("a suggestion from before: %+v %v", sg, err)
	}
	if err := s.db.Create(&Suggestion{OrgID: o.ID, ContactID: c.ID, Field: "email", Value: "hannah@larkmoor.example", Vendor: "hunter", By: "Ada Lovelace"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Create(&Enricher{OrgID: o.ID, Kind: "hunter", MonthlyLimit: 5}).Error; err != nil {
		t.Errorf("a vendor after the upgrade: %v", err)
	}
	var kinds []string
	if s.db.Model(&Suggestion{}).Where("contact_id = ?", c.ID).Order("id").Pluck("vendor", &kinds); len(kinds) != 2 || kinds[1] != "hunter" {
		t.Errorf("vendors after the upgrade: %v", kinds)
	}
}

func TestResearchSaysWhatVendorsFoundAndWhenTheyCanBeAskedAgain(t *testing.T) {
	s, o, api, client, f := enrichSignedIn(t)
	s.dns = &fakeDNS{mx: map[string][]*net.MX{"larkmoor.example": {{Host: "mx.larkmoor.example."}}}}
	hannah := atCompany(t, s, o, "Hannah Pryce", "Larkmoor Yachts", "larkmoor.example")
	ola := person(t, s, o, "Ola Brandt", "Larkmoor Yachts", "", false)
	tove := person(t, s, o, "Tove Lind", "Larkmoor Yachts", "", false)
	f.found["Hannah Pryce@larkmoor.example"] = "hannah.pryce@larkmoor.example"
	f.found["Tove Lind@larkmoor.example"] = "tove@gone.example"
	call(t, client, "PUT", api+"/enrichers", "application/json", map[string]any{"kind": "hunter", "api_key": hunterTestKey, "monthly_limit": 10})
	for _, id := range []int64{hannah.ID, ola.ID, tove.ID} {
		enrich(t, client, api, id, "email")
	}
	// Tove's address can't be kept, and isn't paid for twice.
	if _, out := enrich(t, client, api, tove.ID, "email"); out.Results[0].Outcome != "skipped" || f.calls() != 3 {
		t.Errorf("asked again about an address that can't be kept: %+v after %d calls", out, f.calls())
	}

	code, body := call(t, client, "GET", api+"/research?filter=details", "", nil)
	var list struct {
		Items []struct {
			People []struct {
				Name     string `json:"name"`
				HasEmail bool   `json:"has_email"`
				Pending  []struct {
					Vendor      string `json:"vendor"`
					EmailStatus string `json:"email_status"`
				} `json:"pending"`
				Lookups []struct {
					Field   string    `json:"field"`
					Vendor  string    `json:"vendor"`
					Outcome string    `json:"outcome"`
					At      time.Time `json:"at"`
					AgainAt time.Time `json:"again_at"`
				} `json:"lookups"`
			} `json:"people"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &list); code != http.StatusOK || err != nil || len(list.Items) != 1 {
		t.Fatalf("research: %d %v %s", code, err, body)
	}
	got := map[string]string{}
	for _, p := range list.Items[0].People {
		line := fmt.Sprint(p.HasEmail)
		for _, d := range p.Pending {
			line += " pending " + d.Vendor + " " + d.EmailStatus
		}
		for _, l := range p.Lookups {
			line += " " + l.Field + " " + l.Vendor + " " + l.Outcome
			if l.AgainAt.Sub(l.At) != enrichAgain {
				t.Errorf("%s can be asked again %v after", p.Name, l.AgainAt.Sub(l.At))
			}
		}
		got[p.Name] = line
	}
	want := map[string]string{
		"Hannah Pryce": "false pending hunter verified email hunter found",
		"Ola Brandt":   "false email hunter none",
		"Tove Lind":    "false email hunter not_kept",
	}
	if !maps.Equal(got, want) {
		t.Errorf("people:\n got %v\nwant %v", got, want)
	}

	// Once the 30 days are up, the lookup is no longer listed.
	s.db.Model(&Event{}).Where("contact_id = ?", ola.ID).Update("at", time.Now().Add(-enrichAgain-time.Hour))
	if _, body := call(t, client, "GET", api+"/research?filter=details", "", nil); strings.Contains(body, `"outcome":"none"`) {
		t.Errorf("an old lookup is still listed: %s", body)
	}
}
