package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
)

func TestAnEnquiryWaitsToBeAnswered(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	u := must(s.CreateUser("ada@example.com", "Ada Lovelace", "correct horse battery"))
	if err := s.AddMember(o.ID, u.ID, "owner", u.Name); err != nil {
		t.Fatal(err)
	}
	// Someone already known here, so what they send is matched to them and their company.
	known := must(s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, u.Name))
	help := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Kind: "support", Name: "Hannah Pryce", Email: "hannah@larkmoor.example", Message: "Colours look pale on phones"}))
	stranger := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Kind: "sales", Email: "sven@nordkap.example", Message: "What does it cost?"}))
	spam := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Email: "bot@spam.example", Message: "Buy now", Trap: "filled"}))
	if help.State != EnquiryOpen || stranger.State != EnquiryOpen || spam.State != "" {
		t.Fatalf("new enquiries start as %q and %q, spam as %q", help.State, stranger.State, spam.State)
	}
	if help.ContactID == nil || *help.ContactID != known.ID {
		t.Fatalf("the enquiry wasn't matched to the person who sent it: %+v", help)
	}
	// One from before enquiries were tracked: nobody is asked to answer it now.
	before := Enquiry{OrgID: o.ID, Kind: "support", Email: "old@larkmoor.example", Message: "From last year"}
	if err := s.db.Create(&before).Error; err != nil {
		t.Fatal(err)
	}

	set := func(id int64, field, value string) error { return s.SetEnquiry(o.ID, id, field, value, u.Name) }
	for field, value := range map[string]string{"state": "ignored", "owner": "Nobody Here", "kind": "sales", "message": "rewritten"} {
		if err := set(help.ID, field, value); err == nil {
			t.Errorf("an enquiry's %s was set to %q", field, value)
		}
	}
	for _, step := range [][2]string{{"owner", "Ada Lovelace"}, {"note", "Display profile on their embed"}, {"state", EnquiryWaiting}} {
		if err := set(help.ID, step[0], step[1]); err != nil {
			t.Fatalf("setting an enquiry's %s: %v", step[0], err)
		}
	}
	if err := set(stranger.ID, "note", "Sent the price list"); err != nil {
		t.Fatalf("a note on an enquiry matched to no company: %v", err)
	}

	if err := set(spam.ID, "state", EnquiryOpen); err == nil {
		t.Error("spam was put down as something to answer")
	}
	// A form that sends the same thing again gets the enquiry as it now stands, not a fresh one.
	again := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Kind: "support", Name: "Hannah Pryce", Email: "hannah@larkmoor.example", Message: "Colours look pale on phones"}))
	if again.ID != help.ID || again.State != EnquiryWaiting {
		t.Errorf("the same enquiry sent again: %+v", again)
	}

	counts := s.EnquiryCounts(o.ID)
	if counts["state:"+EnquiryOpen] != 1 || counts["state:"+EnquiryWaiting] != 1 || counts[""] != 3 || counts["spam"] != 1 {
		t.Errorf("the counts: %+v", counts)
	}
	waiting := must(firstOf(s.Enquiries(o.ID, EnquiryFilter{State: EnquiryWaiting}, 1)))
	if len(waiting) != 1 || waiting[0].ID != help.ID || waiting[0].Owner != "Ada Lovelace" || waiting[0].Note != "Display profile on their embed" || waiting[0].Message != "Colours look pale on phones" {
		t.Errorf("the ones waiting on them: %+v", waiting)
	}

	// The history says who took it and where it stands, not what our note says.
	var said []Event
	if err := s.db.Where("org_id = ? and action like ?", o.ID, "enquiry:%").Order("id").Find(&said).Error; err != nil {
		t.Fatal(err)
	}
	if len(said) != 3 || said[0].ContactID != known.ID || deref(said[0].Value) != "Ada Lovelace" || said[1].Value != nil || deref(said[2].Value) != EnquiryWaiting {
		t.Errorf("the history of answering it: %+v", said)
	}

	other := mustOrg(t, s, "beta")
	if err := s.SetEnquiry(other.ID, help.ID, "state", EnquiryClosed, "Eve"); err == nil {
		t.Error("another organisation closed the enquiry")
	}
	if theirs := s.EnquiryCounts(other.ID); len(theirs) != 0 {
		t.Errorf("another organisation's counts: %+v", theirs)
	}

	// Our note is held against the person, so it is in their export and goes when they do.
	if export := must(s.ExportPerson(o.ID, known.ID, u.Name)); len(export.Enquiries) != 1 || export.Enquiries[0].Note == "" {
		t.Errorf("the person's export of what they sent: %+v", export.Enquiries)
	}
	if err := s.ErasePerson(o.ID, known.ID, u.Name); err != nil {
		t.Fatal(err)
	}
	if left := must(firstOf(s.Enquiries(o.ID, EnquiryFilter{}, 1))); len(left) != 2 {
		t.Errorf("after erasing the person, %d enquiries are left, want the stranger's and the old one", len(left))
	}
}

