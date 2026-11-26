package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const prospeoTestKey = "prospeo-test-key"

// fakeProspeo answers Enrich Person from a table of "full name@company website" to
// a person, the way Prospeo shows them: what wasn't paid for is masked. Hunter, at
// the same address, never finds anyone.
type fakeProspeo struct {
	srv    *httptest.Server
	mu     sync.Mutex
	asked  []map[string]any
	people map[string]map[string]any
}

func newFakeProspeo(t *testing.T) *fakeProspeo {
	f := &fakeProspeo{people: map[string]map[string]any{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		if r.URL.Path == "/v2/email-finder" {
			w.Write([]byte(`{"data":{"email":null}}`))
			return
		}
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		f.asked = append(f.asked, in)
		f.mu.Unlock()
		if r.Method != http.MethodPost || r.URL.Path != "/enrich-person" || r.Header.Get("X-KEY") != prospeoTestKey {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":true,"error_code":"INVALID_API_KEY"}`))
			return
		}
		data, _ := in["data"].(map[string]any)
		name, _ := data["full_name"].(string)
		site, _ := data["company_website"].(string)
		p, ok := f.people[name+"@"+site]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":true,"error_code":"NO_MATCH"}`))
			return
		}
		mobile := map[string]any{"status": "UNAVAILABLE", "revealed": false}
		if m, ok := p["mobile"].(string); ok {
			mobile = map[string]any{"status": "VERIFIED", "revealed": false, "mobile": "+44 7***-******"}
			if in["only_verified_mobile"] == true {
				mobile["revealed"], mobile["mobile"] = true, m
			}
		}
		if _, ok := p["mobile"]; !ok && in["only_verified_mobile"] == true {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":true,"error_code":"NO_MATCH"}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"error": false, "person": map[string]any{
			"full_name": name, "linkedin_url": p["linkedin"],
			"email":  map[string]any{"status": p["email_status"], "revealed": true, "email": p["email"]},
			"mobile": mobile,
		}})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeProspeo) last() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.asked) == 0 {
		return nil
	}
	return f.asked[len(f.asked)-1]
}

func TestProspeoFindsAMobileAnEmailAndAProfile(t *testing.T) {
	f := newFakeProspeo(t)
	s, o, api, client := vendorsAt(t, f.srv)
	nell := atCompany(t, s, o, "Nell Achterberg", "Wrenfield Kitchens", "wrenfield.example")
	f.people["Nell Achterberg@wrenfield.example"] = map[string]any{
		"email": "nell@wrenfield.example", "email_status": "VERIFIED", "mobile": "+447911123456", "linkedin": "https://www.linkedin.com/in/nell-achterberg-example",
	}
	call(t, client, "PUT", api+"/enrichers", "application/json", map[string]any{"kind": "prospeo", "api_key": prospeoTestKey, "monthly_limit": 10})

	code, out := enrich(t, client, api, nell.ID, "phone", "email", "linkedin")
	if code != http.StatusOK || len(out.Results) != 3 {
		t.Fatalf("asking: %d %+v", code, out)
	}
	for _, r := range out.Results {
		if r.Outcome != "suggested" || r.Vendor != "prospeo" {
			t.Errorf("%s: %+v", r.Field, r)
		}
	}
	var found []Suggestion
	s.db.Where("contact_id = ?", nell.ID).Order("field").Find(&found)
	got := map[string]string{}
	for _, sg := range found {
		got[sg.Field] = sg.Value + " " + sg.EmailStatus + " " + sg.Vendor
	}
	want := map[string]string{
		"email":    "nell@wrenfield.example verified prospeo",
		"phone":    "+447911123456  prospeo",
		"linkedin": "https://www.linkedin.com/in/nell-achterberg-example  prospeo",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q, want %q", k, got[k], v)
		}
	}
	// The mobile is the dear one, asked for only when it's the mobile that's wanted.
	f.mu.Lock()
	mobiles := 0
	for _, in := range f.asked {
		if in["only_verified_mobile"] == true {
			mobiles++
		}
		data := in["data"].(map[string]any)
		if data["full_name"] != "Nell Achterberg" || data["company_website"] != "wrenfield.example" {
			t.Errorf("Prospeo was told %v", data)
		}
	}
	f.mu.Unlock()
	if mobiles != 1 {
		t.Errorf("a mobile was paid for %d times, want 1", mobiles)
	}
	if used := s.EnricherUsed(o.ID, "prospeo", time.Now()); used != 3 {
		t.Errorf("%d lookups counted, want 3", used)
	}
}

