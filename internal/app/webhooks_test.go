package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/store"
)

const hookURL = "https://hooks.larkmoor.example/prospector"

func TestAnOwnerSendsEventsToAnEndpoint(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	base := srv.URL + "/o/alpha/api/v1/"
	_, key, err := s.CreateKey(o.ID, "crm-sync", []string{"read"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	code, body := call(t, ada, "POST", base+"webhooks", "application/json", map[string]any{"url": hookURL, "key": key.ID, "events": []string{"company.added", "contact.marked", "company.added"}})
	if code != http.StatusCreated {
		t.Fatalf("making one: %d %s", code, body)
	}
	var made api.WebhookMade
	_ = json.Unmarshal([]byte(body), &made)
	if !strings.HasPrefix(made.Secret, "whsec_") || made.Webhook.KeyName != "crm-sync" || strings.Join(made.Webhook.Events, " ") != "company.added contact.marked" {
		t.Fatalf("made: %+v", made)
	}
	one := base + "webhooks/" + itoa(made.Webhook.Id)
	if _, body := call(t, ada, "GET", base+"webhooks", "", nil); strings.Contains(body, made.Secret) || !strings.Contains(body, hookURL) {
		t.Fatalf("the list: %s", body)
	}
	if code, body := call(t, ada, "PATCH", one, "application/json", map[string]any{"enabled": false}); code != http.StatusOK || !strings.Contains(body, `"disabled_why":"stopped by Ada Lovelace"`) {
		t.Fatalf("stopping it: %d %s", code, body)
	}
	if code, body := call(t, ada, "PATCH", one, "application/json", map[string]any{"enabled": true, "events": []string{"deal.changed"}}); code != http.StatusOK || !strings.Contains(body, `"disabled_at":null`) || !strings.Contains(body, `"events":["deal.changed"]`) {
		t.Fatalf("starting it again: %d %s", code, body)
	}
	code, body = call(t, ada, "POST", one+"/secret", "application/json", nil)
	var rotated api.WebhookSecret
	_ = json.Unmarshal([]byte(body), &rotated)
	if code != http.StatusOK || !strings.HasPrefix(rotated.Secret, "whsec_") || rotated.Secret == made.Secret {
		t.Fatalf("a new secret: %d %s", code, body)
	}
	e := must(s.Webhook(o.ID, made.Webhook.Id))
	if e.PreviousSecret == "" || e.PreviousUntil == nil {
		t.Fatalf("the old secret doesn't sign beside the new: %+v", e)
	}
	if was, err := s.Keyring.Open(e.PreviousSecret); err != nil || was != made.Secret {
		t.Fatalf("the old secret kept: %q %v", was, err)
	}
	if code, _ := call(t, ada, "DELETE", one, "", nil); code != http.StatusNoContent {
		t.Fatalf("removing it: %d", code)
	}
	if code, _ := call(t, ada, "GET", one, "", nil); code != http.StatusNotFound {
		t.Fatalf("removed, but still there: %d", code)
	}
	var logged []string
	s.DB.Model(&store.Event{}).Where("org_id = ? and action like 'endpoint:%'", o.ID).Order("id").Pluck("action", &logged)
	if strings.Join(logged, " ") != "endpoint:new endpoint:change endpoint:change endpoint:secret endpoint:remove" {
		t.Fatalf("the history: %v", logged)
	}
}

func TestOnlyAnOwnerSetsUpAnEndpointAndOnlyASafeOne(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	base := srv.URL + "/o/alpha/api/v1/"
	_, reader, _ := s.CreateKey(o.ID, "reader", []string{"read"}, "test")
	_, writer, _ := s.CreateKey(o.ID, "writer", []string{"research:write"}, "test")
	secret, owner, _ := s.CreateKey(o.ID, "owner-ish", []string{"read", "agents:configure"}, "test")
	_, gone, _ := s.CreateKey(o.ID, "gone", []string{"read"}, "test")
	if err := s.RevokeKey(o.ID, gone.ID, "test"); err != nil {
		t.Fatal(err)
	}
	try := func(c *http.Client, url string, key int64, events ...string) int {
		code, _ := call(t, c, "POST", base+"webhooks", "application/json", map[string]any{"url": url, "key": key, "events": events})
		return code
	}
	if code, body := asKey(t, "POST", base+"webhooks", secret, map[string]any{"url": hookURL, "key": owner.ID, "events": []string{"company.added"}}); code != http.StatusForbidden {
		t.Errorf("a key, even one that configures agents, made an endpoint: %d %s", code, body)
	}
	if code, _ := asKey(t, "GET", base+"webhooks", secret, nil); code != http.StatusForbidden {
		t.Errorf("a key listed the endpoints: %d", code)
	}
	mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	if code := try(signInAs(t, srv, "grace@example.com", "correct horse battery"), hookURL, reader.ID, "company.added"); code != http.StatusForbidden {
		t.Errorf("a member who isn't an owner made one: %d", code)
	}
	for _, bad := range []struct {
		why, url string
		key      int64
		events   []string
		want     int
	}{
		{"plain http", "http://hooks.larkmoor.example/", reader.ID, []string{"company.added"}, http.StatusBadRequest},
		{"this host", "https://127.0.0.1/hooks", reader.ID, []string{"company.added"}, http.StatusBadRequest},
		{"a metadata address", "https://169.254.169.254/latest", reader.ID, []string{"company.added"}, http.StatusBadRequest},
		{"localhost", "https://localhost/hooks", reader.ID, []string{"company.added"}, http.StatusBadRequest},
		{"no such event", hookURL, reader.ID, []string{"company.exploded"}, http.StatusBadRequest},
		{"no events", hookURL, reader.ID, []string{}, http.StatusBadRequest},
		{"a key that can't read", hookURL, writer.ID, []string{"company.added"}, http.StatusBadRequest},
		{"a revoked key", hookURL, gone.ID, []string{"company.added"}, http.StatusNotFound},
		{"no such key", hookURL, 999999, []string{"company.added"}, http.StatusNotFound},
	} {
		if code := try(ada, bad.url, bad.key, bad.events...); code != bad.want {
			t.Errorf("%s: %d, want %d", bad.why, code, bad.want)
		}
	}
	if n := len(must(s.Webhooks(o.ID))); n != 0 {
		t.Fatalf("%d endpoints made from refusals", n)
	}
}

// TestAWebhookStaysInItsOrganisation: another organisation's owner can neither read,
// change, rotate nor remove an endpoint, nor bind one of theirs to this one's key.
func TestAWebhookStaysInItsOrganisation(t *testing.T) {
	t.Parallel()
	s, o, srv, _ := signedIn(t)
	_, key, _ := s.CreateKey(o.ID, "crm-sync", []string{"read"}, "test")
	e, _ := must2(s.AddWebhook(o.ID, key.ID, hookURL, []string{"company.added"}, "Ada Lovelace"))
	beta := mustOrg(t, s, "beta")
	mustUser(t, s, beta, "bob@beta.example", "Bob Beta", "owner")
	bob := signInAs(t, srv, "bob@beta.example", "correct horse battery")
	theirs := srv.URL + "/o/beta/api/v1/webhooks/" + itoa(e.ID)
	for _, c := range []struct{ method, path string }{{"GET", ""}, {"PATCH", ""}, {"DELETE", ""}, {"POST", "/secret"}} {
		var body any
		if c.method == "PATCH" {
			body = map[string]any{"enabled": false}
		}
		if code, _ := call(t, bob, c.method, theirs+c.path, "application/json", body); code != http.StatusNotFound {
			t.Errorf("%s %s of another organisation's endpoint: %d", c.method, c.path, code)
		}
	}
	if code, _ := call(t, bob, "POST", srv.URL+"/o/beta/api/v1/webhooks", "application/json", map[string]any{"url": hookURL, "key": key.ID, "events": []string{"company.added"}}); code != http.StatusNotFound {
		t.Errorf("bound to another organisation's key: %d", code)
	}
	if now := must(s.Webhook(o.ID, e.ID)); now.DisabledAt != nil || now.Secret != e.Secret {
		t.Fatalf("changed from another organisation: %+v", now)
	}
}
