package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/store"
	"github.com/shutthegoatup/prospector/internal/tickets"
)

const formSite = "https://www.shop.example"

func takeEnquiriesFrom(t *testing.T, s *Store, o *store.Org, sites string) {
	t.Helper()
	o.Settings.EnquirySites = sites
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
}

// fromSite sends what a browser on another site would.
func fromSite(t *testing.T, method, u, origin string, body any) (*http.Response, string) {
	t.Helper()
	var text []byte
	if body != nil {
		text, _ = json.Marshal(body)
	}
	req, _ := http.NewRequest(method, u, bytes.NewReader(text))
	if origin != "" {
		req.Header.Set("Origin", origin)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
	}
	if method == "POST" {
		req.Header.Set("content-type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	text, _ = io.ReadAll(res.Body)
	return res, string(text)
}

func doRequest(t *testing.T, req *http.Request) (int, string) {
	t.Helper()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	text, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(text)
}

func TestFormEnquiryIsKept(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	intake := srv.URL + "/o/alpha/intake"
	enquiry := tickets.EnquiryIn{Kind: "demo", Name: "Hannah Pryce", Email: "Hannah@Quay.example", Company: "Quay Marine",
		Message: "Could we see it running?", Page: formSite + "/pricing"}

	if res, _ := fromSite(t, "POST", intake, formSite, enquiry); res.StatusCode != http.StatusForbidden {
		t.Fatalf("an organisation that lists no sites took an enquiry: %d", res.StatusCode)
	}
	takeEnquiriesFrom(t, s, o, formSite+"/contact\nhttps://other.example")

	res, _ := fromSite(t, "OPTIONS", intake, formSite, nil)
	if res.StatusCode != http.StatusNoContent || res.Header.Get("Access-Control-Allow-Origin") != formSite ||
		!strings.Contains(res.Header.Get("Access-Control-Allow-Headers"), "content-type") {
		t.Fatalf("preflight: %d %v", res.StatusCode, res.Header)
	}
	for name, origin := range map[string]string{"another site": "https://evil.example", "no site at all": ""} {
		if res, _ := fromSite(t, "POST", intake, origin, enquiry); res.StatusCode != http.StatusForbidden || res.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("an enquiry from %s: %d", name, res.StatusCode)
		}
	}
	if res, _ := fromSite(t, "OPTIONS", intake, "https://evil.example", nil); res.StatusCode != http.StatusForbidden {
		t.Errorf("preflight from another site: %d", res.StatusCode)
	}

	// Sent twice, as a form retrying after a dropped answer would: one enquiry.
	for range 2 {
		if res, _ := fromSite(t, "POST", intake, formSite, enquiry); res.StatusCode != http.StatusOK || res.Header.Get("Access-Control-Allow-Origin") != formSite {
			t.Fatalf("an enquiry from a listed site: %d", res.StatusCode)
		}
	}
	rows, err := firstOf(s.Enquiries(o.ID, tickets.EnquiryFilter{}, 1))
	if err != nil || len(rows) != 1 {
		t.Fatalf("stored %d enquiries, %v", len(rows), err)
	}
	got := rows[0]
	if got.Kind != "demo" || got.Email != "hannah@quay.example" || got.Site != formSite || got.Via != "form" || got.Spam || got.Message != enquiry.Message {
		t.Errorf("stored as %+v", got)
	}
	code, page := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/enquiries", "", nil)
	if code != http.StatusOK || !strings.Contains(page, "Could we see it running?") || !strings.Contains(page, "Hannah Pryce") {
		t.Errorf("the enquiries the page lists don't hold it: %d %s", code, page)
	}

	// The listed site still can't do anything else here.
	if res, _ := fromSite(t, "POST", srv.URL+"/o/alpha/api/v1/people", formSite, map[string]string{"name": "Mallory"}); res.StatusCode != http.StatusForbidden {
		t.Errorf("a listed site reached another route: %d", res.StatusCode)
	}
}

func TestEnquiryOddities(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	takeEnquiriesFrom(t, s, o, formSite)
	intake := srv.URL + "/o/alpha/intake"

	if res, _ := fromSite(t, "POST", intake, formSite, tickets.EnquiryIn{Name: "Nobody", Message: "hello"}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("an enquiry with no email: %d", res.StatusCode)
	}
	long := strings.Repeat("é", 20000)
	if res, _ := fromSite(t, "POST", intake, formSite, tickets.EnquiryIn{Kind: "Partnership", Email: "omar@reef.example", Message: long}); res.StatusCode != http.StatusOK {
		t.Fatalf("an unknown kind and a long message: %d", res.StatusCode)
	}
	if res, _ := fromSite(t, "POST", intake, formSite, tickets.EnquiryIn{Kind: "sales", Email: "bot@spam.example", Message: "buy", Trap: "https://spam.example"}); res.StatusCode != http.StatusOK {
		t.Fatalf("a filled trap field: %d", res.StatusCode)
	}
	rows, _ := firstOf(s.Enquiries(o.ID, tickets.EnquiryFilter{}, 1))
	if len(rows) != 1 || rows[0].Kind != "other" || len([]rune(rows[0].Message)) != 10000 {
		t.Fatalf("kept %d, first %q with %d characters", len(rows), rows[0].Kind, len([]rune(rows[0].Message)))
	}
	spam, _ := firstOf(s.Enquiries(o.ID, tickets.EnquiryFilter{Spam: true}, 1))
	if len(spam) != 1 || spam[0].Email != "bot@spam.example" {
		t.Fatalf("spam kept apart: %+v", spam)
	}
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/enquiries", "", nil); strings.Contains(page, "bot@spam.example") {
		t.Error("spam shows among the enquiries")
	}
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/enquiries?spam=true", "", nil); !strings.Contains(page, "bot@spam.example") {
		t.Error("spam can't be looked at")
	}

	// One sender can't fill the list.
	var refused int
	for i := range enquiriesPerSender + 2 {
		res, _ := fromSite(t, "POST", intake, formSite, tickets.EnquiryIn{Email: "keen@reef.example", Message: fmt.Sprint("again ", i)})
		if res.StatusCode == http.StatusTooManyRequests {
			refused++
			if res.Header.Get("Retry-After") != "3600" {
				t.Fatalf("a refused sender isn't told when to try again: Retry-After %q", res.Header.Get("Retry-After"))
			}
		}
	}
	if refused != 2 {
		t.Errorf("%d of a sender's enquiries past the limit were refused, want 2", refused)
	}
	if res, _ := fromSite(t, "POST", intake, formSite, tickets.EnquiryIn{Email: "someone.else@reef.example", Message: "hello"}); res.StatusCode != http.StatusOK {
		t.Errorf("another sender was held up too: %d", res.StatusCode)
	}
}

