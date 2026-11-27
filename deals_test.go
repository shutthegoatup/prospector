package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/api"
)

func repliedPerson(t *testing.T, s *Store, o *Org, name string) *Contact {
	t.Helper()
	c := must(s.QuickAddPerson(o, QuickAdd{Name: name, Company: "Larkmoor Yachts"}, "Ada Lovelace"))
	if err := s.Mark(o.ID, c.ID, "replied", time.Now().Format("2006-01-02"), "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMoneyIsReadAndWritten(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]int64{"14,000": 1400000, "£14000.5": 1400050, "14 000.50": 1400050, "": 0, ".5": 50, "0": 0} {
		if got, err := parseMoney(text); err != nil || got != want {
			t.Errorf("%q read as %d (%v), want %d", text, got, err, want)
		}
	}
	for _, currency := range []string{"GBP", "EUR", "USD", "CHF", "SEK"} {
		for _, amount := range []int64{1, 99, 1400000, 140050, 100000000} {
			shown := formatMoney(amount, currency)
			if got, err := parseMoney(shown); err != nil || got != amount {
				t.Errorf("%q, as a card shows it, reads back as %d (%v), want %d", shown, got, err, amount)
			}
		}
	}
	for _, text := range []string{"-5", "a lot", "1.234", ".", "1e9", "12345678901234", "CHFS 5"} {
		if got, err := parseMoney(text); err == nil {
			t.Errorf("%q was read as %d", text, got)
		}
	}
	for want, in := range map[string]struct {
		amount   int64
		currency string
	}{"£14,000": {1400000, "GBP"}, "€1,400.50": {140050, "EUR"}, "CHF 999": {99900, "CHF"}, "$1,000,000": {100000000, "USD"}} {
		if got := formatMoney(in.amount, in.currency); got != want {
			t.Errorf("%d %s written as %q, want %q", in.amount, in.currency, got, want)
		}
	}
}

func TestADealIsValuedOnceSomeoneReplies(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	u := must(s.CreateUser("ada@example.com", "Ada Lovelace", "correct horse battery"))
	if err := s.AddMember(o.ID, u.ID, "owner", u.Name); err != nil {
		t.Fatal(err)
	}
	quiet := must(s.QuickAddPerson(o, QuickAdd{Name: "Sven Dahl", Company: "Nordkap Boats"}, u.Name))
	if err := s.Update(o.ID, quiet.ID, "deal_amount", "7500", u.Name); err == nil {
		t.Error("someone who hasn't replied was given a deal")
	}

	c := repliedPerson(t, s, o, "Hannah Pryce")
	set := func(field, value string) error { return s.Update(o.ID, c.ID, "deal_"+field, value, u.Name) }
	for field, value := range map[string]string{"amount": "plenty", "close_date": "next month", "kind": "gift", "owner": "Nobody Here", "colour": "red"} {
		if err := set(field, value); err == nil {
			t.Errorf("a deal's %s was set to %q", field, value)
		}
	}
	if deals := must(s.Deals(o.ID)); len(deals) != 0 {
		t.Fatalf("refused values left a deal behind: %+v", deals)
	}
	for _, step := range [][2]string{{"amount", "£14,000.50"}, {"close_date", "2026-12-12"}, {"kind", "renewal"}, {"owner", "Ada Lovelace"}} {
		if err := set(step[0], step[1]); err != nil {
			t.Fatalf("setting a deal's %s: %v", step[0], err)
		}
	}
	d, ok := must(s.Deals(o.ID))[c.ID]
	if !ok || d.Amount != 1400050 || d.Currency != "GBP" || deref(d.CloseDate) != "2026-12-12" || d.Kind != "renewal" || d.Owner != "Ada Lovelace" {
		t.Fatalf("the deal holds %+v", d)
	}
	if got := d.Value(); got != "£14,000.50" {
		t.Errorf("the deal's value reads %q", got)
	}
	var said []Event
	if err := s.db.Where("org_id = ? and contact_id = ? and action = ?", o.ID, c.ID, "deal:amount").Find(&said).Error; err != nil {
		t.Fatal(err)
	}
	if len(said) != 1 || deref(said[0].Value) != "£14,000.50" || said[0].User != "Ada Lovelace" {
		t.Errorf("the history of the value: %+v", said)
	}

	// The stage and next step are still the contact's, and the board still reads them.
	if err := s.Update(o.ID, c.ID, "stage", "proposal", u.Name); err != nil {
		t.Fatal(err)
	}
	if board := must(s.Conversations(o.ID)); len(board["proposal"]) != 1 {
		t.Errorf("the board after a deal was valued: %+v", board)
	}

	other := mustOrg(t, s, "beta")
	if err := s.Update(other.ID, c.ID, "deal_amount", "1", "Eve"); err == nil {
		t.Error("another organisation valued the deal")
	}
	if deals := must(s.Deals(other.ID)); len(deals) != 0 {
		t.Errorf("another organisation sees deals: %+v", deals)
	}
}

