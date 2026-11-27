package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/internal/mailer"
)

// postbox records what a campaign would send and can be told how each mail goes.
type postbox struct {
	mu   sync.Mutex
	sent []mailer.Message
	// answer, when set, decides how a mail to that address goes.
	answer func(to string) error
}

func (p *postbox) send(_ context.Context, _ mailer.Server, m mailer.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.answer != nil {
		if err := p.answer(m.To); err != nil {
			return err
		}
	}
	p.sent = append(p.sent, m)
	return nil
}

func (p *postbox) to() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, m := range p.sent {
		out = append(out, m.To)
	}
	return out
}

// campaignSite is a server whose organisation can send a campaign: its own mail
// server, a postal address, an https address, and three people in conversation.
func campaignSite(t *testing.T) (*App, *Store, *Org, string, *http.Client, *postbox) {
	t.Helper()
	box := &postbox{}
	var app *App
	s, srv, member, _ := mailSite(t, func(a *App) { a.send, app = box.send, a })
	o := must(s.OrgBySlug("alpha"))
	o.Settings.PostalAddress = "Alpha Ltd\n1 Example Street\nLeith EH6 1AA"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveOrgMail(&OrgMail{OrgID: o.ID, Host: "smtp.alpha.example", Port: 587, Username: "news@alpha.example", Sender: "Alpha <news@alpha.example>"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct{ name, email string }{{"Hannah Pryce", "hannah@larkmoor.example"}, {"Tom Ellery", "tom@larkmoor.example"}, {"Sven Dahl", "sven@nordkap.example"}} {
		person(t, s, o, p.name, "Larkmoor Yachts", p.email, true)
	}
	return app, s, o, srv.URL + "/o/alpha/api/v1/campaigns", member, box
}

func draft(t *testing.T, s *Store, o *Org) *Campaign {
	t.Helper()
	return must(s.AddCampaign(o.ID, CampaignIn{Name: "Spring price list", Subject: "Our spring prices", Body: "Prices change on 1 March."}, "Ada Lovelace"))
}

func mailOf(t *testing.T, s *Store, o *Org, c *Campaign) map[string]CampaignRecipient {
	t.Helper()
	out := map[string]CampaignRecipient{}
	for _, r := range must(s.CampaignRecipients(o.ID, c.ID)) {
		out[r.Address] = r
	}
	return out
}

func TestACampaignIsSentOnceToEachPersonThroughTheOrganisationsOwnServer(t *testing.T) {
	t.Parallel()
	a, s, o, list, member, box := campaignSite(t)
	c := draft(t, s, o)
	code, body := call(t, member, "POST", list+"/"+itoa(c.ID)+"/send", "application/json", map[string]any{})
	if code != http.StatusAccepted || !strings.Contains(body, `"status":"sending"`) || !strings.Contains(body, `"queued":3`) {
		t.Fatalf("sending: %d %s", code, body)
	}
	if code, _ := call(t, member, "POST", list+"/"+itoa(c.ID)+"/send", "application/json", map[string]any{}); code == http.StatusAccepted {
		t.Error("a campaign was sent twice")
	}
	if code, _ := call(t, member, "PATCH", list+"/"+itoa(c.ID), "application/json", map[string]any{"body": "Changed after"}); code != http.StatusBadRequest {
		t.Errorf("changing a campaign that is sending: %d", code)
	}
	a.sendRound(context.Background(), time.Now())
	if got := box.to(); len(got) != 3 {
		t.Fatalf("one round sent %v", got)
	}
	m := box.sent[0]
	for _, want := range []string{"Prices change on 1 March.", "1 Example Street", "To stop these mails: " + testSite + "/u/"} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("a campaign's mail lacks %q:\n%s", want, m.Body)
		}
	}
	if m.Subject != "Our spring prices" || !strings.HasPrefix(m.Unsubscribe, testSite+"/u/") {
		t.Errorf("subject %q, unsubscribe %q", m.Subject, m.Unsubscribe)
	}
	// Run again, and again: nobody is mailed twice, and the campaign is done.
	a.sendRound(context.Background(), time.Now())
	a.sendRound(context.Background(), time.Now())
	if got := box.to(); len(got) != 3 {
		t.Errorf("rounds after the last mailed %v", got)
	}
	if now := must(s.Campaign(o.ID, c.ID)); now.Status != CampaignSent || now.SentAt == nil || now.QueuedBy != "Ada Lovelace" {
		t.Errorf("the campaign after its last mail: %+v", now)
	}
	code, body = call(t, member, "GET", list+"/"+itoa(c.ID)+"/mail", "", nil)
	if code != http.StatusOK || strings.Count(body, `"status":"sent"`) != 3 {
		t.Errorf("its mail: %d %s", code, body)
	}

	// The unsubscribe link in the mail works, and that person gets no more.
	token := strings.TrimPrefix(m.Unsubscribe, testSite+"/u/")
	if err := s.UnsubscribeByToken(token); err != nil {
		t.Fatal(err)
	}
	if err := s.marketable(s.db, o, m.To, ""); err != errSuppressed {
		t.Errorf("after unsubscribing by the mail's link: %v", err)
	}
}

