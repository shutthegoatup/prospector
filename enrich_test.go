package main

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
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
		email, ok := f.found[q.Get("first_name")+" "+q.Get("last_name")+"@"+q.Get("domain")]
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
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	u := must(s.CreateUser("ada@example.com", "Ada Lovelace", "correct horse battery"))
	if err := s.AddMember(o.ID, u.ID, "owner", u.Name); err != nil {
		t.Fatal(err)
	}
	f := newFakeHunter(t)
	srv := httptest.NewServer((&App{s: s, vendorClient: f.srv.Client(), vendorBase: f.srv.URL}).Routes())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	res, err := client.PostForm(srv.URL+"/login", url.Values{"email": {"ada@example.com"}, "password": {"correct horse battery"}})
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("login: %v %v", res.Status, err)
	}
	return s, o, srv.URL + "/o/alpha/api/v1", client, f
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
	// Asked again, the same find isn't raised twice, but it is counted.
	if _, out := enrich(t, client, api, hannah.ID, "email"); out.Results[0].Outcome != "has" {
		t.Errorf("asked twice: %+v", out)
	}
	if used := s.EnricherUsed(o.ID, "hunter", time.Now()); used != 2 {
		t.Errorf("%d lookups counted, want 2", used)
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
	o.Settings.NeverContact = "nordkap.example\nkai@bergholm.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	for _, who := range []*Contact{listed, declined} {
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
