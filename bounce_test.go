package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// postmark posts one webhook record as Postmark does, signed in with a slug and secret.
func postmark(t *testing.T, site, slug, secret string, record map[string]any) int {
	t.Helper()
	body, _ := json.Marshal(record)
	req, _ := http.NewRequest("POST", site+"/hooks/postmark", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(slug, secret)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func bounce(kind, email string) map[string]any {
	return map[string]any{"RecordType": "Bounce", "Type": kind, "Email": email, "MessageID": "b0e1c2d3-0000-4000-8000-000000000001", "ID": 4242}
}

func complaint(email string) map[string]any {
	return map[string]any{"RecordType": "SpamComplaint", "Email": email, "MessageID": "b0e1c2d3-0000-4000-8000-000000000002", "ID": 4343}
}

func reasons(t *testing.T, s *Store, org int64) map[string]string {
	t.Helper()
	rows, _, err := s.Suppressions(org, PageIn{})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range rows {
		out[r.Address] = r.Reason + " by " + r.By
	}
	return out
}

func TestPostmarkBouncesAndComplaintsStopEveryCampaign(t *testing.T) {
	t.Parallel()
	a, s, o, list, member, box := campaignSite(t)
	site := strings.TrimSuffix(list, "/o/alpha/api/v1/campaigns")
	settings := site + "/o/alpha/api/v1/mail-server"
	other := mustOrg(t, s, "beta")
	must(s.Suppress(other.ID, "keep@larkmoor.example", "Ada Lovelace"))

	// Nothing is taken until an owner makes the webhook, and then only with its secret.
	if code := postmark(t, site, "alpha", "", bounce("HardBounce", "hannah@larkmoor.example")); code != http.StatusUnauthorized {
		t.Errorf("a webhook nobody made: %d", code)
	}
	code, body := call(t, member, "POST", settings+"/bounce-hook", "application/json", map[string]any{})
	var hook struct{ URL, Username, Password string }
	if err := json.Unmarshal([]byte(body), &hook); code != http.StatusOK || err != nil || hook.Username != "alpha" || len(hook.Password) < 32 {
		t.Fatalf("making the webhook: %d %s", code, body)
	}
	if want := strings.Replace(testSite, "https://", "https://alpha:"+hook.Password+"@", 1) + "/hooks/postmark"; hook.URL != want {
		t.Errorf("the webhook's address is %q, not %q", hook.URL, want)
	}
	if _, body := call(t, member, "GET", settings, "", nil); !strings.Contains(body, `"has_bounce_hook":true`) || strings.Contains(body, hook.Password) {
		t.Errorf("the mail server after making the webhook: %s", body)
	}
	for _, wrong := range [][2]string{{"alpha", "not-the-secret"}, {"beta", hook.Password}, {"", hook.Password}} {
		if code := postmark(t, site, wrong[0], wrong[1], bounce("HardBounce", "hannah@larkmoor.example")); code != http.StatusUnauthorized {
			t.Errorf("signed in as %q with the wrong secret: %d", wrong[0], code)
		}
	}
	if got := reasons(t, s, o.ID); len(got) != 0 {
		t.Fatalf("a webhook that didn't sign in changed the list: %v", got)
	}

	c := draft(t, s, o)
	must(s.SendCampaign(o, c.ID, "Ada Lovelace"))
	a.sendRound(context.Background(), time.Now())
	if len(box.to()) != 3 {
		t.Fatalf("the campaign went to %v", box.to())
	}
	tell := func(record map[string]any) {
		t.Helper()
		if code := postmark(t, site, "alpha", hook.Password, record); code != http.StatusNoContent {
			t.Errorf("%v: %d", record, code)
		}
	}
	tell(bounce("HardBounce", "Hannah@Larkmoor.example"))
	tell(bounce("HardBounce", "hannah@larkmoor.example")) // Postmark retries; once is the same as twice.
	tell(bounce("SoftBounce", "tom@larkmoor.example"))
	tell(complaint("sven@nordkap.example"))
	tell(map[string]any{"RecordType": "Delivery", "Email": "tom@larkmoor.example", "MessageID": "b0e1c2d3-0000-4000-8000-000000000003"})

	want := map[string]string{"hannah@larkmoor.example": "bounced by Postmark", "sven@nordkap.example": "complained by Postmark"}
	if got := reasons(t, s, o.ID); len(got) != 2 || got["hannah@larkmoor.example"] != want["hannah@larkmoor.example"] || got["sven@nordkap.example"] != want["sven@nordkap.example"] {
		t.Errorf("the list: got %v, want %v", got, want)
	}
	if got := reasons(t, s, other.ID); len(got) != 1 {
		t.Errorf("another organisation's list changed: %v", got)
	}
	mail := mailOf(t, s, o, c)
	if r := mail["hannah@larkmoor.example"]; r.Status != MailFailed || !strings.Contains(r.Reason, "bounced") {
		t.Errorf("the mail that bounced: %+v", r)
	}
	if mail["sven@nordkap.example"].Status != MailSent || mail["tom@larkmoor.example"].Status != MailSent {
		t.Errorf("mail that arrived: %+v", mail)
	}

	// Neither gets a service notice, which still reaches someone who only unsubscribed.
	var hannah Contact
	if err := s.db.Where("org_id = ? and email = ?", o.ID, "hannah@larkmoor.example").First(&hannah).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSubscription(o.ID, SubscriptionIn{Company: *hannah.CompanyID, Name: "Standard plan", Start: "2026-01-12", TermMonths: 12}, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	notice := must(s.AddCampaign(o.ID, CampaignIn{Name: "Price change", Kind: KindService, Audience: AudienceCustomers, Subject: "Your price", Body: "It changes in March."}, "Ada Lovelace"))
	got := leftOut(must(s.Audience(o, *notice)))
	if got["Hannah Pryce"] != "mail to them bounced" || got["Sven Dahl"] != "marked a mail as spam" || got["Tom Ellery"] != "" {
		t.Errorf("a service notice's audience: %q", got)
	}

	// A bounce can be taken off, as when the address was mended; a complaint can't.
	ids := map[string]int64{}
	rows, _, _ := s.Suppressions(o.ID, PageIn{})
	for _, r := range rows {
		ids[r.Address] = r.ID
	}
	if err := s.Unsuppress(o.ID, ids["sven@nordkap.example"], "Ada Lovelace"); err == nil {
		t.Error("a complaint was taken off the list")
	}
	if err := s.Unsuppress(o.ID, ids["hannah@larkmoor.example"], "Ada Lovelace"); err != nil {
		t.Errorf("taking a bounce off: %v", err)
	}

	// Without the webhook, its secret stops working.
	if code, _ := call(t, member, "DELETE", settings+"/bounce-hook", "", nil); code != http.StatusNoContent {
		t.Errorf("stopping the webhook: %d", code)
	}
	if code := postmark(t, site, "alpha", hook.Password, bounce("HardBounce", "tom@larkmoor.example")); code != http.StatusUnauthorized {
		t.Errorf("a stopped webhook: %d", code)
	}
}

func TestTheirOwnWordOutranksABounce(t *testing.T) {
	t.Parallel()
	s, o, _, _ := signedIn(t)
	if _, err := s.suppress(s.db, o.ID, "a@larkmoor.example", SuppressUnsubscribed, ""); err != nil {
		t.Fatal(err)
	}
	must(s.Suppress(o.ID, "b@larkmoor.example", "Ada Lovelace"))
	for _, addr := range []string{"a@larkmoor.example", "b@larkmoor.example"} {
		if err := s.mailFailed(o.ID, addr, SuppressBounced, "it bounced (HardBounce)"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.mailFailed(o.ID, "b@larkmoor.example", SuppressComplained, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.suppress(s.db, o.ID, "b@larkmoor.example", SuppressUnsubscribed, ""); err != nil {
		t.Fatal(err)
	}
	got := reasons(t, s, o.ID)
	if got["a@larkmoor.example"] != "unsubscribed by " || got["b@larkmoor.example"] != "complained by Postmark" {
		t.Errorf("the list: %v", got)
	}
}
