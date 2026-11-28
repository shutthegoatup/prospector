package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// backoff is how long after each failed attempt the next is made, as Standard Webhooks
// suggests; the last repeats until the delivery's window closes.
var backoff = []time.Duration{5 * time.Second, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 5 * time.Hour, 10 * time.Hour}

const (
	// lease is how long a claimed delivery is left alone: past any one attempt's timeout.
	lease   = 2 * time.Minute
	timeout = 15 * time.Second
	batch   = 20
)

// Deliverer sends what is due, as long as Run runs.
type Deliverer struct {
	Store *WebhookStore
	// Client reaches the endpoints; internal/egress keeps it off this host and private networks.
	Client *http.Client
	// Base is the server's own address, which the events' links are on.
	Base string
	// Now and Jitter stand in for the clock and chance in tests; nil is the real ones.
	Now    func() time.Time
	Jitter func() float64
	wg     sync.WaitGroup
}

func (d *Deliverer) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Sign is a Standard Webhooks signature, "v1," and the HMAC-SHA256 of
// "{id}.{timestamp}.{body}" under the secret, base64.
func Sign(secret, id string, at time.Time, body []byte) (string, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil {
		return "", fmt.Errorf("a webhook secret that isn't one: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = fmt.Fprintf(mac, "%s.%d.", id, at.Unix())
	mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

// payload is what is sent: what happened, when, and to which records, with links to read
// them through the API with the endpoint's key. Nothing about anyone is in it.
type payload struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      eventData `json:"data"`
}

type eventData struct {
	Organisation string            `json:"organisation"`
	Action       string            `json:"action"`
	CompanyID    int64             `json:"company_id,omitempty"`
	ContactID    int64             `json:"contact_id,omitempty"`
	TaskID       int64             `json:"task_id,omitempty"`
	Links        map[string]string `json:"links"`
}

func (d *Deliverer) payloadOf(due Due) payload {
	e, base := due.Event, strings.TrimRight(d.Base, "/")+"/o/"+due.Slug+"/api/v1/"
	data := eventData{Organisation: due.Slug, Action: e.Action, ContactID: e.ContactID, TaskID: e.TaskID, Links: map[string]string{}}
	if e.CompanyID != nil && *e.CompanyID != 0 {
		data.CompanyID = *e.CompanyID
		data.Links["company"] = base + "companies/" + strconv.FormatInt(*e.CompanyID, 10)
	}
	if e.ContactID != 0 {
		data.Links["contact"] = base + "contacts/" + strconv.FormatInt(e.ContactID, 10)
	}
	if e.TaskID != 0 {
		data.Links["task"] = base + "tasks/" + strconv.FormatInt(e.TaskID, 10)
	}
	return payload{Type: due.Type, Timestamp: e.At.UTC(), Data: data}
}

// signatures are the delivery's webhook-signature: one by its secret, and one by the
// secret it replaced while that still signs.
func (d *Deliverer) signatures(due Due, at time.Time, body []byte) (string, error) {
	var out []string
	secrets := []string{due.Endpoint.Secret}
	if due.Endpoint.PreviousSecret != "" && due.Endpoint.PreviousUntil != nil && at.Before(*due.Endpoint.PreviousUntil) {
		secrets = append(secrets, due.Endpoint.PreviousSecret)
	}
	for _, sealed := range secrets {
		secret, err := d.Store.Keyring.Open(sealed)
		if err != nil {
			return "", err
		}
		sig, err := Sign(secret, due.MessageID, at, body)
		if err != nil {
			return "", err
		}
		out = append(out, sig)
	}
	return strings.Join(out, " "), nil
}

// send makes one attempt and says what came of it: the status it answered with, or why
// it couldn't be asked, and how long it asked to be left.
func (d *Deliverer) send(ctx context.Context, due Due) (code int, why string, retryAfter time.Duration) {
	body, err := json.Marshal(d.payloadOf(due))
	if err != nil {
		return 0, err.Error(), 0
	}
	at := d.now()
	sigs, err := d.signatures(due, at, body)
	if err != nil {
		return 0, "its secret couldn't be opened: " + err.Error(), 0
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, due.Endpoint.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err.Error(), 0
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Prospector-Webhooks/1")
	req.Header.Set("webhook-id", due.MessageID)
	req.Header.Set("webhook-timestamp", strconv.FormatInt(at.Unix(), 10))
	req.Header.Set("webhook-signature", sigs)
	res, err := d.Client.Do(req)
	if err != nil {
		return 0, err.Error(), 0
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	if secs, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && secs > 0 {
		retryAfter = time.Duration(secs) * time.Second
	}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return res.StatusCode, "", 0
	}
	return res.StatusCode, fmt.Sprintf("it answered %s", res.Status), retryAfter
}

// next is when to try again after attempt n failed, with a tenth either way so that many
// failing together don't all come back at once.
func (d *Deliverer) next(n int, at time.Time, asked time.Duration) time.Time {
	wait := backoff[min(n, len(backoff))-1]
	spread := rand.Float64
	if d.Jitter != nil {
		spread = d.Jitter
	}
	wait = time.Duration(float64(wait) * (0.9 + 0.2*spread()))
	return at.Add(max(wait, asked))
}

// settle records what came of one attempt.
func (d *Deliverer) settle(ctx context.Context, due Due) error {
	if !due.Live {
		return d.Store.Cancel(due.OrgID, due.ID, "its endpoint was stopped or removed, its key revoked, or its event erased")
	}
	code, why, asked := d.send(ctx, due)
	switch {
	case why == "":
		return d.Store.Delivered(due.OrgID, due.ID, code, d.now())
	case code == http.StatusGone:
		return d.Store.GiveUp(due.OrgID, due.ID, due.EndpointID, code, "it answered 410 Gone")
	}
	at := d.now()
	if next := d.next(due.Attempts, at, asked); next.Before(due.RetryUntil) {
		return d.Store.Retry(due.OrgID, due.ID, code, why, next)
	}
	return d.Store.GiveUp(due.OrgID, due.ID, due.EndpointID, code, "it took nothing for three days: "+why)
}

// Round sends what is due now, a batch at a time, and says how many it settled.
func (d *Deliverer) Round(ctx context.Context) int {
	settled := 0
	for ctx.Err() == nil {
		due, err := d.Store.ClaimDue(d.now(), lease, batch)
		if err != nil {
			log.Printf("webhooks: %v", err)
			return settled
		}
		var wg sync.WaitGroup
		for _, one := range due {
			wg.Go(func() {
				if err := d.settle(ctx, one); err != nil {
					log.Printf("webhooks: delivery %d: %v", one.ID, err)
				}
			})
		}
		wg.Wait()
		settled += len(due)
		if len(due) < batch {
			return settled
		}
	}
	return settled
}

// Run sends what is due every so often until ctx ends; Wait waits for it to stop.
func (d *Deliverer) Run(ctx context.Context, every time.Duration) {
	d.wg.Go(func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			d.Round(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	})
}

func (d *Deliverer) Wait() { d.wg.Wait() }
