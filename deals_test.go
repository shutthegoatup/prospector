package main

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
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

// A database written before deals existed has replied contacts and no deals table.
func TestADatabaseFromBeforeDealsOpens(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "old.sqlite")
	if pg := os.Getenv("PROSPECTOR_TEST_PG"); pg != "" {
		dsn = pg
	}
	old := must(Open(dsn))
	if err := old.db.Migrator().DropTable(models...); err != nil {
		t.Fatal(err)
	}
	if err := old.db.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	o := mustOrg(t, old, "alpha")
	c := repliedPerson(t, old, o, "Hannah Pryce")
	if err := old.db.Migrator().DropTable(&Deal{}); err != nil {
		t.Fatal(err)
	}

	s := must(Open(dsn))
	if board := must(s.Conversations(o.ID)); len(board["replied"]) != 1 {
		t.Fatalf("the board after the upgrade: %+v", board)
	}
	if deals := must(s.Deals(o.ID)); len(deals) != 0 {
		t.Fatalf("deals nobody made: %+v", deals)
	}
	if err := s.Update(o.ID, c.ID, "deal_amount", "9,500", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if d := must(s.Deals(o.ID))[c.ID]; d.Amount != 950000 || d.Kind != DealNew {
		t.Errorf("the first deal after the upgrade: %+v", d)
	}
}