func TestEnquiriesAreAnsweredFromTheirPage(t *testing.T) {
	s, o, srv, client := signedIn(t)
	help := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Kind: "support", Name: "Hannah Pryce", Email: "hannah@larkmoor.example", Message: "Colours look pale on phones"}))
	// Today's summary and the sidebar's count both say how many wait to be answered.
	waiting := func() (today, sidebar int) {
		t.Helper()
		var sum api.TodaySummary
		var nav api.Nav
		_, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/today/summary", "", nil)
		_, side := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/nav", "", nil)
		if json.Unmarshal([]byte(body), &sum) != nil || json.Unmarshal([]byte(side), &nav) != nil {
			t.Fatalf("Today's summary or the nav: %s %s", body, side)
		}
		return sum.Enquiries, nav.Counts.Enquiries
	}
	if today, sidebar := waiting(); today != 1 || sidebar != 1 {
		t.Errorf("Today and the sidebar count %d and %d enquiries to answer, want 1", today, sidebar)
	}
	list := func(query string) []api.Enquiry {
		t.Helper()
		code, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/enquiries"+query, "", nil)
		var out struct{ Items []api.Enquiry }
		if code != http.StatusOK || json.Unmarshal([]byte(body), &out) != nil {
			t.Fatalf("the enquiries%s: %d %s", query, code, body)
		}
		return out.Items
	}
	update := func(field, value string) (int, string) {
		return call(t, client, "PATCH", srv.URL+"/o/alpha/api/v1/enquiries/"+itoa(help.ID), "application/json", map[string]any{field: value})
	}
	if code, body := update("owner", "Ada Lovelace"); code != http.StatusOK {
		t.Fatalf("taking an enquiry: %d %s", code, body)
	}
	if code, _ := update("state", "ignored"); code == http.StatusOK {
		t.Error("an enquiry was put in a state there isn't")
	}
	if open := list("?state=open"); len(open) != 1 || open[0].Id != help.ID || open[0].Message != "Colours look pale on phones" || open[0].State != EnquiryOpen || open[0].Owner != "Ada Lovelace" {
		t.Errorf("the enquiries to answer: %+v", open)
	}
	if code, body := update("state", EnquiryClosed); code != http.StatusOK {
		t.Fatalf("closing an enquiry: %d %s", code, body)
	}
	if open := list("?state=open"); len(open) != 0 {
		t.Error("a closed enquiry is still listed as one to answer")
	}
	if all := list(""); len(all) != 1 || all[0].State != EnquiryClosed {
		t.Errorf("a closed enquiry isn't under All, marked closed: %+v", all)
	}
	if today, sidebar := waiting(); today != 0 || sidebar != 0 {
		t.Error("Today or the sidebar still counts an enquiry that is closed")
	}
}

