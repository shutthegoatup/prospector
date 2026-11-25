package main

import (
	"net/http"
	"os"
	"path/filepath"
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
	for _, text := range []string{"-5", "a lot", "1.234", ".", "1e9", "12345678901234"} {
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