func TestTheBoardShowsWhatAStageIsWorth(t *testing.T) {
	s, o, srv, client := signedIn(t)
	first, second := repliedPerson(t, s, o, "Hannah Pryce"), repliedPerson(t, s, o, "Tom Ellery")
	update := func(id int64, field, value string) (int, string) {
		return call(t, client, "POST", srv.URL+"/o/alpha/ui/update", "application/json", map[string]any{"id": id, "field": field, "value": value})
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
		return call(t, client, "POST", srv.URL+"/o/alpha/ui/settings", "application/json", map[string]any{"name": "Alpha", "color": "#56c5f4", "currency": currency})
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

	_, page := call(t, client, "GET", srv.URL+"/o/alpha/conversations", "", nil)
	for _, want := range []string{"£14,000", "€5,000", "£14,000 + €5,000", "Ada Lovelace", `data-field="deal_close_date"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the board doesn't show %q", want)
		}
	}
	_, page = call(t, client, "GET", srv.URL+"/o/alpha/settings", "", nil)
	if !strings.Contains(page, `name="currency" value="EUR"`) {
		t.Error("Settings doesn't show the currency that was saved")
	}
}

// dealV1 is the deals table as the first release with deals made it: one per contact.
type dealV1 struct {
	ID        int64  `gorm:"primaryKey"`
	OrgID     int64  `gorm:"uniqueIndex:idx_deal_contact;not null"`
	ContactID int64  `gorm:"uniqueIndex:idx_deal_contact;not null"`
	Kind      string `gorm:"not null;default:'new'"`
	Amount    int64  `gorm:"not null;default:0"`
	Currency  string `gorm:"not null;default:''"`
	CloseDate *string
	Owner     string `gorm:"not null;default:''"`
	UpdatedAt time.Time
}

func (dealV1) TableName() string { return "deals" }

// A database from the first release with deals has rows without a seq, and an index
// that allows each contact one deal.
func TestDealsFromTheFirstReleaseBecomeTheConversationsOwn(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "old.sqlite")
	if pg := os.Getenv("PROSPECTOR_TEST_PG"); pg != "" {
		dsn = pg
	}
	old := must(Open(dsn))
	if err := old.db.Migrator().DropTable(models...); err != nil {
		t.Fatal(err)
	}
	if err := old.db.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	o := mustOrg(t, old, "alpha")
	valued, fresh := repliedPerson(t, old, o, "Hannah Pryce"), repliedPerson(t, old, o, "Tom Ellery")
	if err := old.db.Migrator().DropTable(&Deal{}); err != nil {
		t.Fatal(err)
	}
	if err := old.db.AutoMigrate(&dealV1{}); err != nil {
		t.Fatal(err)
	}
	if err := old.db.Create(&dealV1{OrgID: o.ID, ContactID: valued.ID, Kind: DealNew, Amount: 1400000, Currency: "GBP"}).Error; err != nil {
		t.Fatal(err)
	}

	s := must(Open(dsn))
	if !s.db.Migrator().HasIndex(&Deal{}, "idx_deal_seq") {
		t.Error("the upgrade didn't add the index that tells a contact's deals apart")
	}
	for _, c := range []*Contact{valued, fresh} {
		if err := s.Update(o.ID, c.ID, "deal_close_date", "2026-12-12", "Ada Lovelace"); err != nil {
			t.Fatalf("setting a deal after the upgrade: %v", err)
		}
	}
	deals := must(s.Deals(o.ID))
	if d := deals[valued.ID]; len(deals) != 2 || d.Amount != 1400000 || d.Seq != 0 || deref(d.CloseDate) != "2026-12-12" {
		t.Errorf("the deals after the upgrade: %+v", deals)
	}

	// The index that held a contact to one deal is gone, so a renewal can be started.
	if s.db.Migrator().HasIndex(&Deal{}, "idx_deal_contact") {
		t.Error("the upgrade left the index that allows each contact one deal")
	}
	renewal, err := s.StartDeal(o.ID, valued.ID, "renewal", "Ada Lovelace")
	if err != nil || renewal.Seq != 1 {
		t.Fatalf("starting a renewal after the upgrade: %+v %v", renewal, err)
	}
	if d := must(s.Deals(o.ID))[valued.ID]; d.Amount != 1400000 || d.Seq != 0 {
		t.Errorf("the conversation's own deal, once it has a renewal beside it: %+v", d)
	}
	// Opening it again finds nothing to drop.
	if _, err := Open(dsn); err != nil {
		t.Errorf("opening the upgraded database a second time: %v", err)
	}
}

func TestAnotherDealWithSomeoneInConversation(t *testing.T) {
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
	if at := board["replied"]; len(at) != 1 || at[0].ID != expansion.ID || !at[0].needsNextStep("2026-11-25") {
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
	s, o, srv, client := signedIn(t)
	c := repliedPerson(t, s, o, "Hannah Pryce")
	post := func(path string, body map[string]any) (int, string) {
		return call(t, client, "POST", srv.URL+"/o/alpha/ui/"+path, "application/json", body)
	}
	if code, body := post("deal/start", map[string]any{"contact": c.ID, "kind": "renewal"}); code != http.StatusOK {
		t.Fatalf("starting a renewal: %d %s", code, body)
	}
	var renewal Deal
	if err := s.db.Where("org_id = ? and seq > 0", o.ID).First(&renewal).Error; err != nil {
		t.Fatal(err)
	}
	if code, body := post("deal", map[string]any{"id": renewal.ID, "field": "amount", "value": "9,500"}); code != http.StatusOK {
		t.Fatalf("valuing the renewal: %d %s", code, body)
	}
	if code, _ := post("deal", map[string]any{"id": renewal.ID, "field": "stage", "value": "signed"}); code == http.StatusOK {
		t.Error("a renewal was moved to a stage there isn't")
	}
	_, page := call(t, client, "GET", srv.URL+"/o/alpha/conversations", "", nil)
	for _, want := range []string{"Renewal", "with Hannah Pryce", "£9,500", `data-deal="` + strconv.FormatInt(renewal.ID, 10) + `"`, `data-form="ui/deal/start"`, "No next step yet"} {
		if !strings.Contains(page, want) {
			t.Errorf("the board doesn't show %q", want)
		}
	}
	if code, body := post("deal/remove", map[string]any{"id": renewal.ID}); code != http.StatusOK {
		t.Fatalf("removing the renewal: %d %s", code, body)
	}
	if _, page = call(t, client, "GET", srv.URL+"/o/alpha/conversations", "", nil); strings.Contains(page, "with Hannah Pryce") {
		t.Error("a removed renewal is still on the board")
	}
}

// A renewal has no card on Today, so the side panel says how many need moving on.
func TestTodayPointsAtDealsThatNeedANextStep(t *testing.T) {
	s, o, srv, client := signedIn(t)
	c := repliedPerson(t, s, o, "Hannah Pryce")
	const prompt = "Move renewals and other deals on"
	// The sidebar's count of open conversations, which a renewal adds to.
	sidebar := regexp.MustCompile(`Conversations</span>\s*<span[^>]*>(\d+)</span>`)
	open := func(page string) string {
		if m := sidebar.FindStringSubmatch(page); m != nil {
			return m[1]
		}
		return "none"
	}
	_, page := call(t, client, "GET", srv.URL+"/o/alpha/today", "", nil)
	if strings.Contains(page, prompt) || open(page) != "1" {
		t.Fatalf("before any renewal: prompt shown %v, sidebar counts %s", strings.Contains(page, prompt), open(page))
	}
	renewal := must(s.StartDeal(o.ID, c.ID, "renewal", "Ada Lovelace"))
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/today", "", nil); !strings.Contains(page, prompt) || open(page) != "2" {
		t.Errorf("with a renewal that has no next step: prompt shown %v, sidebar counts %s", strings.Contains(page, prompt), open(page))
	}
	if err := s.ChangeDeal(o.ID, renewal.ID, "stage", "won", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/today", "", nil); strings.Contains(page, prompt) || open(page) != "1" {
		t.Errorf("once the renewal is won: prompt shown %v, sidebar counts %s", strings.Contains(page, prompt), open(page))
	}
	if err := s.ChangeDeal(o.ID, renewal.ID, "stage", "proposal", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	for field, value := range map[string]string{"next_step": "Send renewal terms", "next_date": "2999-01-01"} {
		if err := s.ChangeDeal(o.ID, renewal.ID, field, value, "Ada Lovelace"); err != nil {
			t.Fatal(err)
		}
	}
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/today", "", nil); strings.Contains(page, prompt) {
		t.Error("Today still points at a renewal whose next step is a long way off")
	}
	if err := s.ChangeDeal(o.ID, renewal.ID, "next_date", "2020-01-01", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/today", "", nil); !strings.Contains(page, prompt) {
		t.Error("Today says nothing of a renewal whose next step is overdue")
	}
}