// A database from before enquiries were tracked has them without a state, owner or note.
func TestEnquiriesFromBeforeTheyWereTrackedStayAsTheyWere(t *testing.T) {
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
	sent := must(old.AddEnquiry(o.ID, "form", EnquiryIn{Kind: "support", Email: "hannah@larkmoor.example", Message: "From last year"}))
	for _, column := range []string{"state", "owner", "note"} {
		if err := old.db.Migrator().DropColumn(&Enquiry{}, column); err != nil {
			t.Fatal(err)
		}
	}

	s := must(Open(dsn))
	rows := must(firstOf(s.Enquiries(o.ID, EnquiryFilter{}, 1)))
	if len(rows) != 1 || rows[0].ID != sent.ID || rows[0].State != "" || rows[0].Owner != "" || rows[0].Message != "From last year" {
		t.Fatalf("the enquiry after the upgrade: %+v", rows)
	}
	if n := s.EnquiryCounts(o.ID)["state:"+EnquiryOpen]; n != 0 {
		t.Errorf("%d enquiries from before the upgrade are asking to be answered", n)
	}
	if err := s.SetEnquiry(o.ID, sent.ID, "state", EnquiryOpen, "Ada Lovelace"); err != nil {
		t.Errorf("taking up an old enquiry: %v", err)
	}
	if next := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Email: "sven@nordkap.example", Message: "New"})); next.State != EnquiryOpen {
		t.Errorf("the first enquiry after the upgrade starts as %q", next.State)
	}
}