func TestProspeoMissesFailuresAndMaskedFinds(t *testing.T) {
	f := newFakeProspeo(t)
	s, o, api, client := vendorsAt(t, f.srv)
	call(t, client, "PUT", api+"/enrichers", "application/json", map[string]any{"kind": "prospeo", "api_key": prospeoTestKey, "monthly_limit": 10})

	nobody := atCompany(t, s, o, "Piet Varga", "Wrenfield Kitchens", "wrenfield.example")
	if _, out := enrich(t, client, api, nobody.ID, "phone"); out.Results[0].Outcome != "none" || out.Results[0].Detail != "" {
		t.Errorf("no match: %+v", out)
	}
	// Found, but without a mobile: nothing is kept.
	noMobile := atCompany(t, s, o, "Ines Moreau", "Wrenfield Kitchens", "wrenfield.example")
	f.people["Ines Moreau@wrenfield.example"] = map[string]any{"email": "ines@wrenfield.example", "email_status": "VERIFIED"}
	if _, out := enrich(t, client, api, noMobile.ID, "phone"); out.Results[0].Outcome != "none" {
		t.Errorf("no mobile: %+v", out)
	}
	// A masked address isn't one.
	masked := atCompany(t, s, o, "Bram Holt", "Wrenfield Kitchens", "wrenfield.example")
	f.people["Bram Holt@wrenfield.example"] = map[string]any{"email": "bram.****@wrenfield.example", "email_status": "VERIFIED"}
	if _, out := enrich(t, client, api, masked.ID, "email"); out.Results[0].Outcome != "none" {
		t.Errorf("masked: %+v", out)
	}
	// An impossible number isn't kept.
	wrong := atCompany(t, s, o, "Saskia Orr", "Wrenfield Kitchens", "wrenfield.example")
	f.people["Saskia Orr@wrenfield.example"] = map[string]any{"email": "", "email_status": "UNAVAILABLE", "mobile": "+44 12"}
	if _, out := enrich(t, client, api, wrong.ID, "phone"); out.Results[0].Outcome != "skipped" {
		t.Errorf("an impossible number: %+v", out)
	}
	var n int64
	s.db.Model(&Suggestion{}).Count(&n)
	if n != 0 {
		t.Errorf("%d suggestions kept, want none", n)
	}

	call(t, client, "PUT", api+"/enrichers", "application/json", map[string]any{"kind": "prospeo", "api_key": "a-wrong-key", "monthly_limit": 10})
	other := atCompany(t, s, o, "Ola Brandt", "Wrenfield Kitchens", "wrenfield.example")
	if _, out := enrich(t, client, api, other.ID, "email"); out.Results[0].Outcome != "failed" || !strings.Contains(out.Results[0].Detail, "the key was refused") {
		t.Errorf("a wrong key: %+v", out)
	}
	if f.last()["data"] == nil {
		t.Error("the person was sent outside data")
	}
}

func TestAVendorThatFindsNothingGivesWayToTheNext(t *testing.T) {
	f := newFakeProspeo(t)
	s, o, api, client := vendorsAt(t, f.srv)
	call(t, client, "PUT", api+"/enrichers", "application/json", map[string]any{"kind": "hunter", "api_key": hunterTestKey, "position": 0, "monthly_limit": 10})
	call(t, client, "PUT", api+"/enrichers", "application/json", map[string]any{"kind": "prospeo", "api_key": prospeoTestKey, "position": 1, "monthly_limit": 10})
	nell := atCompany(t, s, o, "Nell Achterberg", "Wrenfield Kitchens", "wrenfield.example")
	f.people["Nell Achterberg@wrenfield.example"] = map[string]any{"email": "nell@wrenfield.example", "email_status": "UNAVAILABLE"}

	if _, out := enrich(t, client, api, nell.ID, "email"); out.Results[0] != (EnrichAsked{Field: "email", Vendor: "prospeo", Outcome: "suggested"}) {
		t.Fatalf("asked: %+v", out)
	}
	var sg Suggestion
	s.db.Where("contact_id = ?", nell.ID).First(&sg)
	if sg.Vendor != "prospeo" || sg.EmailStatus != "guess" {
		t.Errorf("the suggestion: %+v", sg)
	}
	if s.EnricherUsed(o.ID, "hunter", time.Now()) != 1 || s.EnricherUsed(o.ID, "prospeo", time.Now()) != 1 {
		t.Error("each vendor asked should count one lookup")
	}
}
