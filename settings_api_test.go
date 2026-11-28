package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/store"
)

// What the organisation looks for comes back as a list, which a change must still be
// compared against rather than failing the request.
func TestSettingsChangeWithALookForList(t *testing.T) {
	t.Parallel()
	s, o, srv, _ := signedIn(t)
	o.Settings.LookFor = "careers_page: a page of open roles\nats (must): an applicant tracking system"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	secret, _, err := s.CreateKey(o.ID, "robot", []string{"settings:write"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	url := srv.URL + "/o/alpha/api/v1/settings"
	if code, body := asKey(t, "PATCH", url, secret, map[string]any{"pitch": "We help {company}."}); code != http.StatusOK {
		t.Fatalf("changing the pitch with a look-for list set: %d %s", code, body)
	}
	code, body := asKey(t, "PATCH", url, secret, map[string]any{"look_for": "careers_page: a page of open roles"})
	if code != http.StatusOK || !strings.Contains(body, `"look_for":"careers_page: a page of open roles"`) {
		t.Fatalf("changing the look-for list itself: %d %s", code, body)
	}
	if activity, _ := s.AgentActivity(o.ID, 1); len(activity) == 0 || !strings.Contains(activity[0].Value, "look_for") {
		t.Fatalf("the change should be in the history, naming what changed: %+v", activity)
	}
}

func TestSettingsChangeThroughTheAPIOnlyWhatIsSent(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	o.Settings.Pitch, o.Settings.NeverContact = "We help {company}.", "rival.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	url := srv.URL + "/o/alpha/api/v1/settings"
	key := func(scopes ...string) string {
		secret, _, err := s.CreateKey(o.ID, "robot", scopes, "Ada Lovelace")
		if err != nil {
			t.Fatal(err)
		}
		return secret
	}
	reader, writer := key("read"), key("settings:write")

	if code, body := asKey(t, "PATCH", url, reader, map[string]any{"enquiry_sites": "https://www.alpha.example"}); code != http.StatusForbidden || !strings.Contains(body, "settings:write") {
		t.Fatalf("a read key changed settings: %d %s", code, body)
	}
	code, body := asKey(t, "PATCH", url, writer, map[string]any{"enquiry_sites": "https://www.alpha.example"})
	var got api.Settings
	if json.Unmarshal([]byte(body), &got); code != http.StatusOK || got.EnquirySites != "https://www.alpha.example" || got.Pitch != "We help {company}." || got.Name == "" {
		t.Fatalf("changing only the enquiry sites: %d %s", code, body)
	}
	saved := must(s.OrgBySlug("alpha"))
	if !saved.Settings.TakesEnquiriesFrom("https://www.alpha.example") || saved.Settings.NeverContact != "rival.example" {
		t.Fatalf("what was left out should stay as it was: %+v", saved.Settings)
	}
	if activity, _ := s.AgentActivity(o.ID, 1); len(activity) != 1 || activity[0].Actor != "robot" || activity[0].Action != "settings:change" || activity[0].Value != "enquiry_sites" {
		t.Fatalf("the key's change should be in the history, naming what it changed: %+v", activity)
	}

	beta := mustOrg(t, s, "beta")
	theirs, _, _ := s.CreateKey(beta.ID, "theirs", []string{"settings:write"}, "Grace Hopper")
	mustUser(t, s, beta, "owner@beta.example", "Barbara Liskov", "owner")
	betaOwner := signInAs(t, srv, "owner@beta.example", "correct horse battery")
	if code, _ := asKey(t, "PATCH", url, theirs, map[string]any{"pitch": "theirs"}); code == http.StatusOK {
		t.Fatal("another organisation's key changed alpha's settings")
	}
	if code, _ := call(t, betaOwner, "PATCH", url, "application/json", map[string]any{"pitch": "theirs"}); code == http.StatusOK {
		t.Fatal("another organisation's owner changed alpha's settings")
	}
	if must(s.OrgBySlug("alpha")).Settings.Pitch != "We help {company}." {
		t.Fatal("alpha's pitch was changed from another organisation")
	}

	if code, body := asKey(t, "PATCH", url, writer, map[string]any{"enquiry_sites": "alpha.example", "pitch": "changed"}); code != http.StatusBadRequest || !strings.Contains(body, "isn't a site address") {
		t.Fatalf("a site without a scheme was saved: %d %s", code, body)
	}
	if must(s.OrgBySlug("alpha")).Settings.Pitch != "We help {company}." {
		t.Fatal("a refused change saved part of itself")
	}
	if code, body := asKey(t, "PATCH", url, writer, map[string]any{"name": "  "}); code != http.StatusBadRequest || !strings.Contains(body, "needs a name") {
		t.Fatalf("the name was emptied: %d %s", code, body)
	}

	if code, body := asKey(t, "GET", url, reader, nil); code != http.StatusOK || !strings.Contains(body, `"email_after_days":2`) {
		t.Fatalf("reading settings shows them with defaults: %d %s", code, body)
	}

	mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	grace := signInAs(t, srv, "grace@example.com", "correct horse battery")
	if code, body := call(t, grace, "PATCH", url, "application/json", map[string]any{"pitch": "Grace's"}); code != http.StatusForbidden || !strings.Contains(body, "owner") {
		t.Fatalf("a member who isn't an owner changed settings: %d %s", code, body)
	}
	if code, body := call(t, ada, "PATCH", url, "application/json", map[string]any{"currency": "eur"}); code != http.StatusOK || !strings.Contains(body, `"currency":"EUR"`) {
		t.Fatalf("an owner changing the currency: %d %s", code, body)
	}
}

func TestOnlyWhatTakesEffectDifferentlyIsAChange(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	url := srv.URL + "/o/alpha/api/v1/settings"
	changes := func() []string {
		var out []string
		var events []store.Event
		s.DB.Where("org_id = ? and action = ?", o.ID, "settings:change").Order("id").Find(&events)
		for _, e := range events {
			out = append(out, store.Deref(e.Value))
		}
		return out
	}
	// The page sends back every field as it showed them, defaults included.
	_, body := call(t, ada, "GET", url, "", nil)
	var shown map[string]any
	if err := json.Unmarshal([]byte(body), &shown); err != nil {
		t.Fatal(err)
	}
	shown["pitch"] = "We make {company} faster."
	if code, body := call(t, ada, "PATCH", url, "application/json", shown); code != http.StatusOK {
		t.Fatalf("saving the page as shown: %d %s", code, body)
	}
	if got := changes(); len(got) != 1 || got[0] != "pitch" {
		t.Fatalf("only the pitch changed, but the history says %q", got)
	}
	currency := strings.ToLower(shown["currency"].(string))
	if code, body := call(t, ada, "PATCH", url, "application/json", map[string]any{"currency": currency, "name": " " + o.Name + " "}); code != http.StatusOK {
		t.Fatalf("sending what is already there: %d %s", code, body)
	}
	if got := changes(); len(got) != 1 {
		t.Fatalf("a currency in lower case and a padded name changed nothing, but the history says %q", got)
	}
}