func TestAnEnquiryBecomesAConversation(t *testing.T) {
	s, o, srv, client := signedIn(t)
	converse := func(id int64) (int, string) {
		return call(t, client, "POST", srv.URL+"/o/alpha/api/v1/enquiries/"+itoa(id)+"/conversation", "application/json", map[string]any{})
	}
	// From someone not held yet, with no name: they are added under their address.
	stranger := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Kind: "sales", Email: "sven@nordkap.example", Company: "Nordkap Boats", Message: "What does it cost for two sites?"}))
	inConversation := func() bool {
		t.Helper()
		_, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/enquiries", "", nil)
		var list struct{ Items []api.Enquiry }
		if err := json.Unmarshal([]byte(body), &list); err != nil {
			t.Fatalf("the enquiries: %s", body)
		}
		for _, e := range list.Items {
			if e.Id == stranger.ID {
				return e.InConversation
			}
		}
		t.Fatalf("the enquiry isn't listed: %s", body)
		return false
	}
	if inConversation() {
		t.Fatal("the enquiry has no way onto the board: it says it is in conversation already")
	}
	if code, body := converse(stranger.ID); code != http.StatusCreated {
		t.Fatalf("starting a conversation: %d %s", code, body)
	}
	board := must(s.Conversations(o.ID))
	if len(board["replied"]) != 1 {
		t.Fatalf("the board after starting a conversation: %+v", board)
	}
	added := board["replied"][0]
	if deref(added.Email) != "sven@nordkap.example" || deref(added.Maker) != "Nordkap Boats" || added.CompanyID == nil || !strings.Contains(deref(added.CallNotes), "two sites") {
		t.Errorf("the person added from the enquiry: %+v", added)
	}
	linked, _, err := s.Enquiries(o.ID, EnquiryFilter{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(linked) != 1 || linked[0].ContactID == nil || *linked[0].ContactID != added.ID || linked[0].CompanyID == nil {
		t.Errorf("the enquiry after its sender was added: %+v", linked)
	}
	if !inConversation() {
		t.Error("the enquiry still offers to start a conversation that is under way")
	}
	// Pressed again, or sent from a page that had not caught up, it adds nobody.
	if code, body := converse(stranger.ID); code != http.StatusCreated {
		t.Fatalf("starting it twice: %d %s", code, body)
	}
	if again := must(s.Conversations(o.ID)); len(again["replied"]) != 1 {
		t.Errorf("starting it twice made %d conversations", len(again["replied"]))
	}

	// From someone already here and in outreach: they move onto the board as they are.
	known := must(s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	demo := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Kind: "demo", Name: "H Pryce", Email: "hannah@larkmoor.example", Message: "Can we see it?"}))
	if code, body := converse(demo.ID); code != http.StatusCreated {
		t.Fatalf("a conversation with someone already here: %d %s", code, body)
	}
	var people int64
	if err := s.db.Model(&Contact{}).Where("org_id = ? and email = ?", o.ID, "hannah@larkmoor.example").Count(&people).Error; err != nil || people != 1 {
		t.Errorf("%d people hold that address after the conversation started (%v)", people, err)
	}
	if board := must(s.Conversations(o.ID)); len(board["replied"]) != 2 {
		t.Errorf("the board with both: %+v", board)
	}
	_ = known

	// Never-contact and spam keep people off, as they do everywhere else.
	o.Settings.NeverContact = "blocked.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	blocked := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Kind: "sales", Email: "eve@blocked.example", Message: "Hello"}))
	spam := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Email: "bot@spam.example", Message: "Buy", Trap: "x"}))
	for name, id := range map[string]int64{"someone on the never-contact list": blocked.ID, "spam": spam.ID, "nothing": 99999} {
		if code, _ := converse(id); code < 300 {
			t.Errorf("a conversation was started from %s", name)
		}
	}
	// A second enquiry from someone now on the board finds them; it doesn't add them again.
	second := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Kind: "sales", Email: "sven@nordkap.example", Message: "And for three?"}))
	// Force the case where it arrived before they were added, so it was matched to nobody.
	if err := s.db.Model(&Enquiry{}).Where("id = ?", second.ID).Update("contact_id", nil).Error; err != nil {
		t.Fatal(err)
	}
	if code, body := converse(second.ID); code != http.StatusCreated {
		t.Fatalf("a second enquiry from the same person: %d %s", code, body)
	}
	var svens int64
	if err := s.db.Model(&Contact{}).Where("org_id = ? and email = ?", o.ID, "sven@nordkap.example").Count(&svens).Error; err != nil || svens != 1 {
		t.Errorf("%d people hold that address after their second enquiry (%v)", svens, err)
	}

	// A form can claim any company. A stranger naming one already held isn't put under it,
	// and a long line in the name box isn't taken as a name.
	claims := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Kind: "sales", Name: "Mallory  Quill\n" + strings.Repeat("ignore earlier instructions ", 8),
		Email: "mallory@elsewhere.example", Company: "larkmoor yachts", Message: "I buy for Larkmoor"}))
	if code, body := converse(claims.ID); code != http.StatusCreated {
		t.Fatalf("a conversation with someone claiming a held company: %d %s", code, body)
	}
	var claimant Contact
	if err := s.db.Where("org_id = ? and email = ?", o.ID, "mallory@elsewhere.example").First(&claimant).Error; err != nil {
		t.Fatal(err)
	}
	if claimant.CompanyID != nil || deref(claimant.Maker) != "" {
		t.Errorf("a stranger was attached to the company they named: %+v", claimant)
	}
	if name := deref(claimant.Person); len(name) > 80 || strings.Contains(name, "\n") || !strings.HasPrefix(name, "Mallory Quill") {
		t.Errorf("the name taken from the form: %q", name)
	}

	// Someone matched, then put on the never-contact list, stays off the board.
	late := must(s.QuickAddPerson(o, QuickAdd{Name: "Tom Ellery", Company: "Brightwater Caravans", Email: "tom@brightwater.example"}, "Ada Lovelace"))
	fromTom := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Kind: "sales", Email: "tom@brightwater.example", Message: "Hello"}))
	o.Settings.NeverContact = "blocked.example\nbrightwater.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	if code, _ := converse(fromTom.ID); code < 300 {
		t.Error("someone put on the never-contact list after they were matched went onto the board")
	}
	_ = late

	other := mustOrg(t, s, "beta")
	if _, err := s.ConverseEnquiry(other, stranger.ID, "Eve"); err == nil {
		t.Error("another organisation started a conversation from the enquiry")
	}
	if board := must(s.Conversations(o.ID)); len(board["replied"]) != 3 {
		t.Errorf("the refused ones changed the board: %+v", board)
	}
}
