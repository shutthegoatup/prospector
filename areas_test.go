package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
)

// A write the spec adds without saying whose work it is would be open to everyone.
func TestEveryAPIWriteNamesItsArea(t *testing.T) {
	spec, err := api.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range spec.Paths.Map() {
		for _, op := range item.Operations() {
			if op.Security == nil {
				continue
			}
			for _, req := range *op.Security {
				if s := req["bearer"]; len(s) > 0 && s[0] != "read" {
					if _, ok := opArea[op.OperationID]; !ok {
						t.Errorf("%s needs %s but opArea doesn't say which area does it", op.OperationID, s[0])
					}
				}
			}
		}
	}
}

func TestEachAreaChangesOnlyItsOwnWork(t *testing.T) {
	s, o, srv, ada := signedIn(t)
	grace := mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	asGrace := signInAs(t, srv, "grace@example.com", "correct horse battery")
	c := repliedPerson(t, s, o, "Hannah Pryce")
	help := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Kind: "support", Name: "Edsger Dijkstra", Email: "edsger@eindhoven.example", Message: "Colours look pale on phones"}))
	base := srv.URL + "/o/alpha/"
	sales := func(cl *http.Client) (int, string) {
		code, body := call(t, cl, "PATCH", base+"api/v1/contacts/"+itoa(c.ID), "application/json", map[string]any{"call_notes": "rang twice"})
		if code == http.StatusNoContent {
			code = http.StatusOK
		}
		return code, body
	}
	deal := func(cl *http.Client) (int, string) {
		return call(t, cl, "POST", base+"api/v1/deals", "application/json", map[string]any{"contact": c.ID, "kind": "renewal"})
	}
	support := func(cl *http.Client) (int, string) {
		return call(t, cl, "PATCH", base+"api/v1/enquiries/"+itoa(help.ID), "application/json", map[string]any{"owner": "Grace Hopper"})
	}
	reads := func(cl *http.Client, who string) {
		t.Helper()
		for _, p := range []string{"today", "research", "conversations", "companies", "enquiries", "subscriptions", "onboarding", "api/v1/deals", "api/v1/enquiries"} {
			if code, _ := call(t, cl, "GET", base+p, "", nil); code != http.StatusOK {
				t.Errorf("%s can't read %s: %d", who, p, code)
			}
		}
	}
	areas := func(to ...string) {
		t.Helper()
		if code, body := call(t, ada, "POST", base+"ui/member/areas", "application/json", map[string]any{"user_id": grace.ID, "areas": to}); code != http.StatusOK {
			t.Fatalf("set areas %v: %d %s", to, code, body)
		}
	}

	if m := must(s.Membership(o.ID, grace.ID)); m.Areas != "sales support" {
		t.Fatalf("someone added to a team starts with %q, want every area", m.Areas)
	}
	if code, _ := call(t, asGrace, "POST", base+"ui/member/areas", "application/json", map[string]any{"user_id": grace.ID, "areas": []string{"sales"}}); code != http.StatusForbidden {
		t.Fatalf("a member changed their own areas: %d", code)
	}

	areas("support")
	if _, page := call(t, ada, "GET", base+"settings", "", nil); !strings.Contains(page, "member · support") {
		t.Error("the Team list doesn't say what someone works on")
	}
	task := func(cl *http.Client) (int, string) {
		return call(t, cl, "POST", base+"api/v1/tasks", "application/json", map[string]any{"title": "Yachts", "brief": "Yacht builders."})
	}
	for name, try := range map[string]func(*http.Client) (int, string){"a contact's notes": sales, "a deal": deal, "a task": task} {
		if code, body := try(asGrace); code != http.StatusForbidden || !strings.Contains(body, "Team list") {
			t.Errorf("support changed %s: %d %s", name, code, body)
		}
	}
	if code, body := support(asGrace); code != http.StatusOK {
		t.Errorf("support can't take an enquiry: %d %s", code, body)
	}
	reads(asGrace, "support")

	areas("sales")
	if code, _ := support(asGrace); code != http.StatusForbidden {
		t.Errorf("sales took an enquiry: %d", code)
	}
	if code, body := sales(asGrace); code != http.StatusOK {
		t.Errorf("sales can't change a contact: %d %s", code, body)
	}
	if code, body := deal(asGrace); code != http.StatusCreated {
		t.Errorf("sales can't start a deal: %d %s", code, body)
	}
	if code, body := task(asGrace); code != http.StatusCreated {
		t.Errorf("sales can't start a task: %d %s", code, body)
	}

	areas()
	if _, page := call(t, ada, "GET", base+"settings", "", nil); !strings.Contains(page, "reads only") {
		t.Error("someone with no area isn't shown as reading only")
	}
	for name, try := range map[string]func(*http.Client) (int, string){"sales": sales, "support": support} {
		if code, _ := try(asGrace); code != http.StatusForbidden {
			t.Errorf("someone with no area did %s work: %d", name, code)
		}
	}
	reads(asGrace, "someone with no area")
	converse := func(cl *http.Client) (int, string) {
		return call(t, cl, "POST", base+"api/v1/enquiries/"+itoa(help.ID)+"/conversation", "application/json", map[string]any{})
	}
	if code, _ := converse(asGrace); code != http.StatusForbidden {
		t.Errorf("someone with no area put an enquiry's sender on the board: %d", code)
	}
	areas("support")
	if code, body := converse(asGrace); code >= 300 {
		t.Errorf("support can't hand an enquiry's sender to sales: %d %s", code, body)
	}

	if code, body := support(ada); code != http.StatusOK {
		t.Errorf("an owner is held to an area: %d %s", code, body)
	}
	key, _, _ := s.CreateKey(o.ID, "bot", []string{"read", "pipeline:write"}, "test")
	bearer := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		r.Header.Set("Authorization", "Bearer "+key)
		return http.DefaultTransport.RoundTrip(r)
	})}
	if code, body := support(bearer); code != http.StatusOK {
		t.Errorf("a key is held to an area rather than its scopes: %d %s", code, body)
	}

	if code, _ := call(t, ada, "POST", base+"ui/member/areas", "application/json", map[string]any{"user_id": grace.ID, "areas": []string{"marketing"}}); code != http.StatusBadRequest {
		t.Errorf("an unknown area was accepted: %d", code)
	}
	outsider := must(s.CreateUser("carol@example.com", "Carol Outsider", "correct horse battery"))
	if code, _ := call(t, ada, "POST", base+"ui/member/areas", "application/json", map[string]any{"user_id": outsider.ID, "areas": []string{"sales"}}); code != http.StatusBadRequest {
		t.Errorf("someone outside the organisation was given an area: %d", code)
	}
	if err := s.SetAreas(o.ID, grace.ID, []string{"support"}); err != nil {
		t.Fatal(err)
	}
	if got := must(s.ExportOrg(o.ID, "Ada Lovelace")); !strings.Contains(strings.Join(memberAreas(got), ","), "support") {
		t.Errorf("the organisation's export doesn't say what people work on: %v", memberAreas(got))
	}
}

func memberAreas(e *OrgExport) []string {
	var out []string
	for _, m := range e.Members {
		out = append(out, m.Email+"="+m.Areas)
	}
	return out
}

func TestMembershipsGainAreasWithRowsInThem(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	grace := mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	if err := s.db.Migrator().DropColumn(&Membership{}, "areas"); err != nil {
		t.Fatal(err)
	}
	if err := s.db.AutoMigrate(models...); err != nil {
		t.Fatalf("adding areas to a table with rows: %v", err)
	}
	if m := must(s.Membership(o.ID, grace.ID)); m.Areas != "sales support" {
		t.Fatalf("someone on a team before areas works on %q, want every area", m.Areas)
	}
}
