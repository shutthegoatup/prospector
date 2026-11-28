package main

import (
	"errors"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/research"
)

const hannahEmail = "hannah.pryce@larkmoor.example"

// navOwner is whether the app offers what only an owner may do to whoever client is.
func navOwner(t *testing.T, client *http.Client, site string) bool {
	t.Helper()
	code, body := call(t, client, "GET", site+"/o/alpha/api/v1/nav", "", nil)
	if code != http.StatusOK {
		t.Fatalf("the nav: %d %s", code, body)
	}
	return strings.Contains(body, `"owner":true`)
}

func TestErasePerson(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	other := mustOrg(t, s, "beta")
	do := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, org := range []*Org{o, other} {
		if _, err := s.Import(org.ID, readFixtures(t)); err != nil {
			t.Fatal(err)
		}
	}
	hannah := must(s.FindOne(o.ID, "Hannah"))
	before := must(s.SnapshotText(o.ID))

	do(s.Update(o.ID, hannah.ID, "linkedin", "https://www.linkedin.com/in/hannah-pryce-4a2b1c/", "ada"))
	do(s.Update(o.ID, hannah.ID, "phone", "+44 20 7946 0000", "ada"))
	do(s.Update(o.ID, hannah.ID, "call_notes", "prefers mornings, has a dog called Biscuit", "ada"))
	do(s.AgentMark(o.ID, Actor{Name: "agent:scout"}, research.MarkIn{ContactID: hannah.ID, Step: "replied", Date: "2020-01-02", Note: "she wrote back from her own gmail"}))
	do(s.Update(o.ID, hannah.ID, "stage", "meeting", "ada"))
	do(s.DB.Create(&Fact{OrgID: o.ID, CompanyID: *hannah.CompanyID, ContactID: &hannah.ID, Field: "role", Value: "Head of Marketing", SourceURL: "https://larkmoor.example/team"}).Error)
	var placeholder Contact
	do(s.InOrg(o.ID).Where("key like 'org:%'").First(&placeholder).Error)

	jar, _ := cookiejar.New(nil)
	bob := &http.Client{Jar: jar}
	u := must(s.CreateUser("bob@example.com", "Bob", "correct horse battery"))
	do(s.AddMember(o.ID, u.ID, "member", u.Name))
	res, err := signInWith(bob, srv.URL, "bob@example.com", "correct horse battery")
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("login: %v", err)
	}
	res.Body.Close()
	erase := func(client *http.Client, id int64) int {
		code, _ := call(t, client, "POST", srv.URL+"/o/alpha/api/v1/contacts/"+strconv.FormatInt(id, 10)+"/erase", "application/json", struct{}{})
		return code
	}
	if code := erase(bob, hannah.ID); code != http.StatusForbidden {
		t.Fatalf("a member who isn't an owner erased someone: %d", code)
	}
	if code := erase(ada, placeholder.ID); code < 300 {
		t.Fatal("a company's placeholder row was erased as if it were a person")
	}
	// The company page offers erasing to whoever the nav says is an owner.
	if !navOwner(t, ada, srv.URL) || navOwner(t, bob, srv.URL) {
		t.Fatal("the nav should offer erasing to an owner alone")
	}
	if code, _ := call(t, bob, "POST", srv.URL+"/o/alpha/api/v1/contacts/"+strconv.FormatInt(hannah.ID, 10)+"/erase", "application/json", struct{}{}); code != http.StatusForbidden {
		t.Fatalf("a member who isn't an owner erased someone through the API: %d", code)
	}
	if code := erase(ada, hannah.ID); code != http.StatusNoContent {
		t.Fatalf("erase: %d", code)
	}

	gone := func(when string) {
		t.Helper()
		var n int64
		s.InOrg(o.ID).Model(&Contact{}).Where("id = ? or lower(email) = ? or person like 'Hannah%'", hannah.ID, hannahEmail).Count(&n)
		if n != 0 {
			t.Fatalf("%s: Hannah is in the contacts", when)
		}
	}
	gone("after erasing")
	var facts, attached int64
	s.InOrg(o.ID).Model(&Fact{}).Where("contact_id = ?", hannah.ID).Count(&facts)
	s.InOrg(o.ID).Model(&Event{}).Where("contact_id = ?", hannah.ID).Count(&attached)
	if facts != 0 || attached != 0 {
		t.Fatalf("%d facts and %d events still point at her", facts, attached)
	}
	var events []Event
	do(s.InOrg(o.ID).Find(&events).Error)
	kept := map[string]string{}
	for _, e := range events {
		v := strings.ToLower(deref(e.Value))
		for _, secret := range []string{"hannah", "7946", "biscuit", "gmail", "larkmoor.example"} {
			if strings.Contains(v, secret) {
				t.Errorf("the history still says %q under %s", deref(e.Value), e.Action)
			}
		}
		kept[e.Action] = deref(e.Value)
	}
	if kept["mark:replied"] != "2020-01-02" || kept["set:stage"] != "meeting" {
		t.Fatalf("the history lost what the counts are built from: %v", kept)
	}
	if _, ok := kept["erase"]; !ok {
		t.Fatal("nothing records that somebody was erased")
	}
	company := srv.URL + "/o/alpha/api/v1/companies/" + strconv.FormatInt(*hannah.CompanyID, 10)
	var page string
	for _, part := range []string{"/overview", "/history", "/notes"} {
		_, body := call(t, ada, "GET", company+part, "", nil)
		page += body
	}
	if strings.Contains(page, "Hannah") || !strings.Contains(page, "erased a person on request") {
		t.Fatalf("the company page still names her, or doesn't say someone was erased: %s", page)
	}

	if _, err := s.Import(o.ID, readFixtures(t)); err == nil || !strings.Contains(err.Error(), "erased on request") {
		t.Fatalf("an import that still holds her went through: %v", err)
	}
	gone("after re-importing her file")
	if _, err := s.QuickAddPerson(o, crm.QuickAdd{Name: "H Pryce", Email: " Hannah.Pryce@Larkmoor.example "}, "ada"); !errors.Is(err, errErased) {
		t.Fatalf("she was added again by email: %v", err)
	}
	if _, err := s.AgentAddContact(o, Actor{Name: "agent:scout"}, prospecting.AddContactIn{Company: strconv.FormatInt(*hannah.CompanyID, 10), Name: "H Pryce",
		Linkedin: "https://uk.linkedin.com/in/hannah-pryce-4a2b1c?trk=x", SourceURL: "https://larkmoor.example/team"}); !errors.Is(err, errErased) {
		t.Fatalf("an agent suggested her again by profile: %v", err)
	}
	for _, link := range []string{"https://www.linkedin.com/in/Hannah-Pryce-4a2b1c/details/experience/", "linkedin.com/in/hannah-pryce-4a2b1c/en"} {
		if _, err := s.QuickAddPerson(o, crm.QuickAdd{Name: "H Pryce", Linkedin: "https://" + strings.TrimPrefix(link, "https://")}, "ada"); !errors.Is(err, errErased) {
			t.Fatalf("she was added again by a page under her profile, %s: %v", link, err)
		}
	}
	if _, err := s.QuickAddPerson(o, crm.QuickAdd{Name: "H Pryce", Email: "mailto:" + hannahEmail}, "ada"); !errors.Is(err, errErased) {
		t.Fatalf("she was added again by a mailto link: %v", err)
	}
	if _, err := s.AgentAddContact(o, Actor{Name: "agent:scout"}, prospecting.AddContactIn{Company: strconv.FormatInt(*hannah.CompanyID, 10), Name: "hannah pryce",
		Title: "Head of Marketing", SourceURL: "https://larkmoor.example/team"}); !errors.Is(err, errErased) {
		t.Fatalf("an agent suggested her again by name at the same company: %v", err)
	}
	namesake := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Ostrel"}, "ada"))
	withNamesake := must(s.SnapshotText(o.ID))
	if n, err := s.Restore(o.ID, withNamesake); err != nil || n == 0 {
		t.Fatalf("restore: %d, %v", n, err)
	}
	var twin Contact
	if err := s.InOrg(o.ID).First(&twin, namesake.ID).Error; err != nil {
		t.Fatal("a restore dropped someone at another company who shares her name")
	}
	do(s.DB.Delete(&twin).Error)
	jonas := must(s.FindOne(o.ID, "Jonas"))
	if err := s.Update(o.ID, jonas.ID, "email", hannahEmail, "ada"); !errors.Is(err, errErased) {
		t.Fatalf("her address was put on someone else: %v", err)
	}
	n, err := s.Restore(o.ID, before)
	do(err)
	gone("after restoring a snapshot from before")
	if n == 0 || must(s.FindOne(o.ID, "Jonas")) == nil {
		t.Fatal("the restore dropped everyone, not just her")
	}

	fresh := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Nia Okafor", Company: "Larkmoor"}, "ada"))
	var inherited int64
	s.InOrg(o.ID).Model(&Event{}).Where("contact_id = ? and action <> ?", fresh.ID, "add:person").Count(&inherited)
	if inherited != 0 {
		t.Fatalf("the next person added inherited %d lines of history", inherited)
	}

	if _, err := s.FindOne(other.ID, "Hannah"); err != nil {
		t.Fatalf("erasing her in one organisation removed her from another: %v", err)
	}
	if _, err := s.Import(other.ID, readFixtures(t)); err != nil {
		t.Fatalf("another organisation can no longer import her: %v", err)
	}
}

func TestOnlyAProfileOrAddressIdentifiesSomeone(t *testing.T) {
	t.Parallel()
	if got := identifiers(1, "Hannah Pryce", "", "", "https://www.linkedin.com/search/results/people/?keywords=Larkmoor%20marketing"); len(got) != 0 {
		t.Fatal("a LinkedIn search link, or a name with no company, counts as a person")
	}
	a := identifiers(1, "", "", "A@x.example, b@x.example", "https://www.linkedin.com/in/someone/")
	if len(a) != 3 {
		t.Fatalf("two addresses and a profile gave %d identifiers", len(a))
	}
	if b := identifiers(2, "", "", "a@x.example", ""); b[0] == a[0] {
		t.Fatal("the same address hashes alike in two organisations")
	}
}