func TestEnquiryFindsWhoSentIt(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	takeEnquiriesFrom(t, s, o, formSite)
	known, err := s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Quay Marine", Email: "hannah@quay.example"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	added, err := s.AgentAddCompany(o.ID, store.Actor{Name: "agent:bot"}, research.AddCompanyIn{Name: "Reef Boats", Domain: "reef.example"})
	if err != nil {
		t.Fatal(err)
	}
	intake := srv.URL + "/o/alpha/intake"
	for _, in := range []tickets.EnquiryIn{
		{Kind: "sales", Email: "HANNAH@quay.example", Message: "What does it cost?"},
		{Kind: "support", Email: "omar@reef.example", Message: "The upload stalls."},
		{Kind: "feature", Email: "stranger@elsewhere.example", Message: "Please add exports."},
	} {
		if res, _ := fromSite(t, "POST", intake, formSite, in); res.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d", in.Email, res.StatusCode)
		}
	}
	by := map[string]store.Enquiry{}
	rows, _ := firstOf(s.Enquiries(o.ID, tickets.EnquiryFilter{}, 1))
	for _, e := range rows {
		by[e.Email] = e
	}
	if e := by["hannah@quay.example"]; e.ContactID == nil || *e.ContactID != known.ID || e.CompanyID == nil || *e.CompanyID != *known.CompanyID {
		t.Errorf("a known person's enquiry isn't tied to them: %+v", e)
	}
	if e := by["omar@reef.example"]; e.ContactID != nil || e.CompanyID == nil || *e.CompanyID != added.ID {
		t.Errorf("an enquiry from a known company's domain isn't tied to it: %+v", e)
	}
	if e := by["stranger@elsewhere.example"]; e.ContactID != nil || e.CompanyID != nil {
		t.Errorf("a stranger's enquiry was tied to someone: %+v", e)
	}
	_, page := call(t, client, "GET", fmt.Sprintf("%s/o/alpha/api/v1/companies/%d/history", srv.URL, *known.CompanyID), "", nil)
	if !strings.Contains(page, "sent an enquiry · Hannah Pryce: Sales") {
		t.Errorf("the company's history doesn't mention the enquiry: %s", page)
	}
	if kinds, _ := firstOf(s.Enquiries(o.ID, tickets.EnquiryFilter{Kind: "support"}, 1)); len(kinds) != 1 {
		t.Errorf("filtering by kind found %d", len(kinds))
	}

	// Erasing someone takes what they wrote, matched to them or not.
	if _, err := s.AddEnquiry(o.ID, "form", tickets.EnquiryIn{Email: "hannah@quay.example", Message: "An older note"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&store.Enquiry{}).Where("message = ?", "An older note").Update("contact_id", nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.ErasePerson(o.ID, known.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	left, _ := firstOf(s.Enquiries(o.ID, tickets.EnquiryFilter{}, 1))
	for _, e := range left {
		if e.Email == "hannah@quay.example" {
			t.Errorf("an erased person's enquiry is still here: %q", e.Message)
		}
	}
	if len(left) != 2 {
		t.Errorf("%d enquiries left after erasing one person, want 2", len(left))
	}
}

func TestEnquiryFromAProduct(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	other := mustOrg(t, s, "beta")
	api := srv.URL + "/o/alpha/api/v1/enquiries"
	send := func(key string, in tickets.EnquiryIn) (int, string) {
		b, _ := json.Marshal(in)
		req, _ := http.NewRequest(http.MethodPost, api, strings.NewReader(string(b)))
		req.Header.Set("content-type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key)
		return doRequest(t, req)
	}
	reader, _, err := s.CreateKey(o.ID, "reader", []string{"read"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	writer, _, err := s.CreateKey(o.ID, "back office", []string{"enquiries:write"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	signup := tickets.EnquiryIn{Kind: "signup", Name: "Omar Reyes", Email: "omar@reef.example", Company: "Reef Boats", Site: "app", Trap: "https://reef.example"}
	if code, _ := send(reader, signup); code != http.StatusForbidden {
		t.Errorf("a key without the scope: %d", code)
	}
	code, body := send(writer, signup)
	if code != http.StatusOK || !strings.Contains(body, `"kind":"signup"`) {
		t.Fatalf("a key with the scope: %d %s", code, body)
	}
	rows, _ := firstOf(s.Enquiries(o.ID, tickets.EnquiryFilter{}, 1))
	if len(rows) != 1 || rows[0].Via != "agent:back office" || rows[0].Site != "app" || rows[0].Spam {
		t.Fatalf("stored as %+v", rows)
	}
	if theirs, _ := firstOf(s.Enquiries(other.ID, tickets.EnquiryFilter{}, 1)); len(theirs) != 0 {
		t.Error("another organisation sees it")
	}
	if err := s.DeleteEnquiry(other.ID, rows[0].ID); err == nil {
		t.Error("another organisation could delete it")
	}
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/enquiries", "", nil); !strings.Contains(page, `"via":"agent:back office"`) {
		t.Errorf("the enquiries don't say which key sent it: %s", page)
	}
	if code, body := call(t, client, "DELETE", srv.URL+"/o/alpha/api/v1/enquiries/"+itoa(rows[0].ID), "", nil); code != http.StatusNoContent {
		t.Fatalf("an owner deleting an enquiry: %d %s", code, body)
	}
	if left, _ := firstOf(s.Enquiries(o.ID, tickets.EnquiryFilter{}, 1)); len(left) != 0 {
		t.Error("a deleted enquiry is still here")
	}
}

func TestEnquirySitesSetting(t *testing.T) {
	t.Parallel()
	_, _, srv, client := signedIn(t)
	save := func(sites string) (int, string) {
		return call(t, client, "PATCH", srv.URL+"/o/alpha/api/v1/settings", "application/json",
			map[string]any{"name": "Alpha", "color": "#56c5f4", "enquiry_sites": sites})
	}
	if code, body := save("shop.example"); code != http.StatusBadRequest || !strings.Contains(body, "isn't a site address") {
		t.Errorf("a site without a scheme was saved: %d %s", code, body)
	}
	if code, body := save(formSite + "\nhttp://localhost:4322"); code != http.StatusOK {
		t.Fatalf("saving two sites: %d %s", code, body)
	}
	// The page shows where they post from its own address; the sites come from the settings.
	_, page := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/settings", "", nil)
	if !strings.Contains(page, formSite) || !strings.Contains(page, "http://localhost:4322") {
		t.Errorf("the settings don't hold the sites: %s", page)
	}
	if res, _ := fromSite(t, "POST", srv.URL+"/o/alpha/intake", "http://localhost:4322", tickets.EnquiryIn{Email: "dev@shop.example"}); res.StatusCode != http.StatusOK {
		t.Errorf("an enquiry from the second site: %d", res.StatusCode)
	}
}

func TestEnquiryGuardStaysBounded(t *testing.T) {
	t.Parallel()
	var g enquiryGuard
	for i := range maxTracked {
		if !g.allow(fmt.Sprint("sender ", i), enquiriesPerSender) {
			t.Fatalf("sender %d was refused", i)
		}
	}
	if g.allow("one more", enquiriesPerSender) || len(g.seen) != maxTracked {
		t.Errorf("the guard tracks %d senders, past its limit of %d", len(g.seen), maxTracked)
	}
	if !g.allow("sender 0", enquiriesPerSender) {
		t.Error("a sender already tracked was refused")
	}
}