func TestACampaignIsRefusedWhereItCannotBeSentProperly(t *testing.T) {
	t.Parallel()
	_, s, o, list, member, _ := campaignSite(t)
	c := draft(t, s, o)
	send := func() (int, string) {
		return call(t, member, "POST", list+"/"+itoa(c.ID)+"/send", "application/json", map[string]any{})
	}
	if err := s.DeleteOrgMail(o.ID); err != nil {
		t.Fatal(err)
	}
	if code, body := send(); code != http.StatusBadRequest || !strings.Contains(body, "own mail server") {
		t.Errorf("sending through the server's own mail: %d %s", code, body)
	}
	if err := s.SaveOrgMail(&OrgMail{OrgID: o.ID, Host: "smtp.alpha.example", Port: 587, Username: "news@alpha.example", Sender: "Alpha <news@alpha.example>"}); err != nil {
		t.Fatal(err)
	}
	o.Settings.PostalAddress = " "
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	if code, body := send(); code != http.StatusBadRequest || !strings.Contains(body, "postal address") {
		t.Errorf("sending with no postal address: %d %s", code, body)
	}
	if now := must(s.Campaign(o.ID, c.ID)); now.Status != CampaignDraft {
		t.Errorf("a refused send left it %s", now.Status)
	}
	// The page offers to send only when the list says nothing stops it.
	if _, body := call(t, member, "GET", list, "", nil); !strings.Contains(body, `"not_ready":"`) || strings.Contains(body, `"not_ready":""`) || !strings.Contains(body, "postal address") {
		t.Errorf("the campaigns list should say why nothing can be sent: %s", body)
	}

	// A key needs the scope to send.
	o.Settings.PostalAddress = "Alpha Ltd"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	writer, _, err := s.CreateKey(o.ID, "writer", []string{"campaigns:write"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	if code, body := asKey(t, "POST", list+"/"+itoa(c.ID)+"/send", writer, map[string]any{}); code != http.StatusForbidden || !strings.Contains(body, "campaigns:send") {
		t.Errorf("a key without campaigns:send sending: %d %s", code, body)
	}
}

func TestACampaignOnlyGoesWhereItShould(t *testing.T) {
	t.Parallel()
	a, s, o, _, _, box := campaignSite(t)
	c := draft(t, s, o)
	must(s.SendCampaign(o, c.ID, "Ada Lovelace"))
	// Someone who unsubscribes, or is put on the never-contact list, after it was sent
	// gets nothing.
	must(s.Suppress(o.ID, "tom@larkmoor.example", "Ada Lovelace"))
	o.Settings.NeverContact = "nordkap.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	a.sendRound(context.Background(), time.Now())
	if got := box.to(); len(got) != 1 || got[0] != "hannah@larkmoor.example" {
		t.Errorf("mailed %v", got)
	}
	mail := mailOf(t, s, o, c)
	if mail["tom@larkmoor.example"].Status != MailSkipped || mail["sven@nordkap.example"].Status != MailSkipped || !strings.Contains(mail["sven@nordkap.example"].Reason, "never-contact") {
		t.Errorf("those left out after it was sent: %+v", mail)
	}
}

func TestAMailServerThatIsDownPausesACampaignWithoutLosingAnyone(t *testing.T) {
	t.Parallel()
	a, s, o, list, member, box := campaignSite(t)
	c := draft(t, s, o)
	must(s.SendCampaign(o, c.ID, "Ada Lovelace"))
	box.answer = func(string) error { return fmt.Errorf("%w: reach smtp.alpha.example:587: refused", mailer.ErrUnsent) }
	for range failuresToPause {
		a.sendRound(context.Background(), time.Now())
	}
	now := must(s.Campaign(o.ID, c.ID))
	if now.Status != CampaignPaused || !strings.Contains(now.Paused, "mail server") {
		t.Fatalf("after the mail server failed %d times: %+v", failuresToPause, now)
	}
	for addr, r := range mailOf(t, s, o, c) {
		if r.Status != MailQueued {
			t.Errorf("%s is %s after a send that certainly didn't go", addr, r.Status)
		}
	}
	// Paused, nothing goes until someone resumes it.
	box.answer = nil
	a.sendRound(context.Background(), time.Now())
	if len(box.to()) != 0 {
		t.Error("a paused campaign sent mail")
	}
	if code, body := call(t, member, "POST", list+"/"+itoa(c.ID)+"/resume", "application/json", map[string]any{}); code != http.StatusOK {
		t.Fatalf("resuming: %d %s", code, body)
	}
	a.sendRound(context.Background(), time.Now())
	if got := box.to(); len(got) != 3 {
		t.Errorf("after resuming: %v", got)
	}
}

func TestAMailThatMayHaveGoneIsNeverSentAgain(t *testing.T) {
	t.Parallel()
	a, s, o, _, _, box := campaignSite(t)
	c := draft(t, s, o)
	must(s.SendCampaign(o, c.ID, "Ada Lovelace"))
	box.answer = func(to string) error {
		switch to {
		case "tom@larkmoor.example":
			return errors.New("connection reset while the message was being written")
		case "sven@nordkap.example":
			return fmt.Errorf("%w: 550 no such user", mailer.ErrRefusedRecipient)
		}
		return nil
	}
	a.sendRound(context.Background(), time.Now())
	box.answer = nil
	a.sendRound(context.Background(), time.Now())
	if got := box.to(); len(got) != 1 || got[0] != "hannah@larkmoor.example" {
		t.Errorf("mailed %v", got)
	}
	mail := mailOf(t, s, o, c)
	if mail["tom@larkmoor.example"].Status != MailUnknown || mail["sven@nordkap.example"].Status != MailFailed {
		t.Errorf("an uncertain send and a refused address: %+v", mail)
	}
	if now := must(s.Campaign(o.ID, c.ID)); now.Status != CampaignSent {
		t.Errorf("the campaign is %s", now.Status)
	}
	var links int64
	if err := s.db.Model(&UnsubscribeLink{}).Where("address = ?", "sven@nordkap.example").Count(&links).Error; err != nil || links != 0 {
		t.Errorf("a mail that was refused left %d unsubscribe links (%v)", links, err)
	}
}

func TestAMailClaimedByAServerThatStoppedIsGivenUp(t *testing.T) {
	t.Parallel()
	a, s, o, _, _, box := campaignSite(t)
	c := draft(t, s, o)
	must(s.SendCampaign(o, c.ID, "Ada Lovelace"))
	// As if a server claimed one and stopped before settling it.
	long := time.Now().Add(-2 * claimStale)
	if err := s.db.Model(&CampaignRecipient{}).Where("address = ?", "hannah@larkmoor.example").Updates(map[string]any{"status": MailSending, "claimed_at": long}).Error; err != nil {
		t.Fatal(err)
	}
	a.sendRound(context.Background(), time.Now())
	a.sendRound(context.Background(), time.Now())
	for _, to := range box.to() {
		if to == "hannah@larkmoor.example" {
			t.Error("a mail claimed by a server that stopped was sent again")
		}
	}
	if r := mailOf(t, s, o, c)["hannah@larkmoor.example"]; r.Status != MailUnknown {
		t.Errorf("a stale claim is %s", r.Status)
	}
	if now := must(s.Campaign(o.ID, c.ID)); now.Status != CampaignSent {
		t.Errorf("the campaign is %s", now.Status)
	}
}

func TestACancelledCampaignSendsNoMore(t *testing.T) {
	t.Parallel()
	a, s, o, list, member, box := campaignSite(t)
	c := draft(t, s, o)
	must(s.SendCampaign(o, c.ID, "Ada Lovelace"))
	if code, body := call(t, member, "POST", list+"/"+itoa(c.ID)+"/cancel", "application/json", map[string]any{}); code != http.StatusOK || !strings.Contains(body, `"cancelled":3`) {
		t.Fatalf("cancelling: %d %s", code, body)
	}
	a.sendRound(context.Background(), time.Now())
	if len(box.to()) != 0 {
		t.Error("a cancelled campaign sent mail")
	}
	if code, _ := call(t, member, "POST", list+"/"+itoa(c.ID)+"/resume", "application/json", map[string]any{}); code == http.StatusOK {
		t.Error("a cancelled campaign was resumed")
	}
}

// Asked to send twice at once, a campaign gets one set of mail.
func TestACampaignSentTwiceAtOnceIsSentOnce(t *testing.T) {
	t.Parallel()
	_, s, o, _, _, _ := campaignSite(t)
	c := draft(t, s, o)
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.SendCampaign(o, c.ID, "Ada Lovelace")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	ok := 0
	for err := range results {
		if err == nil {
			ok++
		}
	}
	if ok != 1 || len(mailOf(t, s, o, c)) != 3 {
		t.Errorf("%d sends were taken, with %d mails", ok, len(mailOf(t, s, o, c)))
	}
}

// Erasing someone takes their name and address off a campaign's mail, and keeps it counted.
func TestErasingSomeoneScrubsTheirCampaignMail(t *testing.T) {
	t.Parallel()
	a, s, o, _, _, _ := campaignSite(t)
	c := draft(t, s, o)
	must(s.SendCampaign(o, c.ID, "Ada Lovelace"))
	a.sendRound(context.Background(), time.Now())
	hannah := mailOf(t, s, o, c)["hannah@larkmoor.example"]
	if export := must(s.ExportPerson(o.ID, *hannah.ContactID, "Ada Lovelace")); len(export.CampaignMail) != 1 {
		t.Errorf("her export of campaign mail: %+v", export.CampaignMail)
	}
	if export := must(s.ExportOrg(o.ID, "Ada Lovelace")); len(export.CampaignMail) != 3 {
		t.Errorf("the organisation's export has %d of 3 mails", len(export.CampaignMail))
	}
	if err := s.ErasePerson(o.ID, *hannah.ContactID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	rows := must(s.CampaignRecipients(o.ID, c.ID))
	if len(rows) != 3 {
		t.Fatalf("erasing her left %d of 3 mails", len(rows))
	}
	for _, r := range rows {
		if strings.Contains(r.Address, "hannah") || r.Name == "Hannah Pryce" {
			t.Errorf("her mail still says who she was: %+v", r)
		}
	}
}

// A mail read for sending before its person was erased isn't sent, and leaves no
// unsubscribe link holding their address.
func TestAMailReadBeforeAnErasureIsNotSent(t *testing.T) {
	t.Parallel()
	a, s, o, _, _, box := campaignSite(t)
	c := must(s.SendCampaign(o, draft(t, s, o).ID, "Ada Lovelace"))
	via := must(a.campaignMail(o.ID))
	gate := must(s.gate(s.db, o))
	hannah := mailOf(t, s, o, c)["hannah@larkmoor.example"]

	if err := s.ErasePerson(o.ID, *hannah.ContactID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	a.sendOne(context.Background(), o, *c, via, gate, nil, hannah)
	if got := box.to(); len(got) != 0 {
		t.Fatalf("mail went to someone erased while it was being sent: %v", got)
	}
	var links int64
	s.db.Model(&UnsubscribeLink{}).Where("address = ?", hannah.Address).Count(&links)
	if links != 0 {
		t.Error("an unsubscribe link keeps the erased address")
	}
	if r := must(s.CampaignRecipients(o.ID, c.ID)); len(r) != 3 || slices.ContainsFunc(r, func(r CampaignRecipient) bool { return r.Status == MailSending }) {
		t.Errorf("the mail was claimed after the erasure: %+v", r)
	}
}

// What the review of sending found: a relay, a race to the host's mail, an erasure that
// stalled a campaign, refusals that used up the list, and a server that hangs.
func TestCampaignSendingHoldsAtItsEdges(t *testing.T) {
	t.Parallel()
	a, s, o, list, member, box := campaignSite(t)

	// Never straight to a recipient's server, and never without signing in.
	for _, m := range []OrgMail{{OrgID: o.ID, Host: "mx.larkmoor.example", Port: 25, Username: "x", Sender: "Alpha <news@alpha.example>"}, {OrgID: o.ID, Host: "smtp.alpha.example", Port: 587, Sender: "Alpha <news@alpha.example>"}} {
		if err := s.SaveOrgMail(&m); err != nil {
			t.Fatal(err)
		}
		c := draft(t, s, o)
		if code, body := call(t, member, "POST", list+"/"+itoa(c.ID)+"/send", "application/json", map[string]any{}); code != http.StatusBadRequest || !strings.Contains(body, "sign in to") {
			t.Errorf("sending through %s:%d with username %q: %d %s", m.Host, m.Port, m.Username, code, body)
		}
		if err := s.RemoveCampaign(o.ID, c.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveOrgMail(&OrgMail{OrgID: o.ID, Host: "smtp.alpha.example", Port: 587, Username: "news@alpha.example", Sender: "Alpha <news@alpha.example>"}); err != nil {
		t.Fatal(err)
	}
	// A server removed after a campaign is sent pauses it; it never falls back to the host's.
	c := draft(t, s, o)
	must(s.SendCampaign(o, c.ID, "Ada Lovelace"))
	if err := s.DeleteOrgMail(o.ID); err != nil {
		t.Fatal(err)
	}
	a.sendRound(context.Background(), time.Now())
	if len(box.to()) != 0 || must(s.Campaign(o.ID, c.ID)).Status != CampaignPaused {
		t.Fatalf("with its mail server gone: mailed %v, campaign %s", box.to(), must(s.Campaign(o.ID, c.ID)).Status)
	}
	if err := s.SaveOrgMail(&OrgMail{OrgID: o.ID, Host: "smtp.alpha.example", Port: 587, Username: "news@alpha.example", Sender: "Alpha <news@alpha.example>"}); err != nil {
		t.Fatal(err)
	}

	// Erasing someone whose mail is still waiting skips it, and the rest go.
	hannah := mailOf(t, s, o, c)["hannah@larkmoor.example"]
	if err := s.ErasePerson(o.ID, *hannah.ContactID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	must(s.ResumeCampaign(o.ID, c.ID))
	a.sendRound(context.Background(), time.Now())
	if got := box.to(); len(got) != 2 {
		t.Errorf("after erasing someone still waiting, mailed %v", got)
	}
	if now := must(s.Campaign(o.ID, c.ID)); now.Status != CampaignSent {
		t.Errorf("the campaign is %s", now.Status)
	}

	// A server refusing every address pauses the campaign before the list is used up.
	box.mu.Lock()
	box.sent = nil
	box.mu.Unlock()
	for _, who := range []string{"a@one.example", "b@two.example", "c@three.example", "d@four.example", "e@five.example"} {
		person(t, s, o, who, "Larkmoor Yachts", who, true)
	}
	second := draft(t, s, o)
	must(s.SendCampaign(o, second.ID, "Ada Lovelace"))
	box.answer = func(string) error { return fmt.Errorf("%w: 550 relaying denied", mailer.ErrRefusedRecipient) }
	a.sendRound(context.Background(), time.Now())
	refused := 0
	for _, r := range mailOf(t, s, o, second) {
		if r.Status == MailFailed {
			refused++
		}
	}
	if now := must(s.Campaign(o.ID, second.ID)); now.Status != CampaignPaused || refused != failuresToPause {
		t.Errorf("a server refusing everyone: campaign %s after %d refused", now.Status, refused)
	}
	box.answer = nil
}

func TestAServiceNoticeReachesCustomersWhoUnsubscribedButNobodyElse(t *testing.T) {
	t.Parallel()
	a, s, o, list, member, box := campaignSite(t)
	// Everyone made by campaignSite is at Larkmoor Yachts; make it a current customer.
	var hannah Contact
	if err := s.db.Where("org_id = ? and email = ?", o.ID, "hannah@larkmoor.example").First(&hannah).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSubscription(o.ID, SubscriptionIn{Company: *hannah.CompanyID, Name: "Standard plan", Start: "2026-01-12", TermMonths: 12}, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	person(t, s, o, "Never Mail", "Larkmoor Yachts", "never@larkmoor.example", false)
	o.Settings.NeverContact = "never@larkmoor.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	person(t, s, o, "Not A Customer", "Quayside Ltd", "cold@quayside.example", true)
	must(s.Suppress(o.ID, "tom@larkmoor.example", "Ada Lovelace"))

	// A service notice is for customers only.
	if code, body := call(t, member, "POST", list, "application/json", map[string]any{"name": "Prices", "kind": "service", "audience": "conversations"}); code != http.StatusBadRequest || !strings.Contains(body, "customers only") {
		t.Errorf("a service notice to people in conversation: %d %s", code, body)
	}
	code, body := call(t, member, "POST", list, "application/json", map[string]any{"name": "Price change", "kind": "service", "subject": "Your price from March", "body": "Your price changes on 1 March."})
	if code != http.StatusCreated || !strings.Contains(body, `"kind":"service"`) || !strings.Contains(body, `"audience":"customers"`) {
		t.Fatalf("starting a service notice: %d %s", code, body)
	}
	c := must(s.Campaigns(o.ID))[0]
	if code, _ := call(t, member, "PATCH", list+"/"+itoa(c.ID), "application/json", map[string]any{"audience": "conversations"}); code != http.StatusBadRequest {
		t.Errorf("moving a service notice to people in conversation: %d", code)
	}

	got := leftOut(must(s.Audience(o, c)))
	if got["Tom Ellery"] != "" || got["Never Mail"] != "on the never-contact list" {
		t.Errorf("its audience: unsubscribed %q, never-contact %q", got["Tom Ellery"], got["Never Mail"])
	}
	if _, there := got["Not A Customer"]; there {
		t.Error("someone at no current customer is in a service notice's audience")
	}

	must(s.SendCampaign(o, c.ID, "Ada Lovelace"))
	a.sendRound(context.Background(), time.Now())
	sent := map[string]mailer.Message{}
	for _, m := range box.sent {
		sent[m.To] = m
	}
	if _, ok := sent["tom@larkmoor.example"]; !ok {
		t.Error("a customer who unsubscribed from marketing did not get the service notice")
	}
	for _, nobody := range []string{"never@larkmoor.example", "cold@quayside.example"} {
		if _, ok := sent[nobody]; ok {
			t.Errorf("a service notice went to %s", nobody)
		}
	}
	m := sent["hannah@larkmoor.example"]
	if m.Unsubscribe != "" || strings.Contains(m.Body, "/u/") || !strings.Contains(m.Body, "as a customer of") || !strings.Contains(m.Body, "1 Example Street") {
		t.Errorf("a service notice's mail:\n%s\nunsubscribe %q", m.Body, m.Unsubscribe)
	}

	// A marketing campaign to the same people still leaves out who unsubscribed.
	box.mu.Lock()
	box.sent = nil
	box.mu.Unlock()
	marketing := must(s.AddCampaign(o.ID, CampaignIn{Name: "Offer", Audience: AudienceCustomers, Subject: "An offer", Body: "Buy more."}, "Ada Lovelace"))
	must(s.SendCampaign(o, marketing.ID, "Ada Lovelace"))
	a.sendRound(context.Background(), time.Now())
	for _, to := range box.to() {
		if to == "tom@larkmoor.example" {
			t.Error("a marketing campaign went to someone who unsubscribed")
		}
	}
}

// What the review of service notices found: a kind and audience that do not go together
// are refused when the campaign is sent, not only when it is written, and a new column
// reads right on rows that were there before it.
func TestAServiceNoticeIsCheckedAgainAsItIsSent(t *testing.T) {
	t.Parallel()
	_, s, o, _, _, _ := campaignSite(t)
	c := draft(t, s, o)
	// As a rollback to a release without kinds, or an edit racing the send, could leave it.
	if err := s.db.Model(&Campaign{}).Where("id = ?", c.ID).Update("kind", KindService).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendCampaign(o, c.ID, "Ada Lovelace"); err == nil || !strings.Contains(err.Error(), "customers only") {
		t.Errorf("a service notice to people in conversation was sent: %v", err)
	}
	if now := must(s.Campaign(o.ID, c.ID)); now.Status != CampaignDraft || len(mailOf(t, s, o, c)) != 0 {
		t.Errorf("the refused send left it %s with %d mails", now.Status, len(mailOf(t, s, o, c)))
	}

	// Changing a draft to a service notice takes it to customers.
	m := draft(t, s, o)
	changed := must(s.ChangeCampaign(o.ID, m.ID, []Change{{"kind", KindService}}, "Ada Lovelace"))
	if changed.Kind != KindService || changed.Audience != AudienceCustomers {
		t.Errorf("after making it a service notice: %s to %s", changed.Kind, changed.Audience)
	}
}

// A service notice skips the list not to be mailed only for customers, so a subscription
// that ends while the notice is still going out stops the rest of it reaching them.
func TestAServiceNoticeStopsAtSomeoneNoLongerACustomer(t *testing.T) {
	t.Parallel()
	a, s, o, _, _, box := campaignSite(t)
	var hannah Contact
	if err := s.db.Where("org_id = ? and email = ?", o.ID, "hannah@larkmoor.example").First(&hannah).Error; err != nil {
		t.Fatal(err)
	}
	sub := must(s.AddSubscription(o.ID, SubscriptionIn{Company: *hannah.CompanyID, Name: "Standard plan", Start: "2026-01-12", TermMonths: 12}, "Ada Lovelace"))
	must(s.Suppress(o.ID, "tom@larkmoor.example", "Ada Lovelace"))
	c := must(s.AddCampaign(o.ID, CampaignIn{Name: "Price change", Kind: KindService, Audience: AudienceCustomers, Subject: "Your price", Body: "It changes."}, "Ada Lovelace"))
	must(s.SendCampaign(o, c.ID, "Ada Lovelace"))
	if _, queued := mailOf(t, s, o, c)["tom@larkmoor.example"]; !queued {
		t.Fatal("the unsubscribed customer was not queued")
	}

	if err := s.db.Model(&Subscription{}).Where("id = ?", sub.ID).Update("ended_on", "2026-02-01").Error; err != nil {
		t.Fatal(err)
	}
	a.sendRound(context.Background(), time.Now())
	if to := box.to(); len(to) != 0 {
		t.Errorf("a service notice went to %v after their subscription ended", to)
	}
	if r := mailOf(t, s, o, c)["tom@larkmoor.example"]; r.Status != MailSkipped || r.Reason != "no longer a customer" {
		t.Errorf("the unsubscribed former customer's mail: %s %q", r.Status, r.Reason)
	}
}
