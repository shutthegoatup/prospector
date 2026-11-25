package main

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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
	waiting := must(s.Enquiries(o.ID, EnquiryFilter{State: EnquiryWaiting}))
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
	if left := must(s.Enquiries(o.ID, EnquiryFilter{})); len(left) != 2 {
		t.Errorf("after erasing the person, %d enquiries are left, want the stranger's and the old one", len(left))
	}
}

func TestEnquiriesAreAnsweredFromTheirPage(t *testing.T) {
	s, o, srv, client := signedIn(t)
	help := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Kind: "support", Name: "Hannah Pryce", Email: "hannah@larkmoor.example", Message: "Colours look pale on phones"}))
	sidebar := regexp.MustCompile(`Enquiries</span>\s*<span[^>]*>(\d+)</span>`)
	_, today := call(t, client, "GET", srv.URL+"/o/alpha/today", "", nil)
	if !strings.Contains(today, "Answer enquiries") || !strings.Contains(today, "enquiries?state=open") {
		t.Error("Today says nothing of an enquiry nobody has answered")
	}
	if m := sidebar.FindStringSubmatch(today); m == nil || m[1] != "1" {
		t.Errorf("the sidebar's count of enquiries to answer: %v", m)
	}
	update := func(field, value string) (int, string) {
		return call(t, client, "POST", srv.URL+"/o/alpha/ui/enquiry/update", "application/json", map[string]any{"id": help.ID, "field": field, "value": value})
	}
	if code, body := update("owner", "Ada Lovelace"); code != http.StatusOK {
		t.Fatalf("taking an enquiry: %d %s", code, body)
	}
	if code, _ := update("state", "ignored"); code == http.StatusOK {
		t.Error("an enquiry was put in a state there isn't")
	}
	_, page := call(t, client, "GET", srv.URL+"/o/alpha/enquiries?state=open", "", nil)
	for _, want := range []string{"Colours look pale on phones", "To answer", `data-enquiry="` + itoa(help.ID) + `"`, "Ada Lovelace", "theirs to see if they ask"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page of enquiries to answer doesn't show %q", want)
		}
	}
	if code, body := update("state", EnquiryClosed); code != http.StatusOK {
		t.Fatalf("closing an enquiry: %d %s", code, body)
	}
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/enquiries?state=open", "", nil); strings.Contains(page, "Colours look pale") {
		t.Error("a closed enquiry is still listed as one to answer")
	}
	if _, page := call(t, client, "GET", srv.URL+"/o/alpha/enquiries", "", nil); !strings.Contains(page, "Colours look pale") || !strings.Contains(page, "Closed") {
		t.Error("a closed enquiry isn't under All, marked closed")
	}
	_, today = call(t, client, "GET", srv.URL+"/o/alpha/today", "", nil)
	if strings.Contains(today, "Answer enquiries") || sidebar.MatchString(today) {
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
	rows := must(s.Enquiries(o.ID, EnquiryFilter{}))
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
