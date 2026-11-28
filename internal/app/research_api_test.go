package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/store"
)

func TestARobotAddsPeopleAndReviewsResearchAsItsScopesAllow(t *testing.T) {
	t.Parallel()
	s, o, srv, _ := signedIn(t)
	key := func(name string, scopes ...string) (string, *store.APIKey) {
		secret, k, err := s.CreateKey(o.ID, name, scopes, "Ada Lovelace")
		if err != nil {
			t.Fatal(err)
		}
		return secret, k
	}
	reader, _ := key("reader", "read")
	contacts, _ := key("assistant", "contacts:write")
	reviewer, _ := key("reviewer", "research:review")
	scoutKey, scoutRow := key("scout", "research:write")
	scout := store.Actor{Name: "agent:scout", KeyID: scoutRow.ID}
	base := srv.URL + "/o/alpha/api/v1/"

	// Adding someone yourself.
	person := map[string]any{"name": "Hannah Pryce", "title": "Head of Marketing", "company": "Larkmoor Yachts", "linkedin": "https://www.linkedin.com/in/hannah-pryce"}
	if code, body := asKey(t, "POST", base+"people", reader, person); code != http.StatusForbidden || !strings.Contains(body, "contacts:write") {
		t.Fatalf("a read key adding a person: %d %s", code, body)
	}
	code, body := asKey(t, "POST", base+"people", contacts, person)
	var added api.PersonAdded
	if json.Unmarshal([]byte(body), &added); code != http.StatusCreated || added.Id == 0 || !strings.Contains(added.Message, "LinkedIn queue") {
		t.Fatalf("a contacts key adding a person: %d %s", code, body)
	}

	// Changing their details, together or not at all.
	contact := fmt.Sprintf("%scontacts/%d", base, added.Id)
	if code, body := asKey(t, "PATCH", contact, contacts, map[string]any{"phone": "+44 20 7946 0000", "call_notes": "Rang, left a message."}); code != http.StatusNoContent {
		t.Fatalf("changing two details: %d %s", code, body)
	}
	c := row[store.Contact](t, s, o.ID, added.Id)
	if store.Deref(c.Phone) != "+44 20 7946 0000" || store.Deref(c.CallNotes) != "Rang, left a message." || store.Deref(c.Tz) == "" {
		t.Fatalf("both details, and a time zone guessed from the phone: %+v", c)
	}
	if code, _ := asKey(t, "PATCH", contact, contacts, map[string]any{"call_notes": "Changed my mind.", "stage": "no such stage"}); code == http.StatusNoContent {
		t.Fatal("a change with a bad field was taken")
	}
	if c := row[store.Contact](t, s, o.ID, added.Id); store.Deref(c.CallNotes) != "Rang, left a message." {
		t.Fatalf("a refused change still changed the other field: %q", store.Deref(c.CallNotes))
	}
	// A time zone sent alongside a new phone is kept, not replaced by the phone's guess.
	if code, body := asKey(t, "PATCH", contact, contacts, map[string]any{"phone": "+1 212 555 0100", "tz": "Europe/Lisbon"}); code != http.StatusNoContent {
		t.Fatalf("a phone and a time zone together: %d %s", code, body)
	}
	if c := row[store.Contact](t, s, o.ID, added.Id); store.Deref(c.Tz) != "Europe/Lisbon" {
		t.Fatalf("the time zone sent should win over the phone's guess: %q", store.Deref(c.Tz))
	}
	if code, _ := asKey(t, "PATCH", contact, contacts, map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("a change of nothing: %d", code)
	}
	if code, _ := asKey(t, "PATCH", base+"contacts/999", contacts, map[string]any{"call_notes": "x"}); code != http.StatusNotFound {
		t.Fatalf("a person who isn't there: %d", code)
	}

	// An agent's research waits for review, which its own key can't give.
	co := must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Corvane Marine",
		Facts: []research.FactIn{{Field: "configurator", Value: "Tender builder", SourceURL: "https://corvane.example/build"}}}))
	suggested := must(s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: "Corvane Marine", Name: "Tom Ellery", SourceURL: "https://corvane.example/team"}))
	company := fmt.Sprintf("%scompanies/%d", base, co.ID)
	if code, body := asKey(t, "PATCH", company, scoutKey, map[string]any{"stage": "qualified"}); code != http.StatusForbidden || !strings.Contains(body, "research:review") {
		t.Fatalf("an agent qualifying its own find: %d %s", code, body)
	}
	if code, body := asKey(t, "PATCH", company, reviewer, map[string]any{"stage": "qualified", "fit": "Builds to order"}); code != http.StatusNoContent {
		t.Fatalf("a reviewer qualifying a company: %d %s", code, body)
	}
	if got := row[store.Company](t, s, o.ID, co.ID); got.Stage != prospecting.CompanyQualified || got.Fit != "Builds to order" {
		t.Fatalf("the company: %+v", got)
	}
	if code, _ := asKey(t, "PATCH", company, reviewer, map[string]any{"stage": "maybe"}); code != http.StatusBadRequest {
		t.Fatalf("a stage that isn't one: %d", code)
	}
	if code, body := asKey(t, "POST", fmt.Sprintf("%scontacts/%d/pick", base, suggested.ID), reviewer, nil); code != http.StatusNoContent {
		t.Fatalf("a reviewer picking a suggested person: %d %s", code, body)
	}
	if got := row[store.Contact](t, s, o.ID, suggested.ID); got.Status != "" {
		t.Fatalf("a picked person should leave Research: %+v", got)
	}

	// A detail found for someone already stored waits to be accepted.
	must(s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: "Corvane Marine", Name: "Tom Ellery", Email: "tom@corvane.example", SourceURL: "https://corvane.example/team"}))
	var pending []store.Suggestion
	if err := s.DB.Where("org_id = ? and contact_id = ? and decision = ''", o.ID, suggested.ID).Find(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("the new address should wait: %+v", pending)
	}
	decide := fmt.Sprintf("%ssuggestions/%d", base, pending[0].ID)
	if code, _ := asKey(t, "PATCH", decide, reviewer, map[string]any{"decision": "perhaps"}); code != http.StatusBadRequest {
		t.Fatalf("a decision that isn't one: %d", code)
	}
	if code, body := asKey(t, "PATCH", decide, reviewer, map[string]any{"decision": "accepted"}); code != http.StatusNoContent {
		t.Fatalf("accepting it: %d %s", code, body)
	}
	if got := row[store.Contact](t, s, o.ID, suggested.ID); store.Deref(got.Email) != "tom@corvane.example" {
		t.Fatalf("an accepted address should be filled in: %q", store.Deref(got.Email))
	}
	if code, _ := asKey(t, "PATCH", decide, reviewer, map[string]any{"decision": "dismissed"}); code != http.StatusConflict {
		t.Fatalf("deciding it twice: %d", code)
	}
}

// row is one of the organisation's rows, read straight from the database.
func row[T any](t *testing.T, s *Store, org, id int64) T {
	t.Helper()
	var out T
	if err := s.DB.Where("org_id = ? and id = ?", org, id).First(&out).Error; err != nil {
		t.Fatal(err)
	}
	return out
}
