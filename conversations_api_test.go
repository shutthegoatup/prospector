package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/onboarding"
)

// The React Conversations page reads the board, cards and all, from the API.
func TestTheBoardIsReadThroughTheAPI(t *testing.T) {
	t.Parallel()
	s, o, srv, owner := signedIn(t)
	o.Settings.OnboardingSteps = "them: Send your product files, 7 days\nus: Go live"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	day := today()
	hannah := repliedPerson(t, s, o, "Hannah Pryce")
	for field, value := range map[string]string{"call_notes": "Wants a configurator by spring", "deal_amount": "14,000", "deal_owner": "Ada Lovelace", "stage": "won"} {
		if err := s.Update(o.ID, hannah.ID, field, value, "Ada Lovelace"); err != nil {
			t.Fatal(err)
		}
	}
	renewal := must(s.StartDeal(o.ID, hannah.ID, "renewal", "Ada Lovelace"))
	must(s.StartOnboarding(o.ID, onboarding.OnboardingIn{Company: *hannah.CompanyID, Start: day}, "Ada Lovelace"))

	ines := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Ines Moreau", Company: "Belair Kitchens"}, "Ada Lovelace"))
	if err := s.Mark(o.ID, ines.ID, "replied", day, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(o.ID, ines.ID, "stage", "won", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	must(s.StartOnboarding(o.ID, onboarding.OnboardingIn{Company: *ines.CompanyID, Start: day}, "Ada Lovelace"))
	for _, step := range must(s.Onboardings(o.ID, *ines.CompanyID, day))[0].Steps {
		if err := s.SetOnboardingStep(o.ID, step.ID, "done", "yes", "Ada Lovelace"); err != nil {
			t.Fatal(err)
		}
	}
	tobias := repliedPerson(t, s, o, "Tobias Wren")

	base := srv.URL + "/o/alpha/api/v1/"
	var board struct {
		Items           []api.DealCard `json:"items"`
		OnboardingSteps int            `json:"onboarding_steps"`
	}
	code, body := call(t, owner, "GET", base+"conversations", "", nil)
	if err := json.Unmarshal([]byte(body), &board); err != nil || code != http.StatusOK {
		t.Fatalf("the board: %d %s", code, body)
	}
	if board.OnboardingSteps != 2 || len(board.Items) != 4 {
		t.Fatalf("the board should have four cards and two planned steps: %s", body)
	}
	cards := map[string]api.DealCard{}
	for _, c := range board.Items {
		cards[c.Person+"/"+c.Kind] = c
	}
	if c := cards["Tobias Wren/new"]; c.Stage != "replied" || c.Id != 0 || c.Company != "Larkmoor Yachts" || c.Onboarding != "running" {
		t.Errorf("a conversation nothing is set on: %+v", c)
	}
	if c := cards["Hannah Pryce/new"]; c.Seq != 0 || c.Stage != "won" || c.Value != "£14,000" || c.Owner != "Ada Lovelace" || c.CallNotes != "Wants a configurator by spring" || c.Onboarding != "running" {
		t.Errorf("a won conversation with a value: %+v", c)
	}
	if c := cards["Hannah Pryce/renewal"]; c.Id != renewal.ID || c.Seq != 1 || c.Company != "Larkmoor Yachts" || c.ContactId != hannah.ID {
		t.Errorf("a renewal with someone in conversation: %+v", c)
	}
	if c := cards["Ines Moreau/new"]; c.Company != "Belair Kitchens" || c.Onboarding != "live" {
		t.Errorf("a won deal whose company is live: %+v", c)
	}
	if board.Items[0].Person != "Tobias Wren" || board.Items[len(board.Items)-1].Stage != "won" {
		t.Errorf("the board isn't in its stages' order: %s", body)
	}

	// A page at a time, the board is the whole board, each card once.
	seen, cursor := map[string]bool{}, ""
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("the board's pages never ended")
		}
		var page struct {
			Items      []api.DealCard `json:"items"`
			NextCursor string         `json:"next_cursor"`
		}
		code, body := call(t, owner, "GET", base+"conversations?limit=1&cursor="+cursor, "", nil)
		if err := json.Unmarshal([]byte(body), &page); err != nil || code != http.StatusOK || len(page.Items) != 1 {
			t.Fatalf("a page of the board: %d %s", code, body)
		}
		seen[page.Items[0].Person+"/"+page.Items[0].Kind] = true
		if cursor = page.NextCursor; cursor == "" {
			break
		}
	}
	if len(seen) != 4 {
		t.Errorf("walking the board met %d cards, want 4", len(seen))
	}

	// The names a deal can be given to, from the team list.
	mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	code, body = call(t, owner, "GET", base+"members", "", nil)
	if code != http.StatusOK || !strings.Contains(body, `"name":"Ada Lovelace"`) || !strings.Contains(body, `"name":"Grace Hopper"`) {
		t.Errorf("the members: %d %s", code, body)
	}

	reader, _, err := s.CreateKey(o.ID, "reader", []string{"read"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	stranger, _, err := s.CreateKey(mustOrg(t, s, "beta").ID, "theirs", []string{"read", "pipeline:write"}, "Grace Hopper")
	if err != nil {
		t.Fatal(err)
	}
	for _, list := range []string{"conversations", "members"} {
		if code, body := asKey(t, "GET", base+list, reader, nil); code != http.StatusOK || !strings.Contains(body, `"items":[{`) {
			t.Errorf("a read key reading %s: %d %s", list, code, body)
		}
		if code, _ := asKey(t, "GET", base+list, stranger, nil); code != http.StatusForbidden {
			t.Errorf("another organisation's key reading %s: %d", list, code)
		}
	}
	if code, _ := asKey(t, "PATCH", base+"contacts/"+itoa(tobias.ID)+"/deal", reader, map[string]any{"amount": "500"}); code != http.StatusForbidden {
		t.Errorf("a read key valuing a deal: %d", code)
	}
}

// What the board's cards offer, the API refuses to anyone whose work it isn't.
func TestTheBoardsWritesAreRefusedOutsideTheirArea(t *testing.T) {
	t.Parallel()
	s, o, srv, _ := signedIn(t)
	o.Settings.OnboardingSteps = "us: Go live"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	c := repliedPerson(t, s, o, "Hannah Pryce")
	renewal := must(s.StartDeal(o.ID, c.ID, "renewal", "Ada Lovelace"))
	base := srv.URL + "/o/alpha/api/v1/"
	writes := []struct {
		method, path string
		body         any
	}{
		{"PATCH", "contacts/" + itoa(c.ID), map[string]any{"stage": "meeting"}},
		{"PATCH", "contacts/" + itoa(c.ID) + "/deal", map[string]any{"amount": "500"}},
		{"POST", "deals", map[string]any{"contact": c.ID, "kind": "expansion"}},
		{"PATCH", "deals/" + itoa(renewal.ID), map[string]any{"next_step": "send terms"}},
		{"DELETE", "deals/" + itoa(renewal.ID), nil},
	}
	support := mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	if err := s.SetAreas(o.ID, support.ID, []string{identity.AreaSupport}); err != nil {
		t.Fatal(err)
	}
	asSupport := signInAs(t, srv, "grace@example.com", "correct horse battery")
	if code, _ := call(t, asSupport, "GET", base+"conversations", "", nil); code != http.StatusOK {
		t.Errorf("support reading the board: %d", code)
	}
	for _, w := range writes {
		if code, body := call(t, asSupport, w.method, base+w.path, "application/json", w.body); code != http.StatusForbidden || !strings.Contains(body, "sales") {
			t.Errorf("support doing %s %s: %d %s", w.method, w.path, code, body)
		}
	}
	if d := must(s.Deals(o.ID)); len(d) != 0 {
		t.Errorf("a refused write left a deal: %+v", d)
	}

	nobody := mustUser(t, s, o, "carol@example.com", "Carol Shaw", "member")
	if err := s.SetAreas(o.ID, nobody.ID, nil); err != nil {
		t.Fatal(err)
	}
	asNobody := signInAs(t, srv, "carol@example.com", "correct horse battery")
	if code, body := call(t, asNobody, "POST", base+"onboardings", "application/json", map[string]any{"company": *c.CompanyID}); code != http.StatusForbidden {
		t.Errorf("someone with no area starting an onboarding: %d %s", code, body)
	}
	if code, body := call(t, asSupport, "POST", base+"onboardings", "application/json", map[string]any{"company": *c.CompanyID}); code != http.StatusCreated {
		t.Errorf("support starting an onboarding from a won deal: %d %s", code, body)
	}
}
