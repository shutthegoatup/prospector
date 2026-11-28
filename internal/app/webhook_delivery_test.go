package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/store"
	"github.com/shutthegoatup/prospector/internal/webhooks"
)

// hookReceiver is an endpoint that answers with what its status says, and keeps what it got.
type hookReceiver struct {
	mu     sync.Mutex
	status int
	got    []*http.Request
	bodies [][]byte
	srv    *httptest.Server
}

func newHookReceiver(t *testing.T) *hookReceiver {
	r := &hookReceiver{status: http.StatusNoContent}
	r.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		defer r.mu.Unlock()
		r.got, r.bodies = append(r.got, req), append(r.bodies, b)
		w.WriteHeader(r.status)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

// hooked is an organisation with a key and an endpoint at rcv for the events named; the
// endpoint is pointed at the test's server, which a real one couldn't be.
func hooked(t *testing.T, s *Store, o *store.Org, rcv *hookReceiver, events ...string) (*store.WebhookEndpoint, string, *webhooks.Deliverer) {
	t.Helper()
	_, key, err := s.CreateKey(o.ID, "crm-sync", []string{"read"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	e, secret := must2(s.AddWebhook(o.ID, key.ID, hookURL, events, "Ada Lovelace"))
	if err := s.DB.Model(e).Update("url", rcv.srv.URL).Error; err != nil {
		t.Fatal(err)
	}
	return e, secret, &webhooks.Deliverer{Store: &s.WebhookStore, Client: rcv.srv.Client(), Base: "https://prospector.example", Jitter: func() float64 { return 0.5 }}
}

func deliveriesOf(t *testing.T, s *Store, org int64) []store.WebhookDelivery {
	t.Helper()
	var out []store.WebhookDelivery
	if err := s.DB.Where("org_id = ?", org).Order("id").Find(&out).Error; err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAnEventGoesToItsEndpointSigned(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	rcv := newHookReceiver(t)
	_, secret, d := hooked(t, s, o, rcv, "contact.added")
	c := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts"}, "Ada Lovelace"))
	queued := deliveriesOf(t, s, o.ID)
	if len(queued) != 1 || queued[0].Type != "contact.added" || queued[0].Status != store.DeliveryPending || !strings.HasPrefix(queued[0].MessageID, "msg_") {
		t.Fatalf("queued: %+v", queued)
	}
	if n := d.Round(context.Background()); n != 1 || len(rcv.got) != 1 {
		t.Fatalf("settled %d, received %d", n, len(rcv.got))
	}
	req, body := rcv.got[0], rcv.bodies[0]
	id, ts := req.Header.Get("webhook-id"), req.Header.Get("webhook-timestamp")
	key, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(body)
	if want := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil)); req.Header.Get("webhook-signature") != want || id != queued[0].MessageID {
		t.Fatalf("signed %q with id %q, want %q", req.Header.Get("webhook-signature"), id, want)
	}
	var got struct {
		Type string `json:"type"`
		Data struct {
			Organisation string            `json:"organisation"`
			ContactID    int64             `json:"contact_id"`
			Links        map[string]string `json:"links"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &got); err != nil || got.Type != "contact.added" || got.Data.Organisation != "alpha" || got.Data.ContactID != c.ID ||
		got.Data.Links["contact"] != "https://prospector.example/o/alpha/api/v1/contacts/"+itoa(c.ID) {
		t.Fatalf("sent %s", body)
	}
	if strings.Contains(string(body), "Hannah") || strings.Contains(string(body), "Larkmoor") {
		t.Fatalf("someone's details were sent: %s", body)
	}
	if now := deliveriesOf(t, s, o.ID)[0]; now.Status != store.DeliveryDelivered || now.LastCode != http.StatusNoContent || now.DeliveredAt == nil {
		t.Fatalf("settled as %+v", now)
	}
	if d.Round(context.Background()) != 0 || len(rcv.got) != 1 {
		t.Fatal("sent again once taken")
	}
}

// TestNothingIsQueuedWithoutAnEndpointThatAsked: no endpoint, another event, a key that
// can't read or is revoked, or a stopped endpoint, and the history writes nothing more.
func TestNothingIsQueuedWithoutAnEndpointThatAsked(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o, beta := mustOrg(t, s, "alpha"), mustOrg(t, s, "beta")
	rcv := newHookReceiver(t)
	e, _, _ := hooked(t, s, o, rcv, "deal.changed")
	must(s.QuickAddPerson(beta, crm.QuickAdd{Name: "Tom Vell", Company: "Vell Studio"}, "Bob"))
	must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts"}, "Ada Lovelace"))
	if got := deliveriesOf(t, s, o.ID); len(got) != 0 {
		t.Fatalf("an event it didn't ask for was queued: %+v", got)
	}
	if got := deliveriesOf(t, s, beta.ID); len(got) != 0 {
		t.Fatalf("queued for an organisation with no endpoint: %+v", got)
	}
	must(s.ChangeWebhook(o.ID, e.ID, webhooks.WebhookChange{Events: &[]string{"contact.added"}, Enabled: new(false)}, "Ada Lovelace"))
	must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Ines Ward", Company: "Harbourline"}, "Ada Lovelace"))
	if got := deliveriesOf(t, s, o.ID); len(got) != 0 {
		t.Fatalf("queued for a stopped endpoint: %+v", got)
	}
	must(s.ChangeWebhook(o.ID, e.ID, webhooks.WebhookChange{Enabled: new(true)}, "Ada Lovelace"))
	if err := s.RevokeKey(o.ID, e.KeyID, "test"); err != nil {
		t.Fatal(err)
	}
	must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Olu Bright", Company: "Brightworks"}, "Ada Lovelace"))
	if got := deliveriesOf(t, s, o.ID); len(got) != 0 {
		t.Fatalf("queued for a revoked key: %+v", got)
	}
}

func TestAFailingEndpointIsTriedAgainThenStopped(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	rcv := newHookReceiver(t)
	e, _, d := hooked(t, s, o, rcv, "contact.added")
	rcv.status = http.StatusInternalServerError
	must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts"}, "Ada Lovelace"))
	now := time.Now()
	d.Now = func() time.Time { return now }
	d.Round(context.Background())
	got := deliveriesOf(t, s, o.ID)[0]
	if got.Status != store.DeliveryPending || got.Attempts != 1 || got.LastCode != 500 || !got.NextAt.After(now) {
		t.Fatalf("after one failure: %+v", got)
	}
	if d.Round(context.Background()) != 0 {
		t.Fatal("tried again before its time")
	}
	// Three days on, the last try fails too, and the endpoint is stopped.
	now = got.RetryUntil.Add(-time.Minute)
	d.Round(context.Background())
	if got := deliveriesOf(t, s, o.ID)[0]; got.Status != store.DeliveryFailed {
		t.Fatalf("after its window: %+v", got)
	}
	if stopped := must(s.Webhook(o.ID, e.ID)); stopped.DisabledAt == nil || !strings.Contains(stopped.DisabledWhy, "three days") {
		t.Fatalf("not stopped: %+v", stopped)
	}
	var said int64
	s.DB.Model(&store.Event{}).Where("org_id = ? and action = ? and \"user\" = ?", o.ID, store.ActEndpointSet.String(), "Prospector").Count(&said)
	if said != 1 {
		t.Fatalf("stopping it isn't in the history: %d", said)
	}
}

func TestAnEndpointThatIsGoneIsStoppedAtOnce(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	rcv := newHookReceiver(t)
	e, _, d := hooked(t, s, o, rcv, "contact.added")
	rcv.status = http.StatusGone
	must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts"}, "Ada Lovelace"))
	d.Round(context.Background())
	if got := deliveriesOf(t, s, o.ID)[0]; got.Status != store.DeliveryFailed || got.LastCode != http.StatusGone {
		t.Fatalf("%+v", got)
	}
	if stopped := must(s.Webhook(o.ID, e.ID)); stopped.DisabledAt == nil || !strings.Contains(stopped.DisabledWhy, "410") {
		t.Fatalf("not stopped: %+v", stopped)
	}
}

// TestADeliveryThatMayNoLongerGoIsCancelled: queued, then its key revoked before it went.
func TestADeliveryThatMayNoLongerGoIsCancelled(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	rcv := newHookReceiver(t)
	e, _, d := hooked(t, s, o, rcv, "contact.added")
	must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts"}, "Ada Lovelace"))
	if err := s.RevokeKey(o.ID, e.KeyID, "test"); err != nil {
		t.Fatal(err)
	}
	d.Round(context.Background())
	if got := deliveriesOf(t, s, o.ID)[0]; got.Status != store.DeliveryCancelled || len(rcv.got) != 0 {
		t.Fatalf("%+v, %d sent", got, len(rcv.got))
	}
}

// TestANewSecretSignsBesideTheOld: for a day after a new secret, both sign.
func TestANewSecretSignsBesideTheOld(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	rcv := newHookReceiver(t)
	e, _, d := hooked(t, s, o, rcv, "contact.added")
	must(s.RotateWebhookSecret(o.ID, e.ID, "Ada Lovelace"))
	must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts"}, "Ada Lovelace"))
	d.Round(context.Background())
	if sigs := strings.Fields(rcv.got[0].Header.Get("webhook-signature")); len(sigs) != 2 {
		t.Fatalf("signatures: %v", sigs)
	}
}
