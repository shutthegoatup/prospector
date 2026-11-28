package main

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/internal/privacy"
)

func TestExportPerson(t *testing.T) {
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
	colleague := must(s.QuickAddPerson(o, QuickAdd{Name: "Nia Okafor", Company: deref(hannah.Maker), Email: "nia.okafor@larkmoor.example"}, "ada"))
	do(s.Update(o.ID, colleague.ID, "call_notes", "asked for a callback in spring", "ada"))
	do(s.Update(o.ID, hannah.ID, "phone", "+44 20 7946 0000", "ada"))
	do(s.Update(o.ID, hannah.ID, "call_notes", "prefers mornings, has a dog called Biscuit", "ada"))
	do(s.AgentMark(o.ID, Actor{Name: "agent:scout"}, MarkIn{ContactID: hannah.ID, Step: "replied", Date: "2020-01-02", Note: "she wrote back from her own gmail"}))
	do(s.Update(o.ID, hannah.ID, "stage", "meeting", "ada"))
	do(s.DB.Create(&Fact{OrgID: o.ID, CompanyID: *hannah.CompanyID, ContactID: &hannah.ID, Field: "role", Value: "Head of Marketing", SourceURL: "https://larkmoor.example/team"}).Error)
	do(s.DB.Create(&[]Enquiry{
		{OrgID: o.ID, Name: "Hannah Pryce", Email: hannahEmail, Message: "Could you send pricing?", ContactID: &hannah.ID},
		{OrgID: o.ID, Name: "H. Pryce", Email: hannahEmail, Message: "Following up on pricing"},
		{OrgID: o.ID, Name: "Nia Okafor", Email: "nia.okafor@larkmoor.example", Message: "Please ring the front desk", ContactID: &colleague.ID},
		{OrgID: other.ID, Name: "Hannah Pryce", Email: hannahEmail, Message: "A note to the other organisation"},
	}).Error)
	var placeholder Contact
	do(s.InOrg(o.ID).Where("key like 'org:%'").First(&placeholder).Error)
	theirs := must(s.FindOne(other.ID, "Hannah"))

	jar, _ := cookiejar.New(nil)
	bob := &http.Client{Jar: jar}
	u := must(s.CreateUser("bob@example.com", "Bob", "correct horse battery"))
	do(s.AddMember(o.ID, u.ID, "member", u.Name))
	res, err := signInWith(bob, srv.URL, "bob@example.com", "correct horse battery")
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("login: %v", err)
	}
	res.Body.Close()

	companyHistory := srv.URL + "/o/alpha/api/v1/companies/" + strconv.FormatInt(*hannah.CompanyID, 10) + "/history"
	export := func(client *http.Client, id int64) (int, string) {
		return call(t, client, "POST", srv.URL+"/o/alpha/api/v1/contacts/"+strconv.FormatInt(id, 10)+"/export", "application/json", struct{}{})
	}
	if code, _ := export(bob, hannah.ID); code != http.StatusForbidden {
		t.Fatalf("a member who isn't an owner exported someone: %d", code)
	}
	key, _, err := s.CreateKey(o.ID, "scout", []string{"read", "research:write", "outcomes:write", "import"}, "test")
	do(err)
	agent := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		r.Header.Set("Authorization", "Bearer "+key)
		return http.DefaultTransport.RoundTrip(r)
	})}
	if code, _ := export(agent, hannah.ID); code == http.StatusOK {
		t.Fatal("an agent's key exported someone")
	}
	// A link on another site can send an owner's browser anywhere a GET reaches.
	if code, _ := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/contacts/"+strconv.FormatInt(hannah.ID, 10)+"/export", "", nil); code == http.StatusOK {
		t.Fatal("following a link exported someone")
	}
	for what, id := range map[string]int64{"a company's placeholder row": placeholder.ID, "someone in another organisation": theirs.ID, "nobody": 999999} {
		if code, body := export(ada, id); code == http.StatusOK {
			t.Fatalf("exported %s: %s", what, body)
		}
	}
	var early int64
	s.InOrg(o.ID).Model(&Event{}).Where("action = ?", "export").Count(&early)
	if early != 0 {
		t.Fatalf("%d refused exports were written into the history", early)
	}
	// The company page offers the export to whoever the nav says is an owner.
	if !navOwner(t, ada, srv.URL) || navOwner(t, bob, srv.URL) {
		t.Fatal("the nav should offer the export to an owner alone")
	}
	if code, _ := call(t, bob, "POST", srv.URL+"/o/alpha/api/v1/contacts/"+strconv.FormatInt(hannah.ID, 10)+"/export", "application/json", struct{}{}); code != http.StatusForbidden {
		t.Fatalf("a member who isn't an owner exported someone through the API: %d", code)
	}

	code, body := export(ada, hannah.ID)
	if code != http.StatusOK {
		t.Fatalf("export: %d %s", code, body)
	}
	var out privacy.PersonExport
	do(json.Unmarshal([]byte(body), &out))
	if deref(out.Person.Email) != hannahEmail || deref(out.Person.Phone) != "+44 20 7946 0000" || out.Person.Stage != "meeting" ||
		!strings.Contains(deref(out.Person.CallNotes), "Biscuit") || deref(out.Person.Note) == "" {
		t.Fatalf("the export is missing her details: %+v", out.Person)
	}
	if out.Company == "" || out.Note != "" {
		t.Fatalf("company %q, note %q", out.Company, out.Note)
	}
	if len(out.Facts) != 1 || out.Facts[0].Value != "Head of Marketing" {
		t.Fatalf("facts: %+v", out.Facts)
	}
	if len(out.Enquiries) != 2 || out.Enquiries[0].Message != "Could you send pricing?" || out.Enquiries[1].Message != "Following up on pricing" {
		t.Fatalf("the export should hold what she wrote to us, matched to her or not: %+v", out.Enquiries)
	}
	said := map[string]privacy.ExportEvent{}
	for _, e := range out.History {
		said[e.Action] = e
	}
	if e := said["mark:replied"]; !strings.Contains(e.Value, "gmail") || e.By != "agent:scout" || e.At.IsZero() {
		t.Fatalf("the history lost the agent's mark: %+v", out.History)
	}
	if said["set:stage"].Value != "meeting" || said["set:call_notes"].By != "ada" {
		t.Fatalf("the history lost her edits: %+v", out.History)
	}
	whole := string(must(json.Marshal(out)))
	for _, notHers := range []string{"Nia", "nia.okafor", "callback in spring", "front desk", "other organisation", `"OrgID"`, "org_id"} {
		if strings.Contains(whole, notHers) {
			t.Errorf("the export holds %q, which isn't hers to see", notHers)
		}
	}

	var noted []Event
	do(s.InOrg(o.ID).Where("action = ?", "export").Find(&noted).Error)
	if len(noted) != 1 || noted[0].ContactID != hannah.ID || noted[0].Value != nil || noted[0].User == "" {
		t.Fatalf("the history should note one export of her, by name and with no details: %+v", noted)
	}
	if _, history := call(t, ada, "GET", companyHistory, "", nil); !strings.Contains(history, "exported what is held on them") {
		t.Fatalf("the company's history doesn't show that she was exported: %s", history)
	}

	row := Contact{OrgID: o.ID, Key: "pair", File: "test", Person: str("Jonas Weber, CMO; Ines Duarte, Head of Digital")}
	do(s.DB.Create(&row).Error)
	both := must(s.ExportPerson(o.ID, row.ID, "ada"))
	if both.Note == "" {
		t.Fatal("a row naming two people was exported as if it were one person's")
	}
}