func TestADealIsInBothExportsAndGoesWithItsPerson(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	c := repliedPerson(t, s, o, "Hannah Pryce")
	if err := s.Update(o.ID, c.ID, "deal_amount", "20000", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if person := must(s.ExportPerson(o.ID, c.ID, "Ada Lovelace")); len(person.Deals) != 1 || person.Deals[0].Amount != 2000000 {
		t.Errorf("the person's export holds deals %+v", person.Deals)
	}
	if org := must(s.ExportOrg(o.ID, "Ada Lovelace")); len(org.Deals) != 1 || org.Deals[0].ContactID != c.ID {
		t.Errorf("the organisation's export holds deals %+v", org.Deals)
	}
	if err := s.ErasePerson(o.ID, c.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if deals := must(s.Deals(o.ID)); len(deals) != 0 {
		t.Errorf("erasing the person left their deal: %+v", deals)
	}
	var said []Event
	if err := s.db.Where("org_id = ? and action = ?", o.ID, "deal:amount").Find(&said).Error; err != nil {
		t.Fatal(err)
	}
	if len(said) != 1 || said[0].Value != nil || said[0].ContactID != 0 {
		t.Errorf("the history still says what an erased person's deal was worth: %+v", said)
	}
}

func TestADealKeepsItsCurrencyWhenTheSettingChanges(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	c := repliedPerson(t, s, o, "Hannah Pryce")
	set := func(value string) Deal {
		t.Helper()
		if err := s.Update(o.ID, c.ID, "deal_amount", value, "Ada Lovelace"); err != nil {
			t.Fatal(err)
		}
		return must(s.Deals(o.ID))[c.ID]
	}
	set("14,000")
	o.Settings.Currency = "EUR"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	if d := set("£14,500"); d.Value() != "£14,500" {
		t.Errorf("correcting a figure in pounds after the setting became euros left %q", d.Value())
	}
	set("")
	if d := set("9,000"); d.Value() != "€9,000" {
		t.Errorf("a deal cleared and valued afresh reads %q, not the new currency", d.Value())
	}
	var cleared []Event
	if err := s.db.Where("org_id = ? and action = ? and value is null", o.ID, "deal:amount").Find(&cleared).Error; err != nil {
		t.Fatal(err)
	}
	if len(cleared) != 1 {
		t.Errorf("clearing a value should be in the history once, with no figure: %+v", cleared)
	}
}

func TestADealFollowsItsPersonThroughARestore(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	stays, goes := repliedPerson(t, s, o, "Hannah Pryce"), repliedPerson(t, s, o, "Tom Ellery")
	for _, c := range []*Contact{stays, goes} {
		if err := s.Update(o.ID, c.ID, "deal_amount", "20000", "Ada Lovelace"); err != nil {
			t.Fatal(err)
		}
	}
	var renumbered Contact
	if err := s.db.First(&renumbered, stays.ID).Error; err != nil {
		t.Fatal(err)
	}
	renumbered.ID = goes.ID + 50
	if _, err := s.replaceContacts(o.ID, []Contact{renumbered}, nil); err != nil {
		t.Fatal(err)
	}
	deals := must(s.Deals(o.ID))
	if d, ok := deals[renumbered.ID]; len(deals) != 1 || !ok || d.Amount != 2000000 {
		t.Errorf("after a restore that renumbered one person and dropped another, the deals are %+v", deals)
	}
}

func TestTheBoardShowsWhatAStageIsWorth(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	first, second := repliedPerson(t, s, o, "Hannah Pryce"), repliedPerson(t, s, o, "Tom Ellery")
	update := func(id int64, field, value string) (int, string) {
		of, isDeal := strings.CutPrefix(field, "deal_")
		path := srv.URL + "/o/alpha/api/v1/contacts/" + strconv.FormatInt(id, 10)
		if isDeal {
			path += "/deal"
			field = of
		}
		code, body := call(t, client, "PATCH", path, "application/json", map[string]any{field: value})
		if code == http.StatusNoContent {
			code = http.StatusOK
		}
		return code, body
	}
	if code, body := update(first.ID, "deal_amount", "14,000"); code != http.StatusOK {
		t.Fatalf("valuing a deal: %d %s", code, body)
	}
	if code, body := update(first.ID, "deal_owner", "Ada Lovelace"); code != http.StatusOK {
		t.Fatalf("giving a deal an owner: %d %s", code, body)
	}
	if code, _ := update(first.ID, "deal_amount", "lots"); code == http.StatusOK {
		t.Error("a value that isn't an amount was saved")
	}

	settings := func(currency string) (int, string) {
		return call(t, client, "PATCH", srv.URL+"/o/alpha/api/v1/settings", "application/json", map[string]any{"name": "Alpha", "color": "#56c5f4", "currency": currency})
	}
	if code, body := settings("euros"); code != http.StatusBadRequest || !strings.Contains(body, "three letters") {
		t.Errorf("a currency that isn't a code was saved: %d %s", code, body)
	}
	if code, body := settings("eur"); code != http.StatusOK {
		t.Fatalf("saving a currency: %d %s", code, body)
	}
	if code, body := update(second.ID, "deal_amount", "5000"); code != http.StatusOK {
		t.Fatalf("valuing a deal in the new currency: %d %s", code, body)
	}

	// Each card keeps its own currency, so the stage's worth adds them up side by side.
	values := map[int64]string{}
	for _, c := range boardCards(t, client, srv.URL) {
		values[c.ContactId] = c.Value + " " + c.Owner
	}
	if values[first.ID] != "£14,000 Ada Lovelace" || values[second.ID] != "€5,000 " {
		t.Errorf("the board's cards: %v", values)
	}
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/settings", "", nil); !strings.Contains(body, `"currency":"EUR"`) {
		t.Errorf("Settings doesn't read back the currency that was saved: %s", body)
	}
}

// boardCards are the Conversations board's cards, as its page reads them.
func boardCards(t *testing.T, c *http.Client, base string) []api.DealCard {
	t.Helper()
	code, body := call(t, c, "GET", base+"/o/alpha/api/v1/conversations?limit=200", "", nil)
	var out struct{ Items []api.DealCard }
	if code != http.StatusOK || json.Unmarshal([]byte(body), &out) != nil {
		t.Fatalf("the board: %d %s", code, body)
	}
	return out.Items
}

func TestAnotherDealWithSomeoneInConversation(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	quiet := must(s.QuickAddPerson(o, QuickAdd{Name: "Sven Dahl", Company: "Nordkap Boats"}, "Ada Lovelace"))
	if _, err := s.StartDeal(o.ID, quiet.ID, "renewal", "Ada Lovelace"); err == nil {
		t.Error("a renewal was started with someone who hasn't replied")
	}
	c := repliedPerson(t, s, o, "Hannah Pryce")
	if _, err := s.StartDeal(o.ID, c.ID, "gift", "Ada Lovelace"); err == nil {
		t.Error("a deal of an unknown kind was started")
	}
	renewal := must(s.StartDeal(o.ID, c.ID, "renewal", "Ada Lovelace"))
	expansion := must(s.StartDeal(o.ID, c.ID, "expansion", "Ada Lovelace"))
	if renewal.Seq != 1 || expansion.Seq != 2 || renewal.Stage != "replied" || renewal.Currency != "GBP" {
		t.Fatalf("the deals started: %+v %+v", renewal, expansion)
	}

	change := func(field, value string) error { return s.ChangeDeal(o.ID, renewal.ID, field, value, "Ada Lovelace") }
	for field, value := range map[string]string{"stage": "signed", "next_date": "soon", "amount": "plenty", "colour": "red"} {
		if err := change(field, value); err == nil {
			t.Errorf("a renewal's %s was set to %q", field, value)
		}
	}
	for _, step := range [][2]string{{"amount", "9,500"}, {"stage", "proposal"}, {"next_step", "Send renewal terms"}, {"next_date", "2026-12-03"}, {"close_date", "2027-01-12"}} {
		if err := change(step[0], step[1]); err != nil {
			t.Fatalf("setting a renewal's %s: %v", step[0], err)
		}
	}
	// The conversation's own deal still takes its stage and next step from the contact.
	for _, field := range []string{"deal_stage", "deal_next_step", "deal_next_date", "deal_lost_reason"} {
		if err := s.Update(o.ID, c.ID, field, "2026-12-03", "Ada Lovelace"); err == nil {
			t.Errorf("the conversation's own deal was given a %s", field)
		}
	}
	if own := must(s.Deals(o.ID)); len(own) != 0 {
		t.Errorf("starting other deals made the conversation one of its own: %+v", own)
	}

	board := must(s.OtherDeals(o.ID))
	if at := board["proposal"]; len(at) != 1 || at[0].ID != renewal.ID || at[0].Value() != "£9,500" || at[0].NextStep != "Send renewal terms" || displayName(at[0].With) != "Hannah Pryce" {
		t.Errorf("the renewal on the board: %+v", board)
	}
	if at := board["replied"]; len(at) != 1 || at[0].ID != expansion.ID || !at[0].NeedsNextStep("2026-11-25") {
		t.Errorf("the expansion on the board: %+v", board)
	}
	var said []Event
	if err := s.db.Where("org_id = ? and contact_id = ? and action like ?", o.ID, c.ID, "deal:%").Order("id").Find(&said).Error; err != nil {
		t.Fatal(err)
	}
	if len(said) != 7 || said[0].Action != "deal:start" || deref(said[0].Value) != "renewal" || said[4].Action != "deal:next_step" || said[4].Value != nil {
		t.Errorf("the history of the two deals: %+v", said)
	}

	// Someone who later declines leaves the conversations, but their deals stay to be closed.
	if err := s.Mark(o.ID, c.ID, "declined", time.Now().Format("2006-01-02"), "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if left := must(s.OtherDeals(o.ID)); len(left["proposal"]) != 1 || len(left["replied"]) != 1 {
		t.Errorf("the deals of someone who declined left the board with nothing to close them from: %+v", left)
	}
	if _, err := s.StartDeal(o.ID, c.ID, "renewal", "Ada Lovelace"); err == nil {
		t.Error("another deal was started with someone who declined")
	}
	if err := change("stage", "lost"); err != nil {
		t.Errorf("closing the deal of someone who declined: %v", err)
	}
	if err := change("stage", "proposal"); err != nil {
		t.Fatal(err)
	}

	other := mustOrg(t, s, "beta")
	if err := s.ChangeDeal(other.ID, renewal.ID, "amount", "1", "Eve"); err == nil {
		t.Error("another organisation changed the renewal")
	}
	if err := s.RemoveDeal(other.ID, renewal.ID, "Eve"); err == nil {
		t.Error("another organisation removed the renewal")
	}
	if _, err := s.StartDeal(other.ID, c.ID, "renewal", "Eve"); err == nil {
		t.Error("another organisation started a deal with the first one's contact")
	}
	if theirs := must(s.OtherDeals(other.ID)); len(theirs) != 0 {
		t.Errorf("another organisation sees deals: %+v", theirs)
	}

	if err := s.Update(o.ID, c.ID, "deal_amount", "20000", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveDeal(o.ID, must(s.Deals(o.ID))[c.ID].ID, "Ada Lovelace"); err == nil {
		t.Error("the conversation's own deal was removed")
	}
	if err := s.RemoveDeal(o.ID, expansion.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if person := must(s.ExportPerson(o.ID, c.ID, "Ada Lovelace")); len(person.Deals) != 2 {
		t.Errorf("the person's export holds %d deals, want their own and the renewal", len(person.Deals))
	}
	if err := s.ErasePerson(o.ID, c.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	var left int64
	if err := s.db.Model(&Deal{}).Where("org_id = ?", o.ID).Count(&left).Error; err != nil || left != 0 {
		t.Errorf("erasing the person left %d of their deals (%v)", left, err)
	}
}

func TestARenewalIsStartedAndWorkedFromTheBoard(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	c := repliedPerson(t, s, o, "Hannah Pryce")
	if code, body := call(t, client, "POST", srv.URL+"/o/alpha/api/v1/deals", "application/json", map[string]any{"contact": c.ID, "kind": "renewal"}); code != http.StatusCreated {
		t.Fatalf("starting a renewal: %d %s", code, body)
	}
	var renewal Deal
	if err := s.db.Where("org_id = ? and seq > 0", o.ID).First(&renewal).Error; err != nil {
		t.Fatal(err)
	}
	if code, body := call(t, client, "PATCH", srv.URL+"/o/alpha/api/v1/deals/"+itoa(renewal.ID), "application/json", map[string]any{"amount": "9,500"}); code != http.StatusOK {
		t.Fatalf("valuing the renewal: %d %s", code, body)
	}
	if code, _ := call(t, client, "PATCH", srv.URL+"/o/alpha/api/v1/deals/"+itoa(renewal.ID), "application/json", map[string]any{"stage": "signed"}); code == http.StatusOK {
		t.Error("a renewal was moved to a stage there isn't")
	}
	renewalCard := func() *api.DealCard {
		for _, card := range boardCards(t, client, srv.URL) {
			if card.Id == renewal.ID {
				return &card
			}
		}
		return nil
	}
	if card := renewalCard(); card == nil || card.Kind != "renewal" || card.Person != "Hannah Pryce" || card.Value != "£9,500" || card.Seq == 0 || card.NextStep != "" {
		t.Errorf("the board's card for the renewal: %+v", card)
	}
	if code, body := call(t, client, "DELETE", srv.URL+"/o/alpha/api/v1/deals/"+itoa(renewal.ID), "", nil); code != http.StatusNoContent {
		t.Fatalf("removing the renewal: %d %s", code, body)
	}
	if card := renewalCard(); card != nil {
		t.Error("a removed renewal is still on the board")
	}
}

// A renewal has no card on Today, so the side panel says how many need moving on.
func TestTodayPointsAtDealsThatNeedANextStep(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	c := repliedPerson(t, s, o, "Hannah Pryce")
	// Today's side panel points at deals to move on when its summary counts any; the
	// sidebar counts open conversations, which a renewal adds to.
	state := func() (prompt bool, open int) {
		t.Helper()
		_, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/today/summary", "", nil)
		var sum api.TodaySummary
		if err := json.Unmarshal([]byte(body), &sum); err != nil {
			t.Fatalf("today's summary: %s", body)
		}
		_, body = call(t, client, "GET", srv.URL+"/o/alpha/api/v1/nav", "", nil)
		var n api.Nav
		if err := json.Unmarshal([]byte(body), &n); err != nil {
			t.Fatalf("the nav: %s", body)
		}
		return sum.Deals > 0, n.Counts.Conversations
	}
	if prompt, open := state(); prompt || open != 1 {
		t.Fatalf("before any renewal: prompt shown %v, sidebar counts %d", prompt, open)
	}
	renewal := must(s.StartDeal(o.ID, c.ID, "renewal", "Ada Lovelace"))
	if prompt, open := state(); !prompt || open != 2 {
		t.Errorf("with a renewal that has no next step: prompt shown %v, sidebar counts %d", prompt, open)
	}
	if err := s.ChangeDeal(o.ID, renewal.ID, "stage", "won", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if prompt, open := state(); prompt || open != 1 {
		t.Errorf("once the renewal is won: prompt shown %v, sidebar counts %d", prompt, open)
	}
	if err := s.ChangeDeal(o.ID, renewal.ID, "stage", "proposal", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	for field, value := range map[string]string{"next_step": "Send renewal terms", "next_date": "2999-01-01"} {
		if err := s.ChangeDeal(o.ID, renewal.ID, field, value, "Ada Lovelace"); err != nil {
			t.Fatal(err)
		}
	}
	if prompt, _ := state(); prompt {
		t.Error("Today still points at a renewal whose next step is a long way off")
	}
	if err := s.ChangeDeal(o.ID, renewal.ID, "next_date", "2020-01-01", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if prompt, _ := state(); !prompt {
		t.Error("Today says nothing of a renewal whose next step is overdue")
	}
}

func TestARobotWorksDealsAndEnquiriesAsItsScopesAllow(t *testing.T) {
	t.Parallel()
	s, o, srv, owner := signedIn(t)
	other := mustOrg(t, s, "beta")
	key := func(org int64, scopes ...string) string {
		secret, _, err := s.CreateKey(org, "robot "+strings.Join(scopes, "+"), scopes, "Ada Lovelace")
		if err != nil {
			t.Fatal(err)
		}
		return secret
	}
	reader, worker, deleter, stranger := key(o.ID, "read"), key(o.ID, "pipeline:write"), key(o.ID, "enquiries:delete"), key(other.ID, "read", "pipeline:write", "enquiries:delete")
	c := repliedPerson(t, s, o, "Hannah Pryce")
	base, theirs := srv.URL+"/o/alpha/api/v1/", srv.URL+"/o/beta/api/v1/"

	// A conversation has a deal of its own before anything is set on it.
	code, body := asKey(t, "GET", base+"deals", reader, nil)
	if code != http.StatusOK || !strings.Contains(body, `"person":"Hannah Pryce"`) || !strings.Contains(body, `"seq":0`) || !strings.Contains(body, `"stage":"replied"`) || !strings.Contains(body, `"kind":"new"`) {
		t.Fatalf("listing deals: %d %s", code, body)
	}
	own := base + "contacts/" + itoa(c.ID) + "/deal"
	if code, body := asKey(t, "PATCH", own, reader, map[string]any{"amount": "14,000"}); code != http.StatusForbidden || !strings.Contains(body, "pipeline:write") {
		t.Errorf("a read key valuing a deal: %d %s", code, body)
	}
	// Saved together or not at all.
	if code, body := asKey(t, "PATCH", own, worker, map[string]any{"amount": "14,000", "close_date": "soon"}); code != http.StatusBadRequest {
		t.Errorf("a change with a bad date: %d %s", code, body)
	}
	if deals := must(s.Deals(o.ID)); deals[c.ID].Amount != 0 {
		t.Errorf("the part before the refused one was saved: %+v", deals[c.ID])
	}
	if code, body := asKey(t, "PATCH", own, worker, map[string]any{"amount": "14,000", "close_date": "2027-01-31", "kind": "expansion"}); code != http.StatusOK || !strings.Contains(body, `"value":"£14,000"`) || !strings.Contains(body, `"close_date":"2027-01-31"`) || !strings.Contains(body, `"kind":"expansion"`) {
		t.Errorf("valuing the conversation's own deal: %d %s", code, body)
	}
	if code, body := asKey(t, "PATCH", own, worker, map[string]any{"stage": "won"}); code != http.StatusBadRequest || !strings.Contains(body, "contact") {
		t.Errorf("setting the own deal's stage here: %d %s", code, body)
	}

	code, body = asKey(t, "POST", base+"deals", worker, map[string]any{"contact": c.ID, "kind": "renewal"})
	if code != http.StatusCreated || !strings.Contains(body, `"seq":1`) || !strings.Contains(body, `"kind":"renewal"`) {
		t.Fatalf("starting another deal: %d %s", code, body)
	}
	var second Deal
	if err := s.db.Where("org_id = ? and seq = 1", o.ID).First(&second).Error; err != nil {
		t.Fatal(err)
	}
	one := "deals/" + itoa(second.ID)
	if code, body := asKey(t, "PATCH", base+one, worker, map[string]any{"stage": "proposal", "next_step": "Send the order form", "amount": "9,500"}); code != http.StatusOK || !strings.Contains(body, `"stage":"proposal"`) || !strings.Contains(body, `"next_step":"Send the order form"`) || !strings.Contains(body, `"value":"£9,500"`) {
		t.Errorf("working another deal: %d %s", code, body)
	}
	if code, body := asKey(t, "PATCH", base+one, worker, map[string]any{"next_step": "Chase", "stage": "signed"}); code != http.StatusBadRequest {
		t.Errorf("a stage that is not one: %d %s", code, body)
	} else if err := s.db.First(&second, second.ID).Error; err != nil || second.NextStep != "Send the order form" {
		t.Errorf("the part before the refused one was saved: %q (%v)", second.NextStep, err)
	}
	for what, try := range map[string][2]string{"changing": {"PATCH", theirs + one}, "removing": {"DELETE", theirs + one}, "valuing": {"PATCH", theirs + "contacts/" + itoa(c.ID) + "/deal"}} {
		var sent any
		if try[0] == "PATCH" {
			sent = map[string]any{"amount": "1"}
		}
		if code, body := asKey(t, try[0], try[1], stranger, sent); code != http.StatusNotFound {
			t.Errorf("another organisation %s our deal by number: %d %s", what, code, body)
		}
	}
	if code, _ := asKey(t, "GET", base+"deals", stranger, nil); code < 400 {
		t.Errorf("another organisation's key listing our deals: %d", code)
	}
	if code, body := asKey(t, "DELETE", base+one, worker, nil); code != http.StatusNoContent {
		t.Errorf("removing the deal: %d %s", code, body)
	}

	// With no kind given a deal is new business, and someone who replied and then declined
	// still has their own deal changed and read back, though the board no longer shows it.
	if code, body := asKey(t, "POST", base+"deals", worker, map[string]any{"contact": c.ID}); code != http.StatusCreated || !strings.Contains(body, `"kind":"new"`) {
		t.Errorf("starting a deal with no kind: %d %s", code, body)
	}
	gone := repliedPerson(t, s, o, "Tom Ellery")
	if err := s.Mark(o.ID, gone.ID, "declined", today(), "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if code, body := asKey(t, "PATCH", base+"contacts/"+itoa(gone.ID)+"/deal", worker, map[string]any{"amount": "5,000"}); code != http.StatusOK || !strings.Contains(body, `"value":"£5,000"`) {
		t.Errorf("valuing the deal of someone who has since declined: %d %s", code, body)
	}

	// Enquiries: listed, tracked, and deleted only by a key given that, or an owner.
	e := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Name: "Tom Ellery", Email: "tom@larkmoor.example", Message: "Do you cover Leith?"}))
	spam := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Name: "Bot", Email: "bot@spam.example", Message: "hello", Trap: "caught"}))
	code, body = asKey(t, "GET", base+"enquiries", reader, nil)
	if code != http.StatusOK || !strings.Contains(body, `"email":"tom@larkmoor.example"`) || strings.Contains(body, "bot@spam.example") || strings.Contains(body, `"next_cursor"`) || !strings.Contains(body, `"state":"open"`) {
		t.Errorf("listing enquiries: %d %s", code, body)
	}
	if code, body := asKey(t, "GET", base+"enquiries?spam=true", reader, nil); code != http.StatusOK || !strings.Contains(body, "bot@spam.example") {
		t.Errorf("listing spam: %d %s", code, body)
	}
	if code, body := asKey(t, "GET", base+"enquiries?state=closed", reader, nil); code != http.StatusOK || strings.Contains(body, "tom@larkmoor.example") {
		t.Errorf("listing closed ones: %d %s", code, body)
	}
	one = "enquiries/" + itoa(e.ID)
	if code, body := asKey(t, "PATCH", base+one, reader, map[string]any{"state": "closed"}); code != http.StatusForbidden {
		t.Errorf("a read key closing an enquiry: %d %s", code, body)
	}
	if code, body := asKey(t, "PATCH", base+one, worker, map[string]any{"note": "Rang them", "state": "ignored"}); code != http.StatusBadRequest {
		t.Errorf("a state that is not one: %d %s", code, body)
	}
	if code, body := asKey(t, "PATCH", base+one, worker, map[string]any{"note": "Rang them", "state": "waiting"}); code != http.StatusOK || !strings.Contains(body, `"note":"Rang them"`) || !strings.Contains(body, `"state":"waiting"`) {
		t.Errorf("tracking an enquiry: %d %s", code, body)
	}
	if code, body := asKey(t, "PATCH", theirs+one, stranger, map[string]any{"state": "closed"}); code != http.StatusNotFound {
		t.Errorf("another organisation closing our enquiry by number: %d %s", code, body)
	}
	if code, body := asKey(t, "POST", theirs+one+"/conversation", stranger, nil); code != http.StatusNotFound {
		t.Errorf("another organisation conversing with our enquiry by number: %d %s", code, body)
	}
	if code, body := asKey(t, "POST", base+one+"/conversation", worker, nil); code != http.StatusCreated || !strings.Contains(body, `"contact":`) {
		t.Errorf("starting a conversation from an enquiry: %d %s", code, body)
	}

	member := must(s.CreateUser("grace@example.com", "Grace Hopper", "correct horse battery"))
	if err := s.AddMember(o.ID, member.ID, "member", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	notOwner := newMailClient()
	if !signIn(t, notOwner, srv, "grace@example.com", "correct horse battery") {
		t.Fatal("Grace cannot sign in")
	}
	for who, refused := range map[string]func() (int, string){
		"a key without enquiries:delete": func() (int, string) { return asKey(t, "DELETE", base+"enquiries/"+itoa(spam.ID), worker, nil) },
		"a member who is not an owner":   func() (int, string) { return call(t, notOwner, "DELETE", base+"enquiries/"+itoa(spam.ID), "", nil) },
		"another organisation's key":     func() (int, string) { return asKey(t, "DELETE", theirs+"enquiries/"+itoa(spam.ID), stranger, nil) },
	} {
		if code, body := refused(); code < 400 {
			t.Errorf("%s deleted an enquiry: %d %s", who, code, body)
		}
	}
	if code, body := asKey(t, "DELETE", base+"enquiries/"+itoa(spam.ID), deleter, nil); code != http.StatusNoContent {
		t.Errorf("a key with enquiries:delete deleting: %d %s", code, body)
	}
	if code, body := call(t, owner, "DELETE", base+one, "", nil); code != http.StatusNoContent {
		t.Errorf("an owner deleting: %d %s", code, body)
	}
}
