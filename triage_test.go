package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestATriageKeyWorksEnquiriesButNotDeals(t *testing.T) {
	s, o, srv, _ := signedIn(t)
	c := repliedPerson(t, s, o, "Hannah Pryce")
	help := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Kind: "support", Name: "Edsger Dijkstra", Email: "edsger@eindhoven.example", Message: "Colours look pale on phones"}))
	base := srv.URL + "/o/alpha/api/v1/"
	keyed := func(scopes ...string) *http.Client {
		secret, _, err := s.CreateKey(o.ID, strings.Join(scopes, "+"), scopes, "test")
		if err != nil {
			t.Fatal(err)
		}
		return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			r.Header.Set("Authorization", "Bearer "+secret)
			return http.DefaultTransport.RoundTrip(r)
		})}
	}
	take := func(cl *http.Client) (int, string) {
		return call(t, cl, "PATCH", base+"enquiries/"+itoa(help.ID), "application/json", map[string]any{"state": "waiting"})
	}
	triage, pipeline, reader := keyed("read", "enquiries:triage"), keyed("read", "pipeline:write"), keyed("read")

	if code, body := take(triage); code != http.StatusOK {
		t.Fatalf("a triage key can't move an enquiry along: %d %s", code, body)
	}
	if code, body := call(t, triage, "POST", base+"enquiries/"+itoa(help.ID)+"/conversation", "application/json", map[string]any{}); code != http.StatusCreated {
		t.Fatalf("a triage key can't hand an enquiry's sender to sales: %d %s", code, body)
	}
	if code, body := call(t, triage, "POST", base+"deals", "application/json", map[string]any{"contact": c.ID, "kind": "renewal"}); code != http.StatusForbidden || !strings.Contains(body, "pipeline:write") {
		t.Fatalf("a triage key started a deal: %d %s", code, body)
	}
	if code, body := take(pipeline); code != http.StatusOK {
		t.Fatalf("a pipeline key lost enquiries: %d %s", code, body)
	}
	if code, body := take(reader); code != http.StatusForbidden || !strings.Contains(body, "pipeline:write or enquiries:triage") {
		t.Fatalf("a read key moved an enquiry, or wasn't told which scopes would: %d %s", code, body)
	}
	if _, page := call(t, http.DefaultClient, "GET", srv.URL+"/api", "", nil); !strings.Contains(page, "pipeline:write or enquiries:triage") {
		t.Error("the API page doesn't say either scope will do")
	}
}
