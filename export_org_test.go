package main

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
)

func TestExportOrg(t *testing.T) {
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
	do(s.db.Model(&Org{}).Where("id = ?", other.ID).Update("name", "Betamax Holdings").Error)
	hannah := must(s.FindOne(o.ID, "Hannah"))
	do(s.Update(o.ID, hannah.ID, "phone", "+44 20 7946 0000", "ada"))
	do(s.db.Create(&Fact{OrgID: o.ID, CompanyID: *hannah.CompanyID, ContactID: &hannah.ID, Field: "role", Value: "Head of Marketing", SourceURL: "https://larkmoor.example/team"}).Error)
	do(s.db.Create(&[]Enquiry{
		{OrgID: o.ID, Name: "Hannah Pryce", Email: hannahEmail, Message: "Could you send pricing?"},
		{OrgID: other.ID, Name: "Hannah Pryce", Email: hannahEmail, Message: "A note to the other organisation"},
	}).Error)
	jonas := must(s.FindOne(o.ID, "Jonas"))
	do(s.ErasePerson(o.ID, jonas.ID, "ada"))
	key, _, err := s.CreateKey(o.ID, "scout", []string{"read", "research:write", "outcomes:write", "import"}, "ada")
	do(err)
	theirKey, _, err := s.CreateKey(other.ID, "their-bot", []string{"read"}, "test")
	do(err)
	outsider := must(s.CreateUser("carol@example.com", "Carol Outsider", "correct horse battery"))
	do(s.AddMember(other.ID, outsider.ID, "owner", outsider.Name))
	do(s.SaveSSO(&SSOConnection{OrgID: o.ID, Issuer: "https://id.larkmoor.example", ClientID: "prospector-client", ClientSecret: "s3cret-client-value"}, "ada"))
	do(s.db.Create(&SSODomain{OrgID: o.ID, Domain: "alpha.example", Token: "dns-proof-token"}).Error)
	do(s.SaveOrgMail(&OrgMail{OrgID: o.ID, Host: "smtp.alpha.example", Port: 587, Username: "mailer", Password: "sealed-mail-password", Sender: "Alpha <hello@alpha.example>"}))
	do(s.SaveOrgMail(&OrgMail{OrgID: other.ID, Host: "smtp.beta.example", Port: 587, Sender: "beta@beta.example"}))

	jar, _ := cookiejar.New(nil)
	bob := &http.Client{Jar: jar}
	u := must(s.CreateUser("bob@example.com", "Bob", "correct horse battery"))
	do(s.AddMember(o.ID, u.ID, "member", u.Name))
	res, err := bob.PostForm(srv.URL+"/login", url.Values{"email": {"bob@example.com"}, "password": {"correct horse battery"}})
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("login: %v", err)
	}
	res.Body.Close()
	agent := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		r.Header.Set("Authorization", "Bearer "+key)
		return http.DefaultTransport.RoundTrip(r)
	})}
	export := func(client *http.Client) (int, string) {
		return call(t, client, "POST", srv.URL+"/o/alpha/ui/export-org", "application/json", struct{}{})
	}
	if code, _ := export(bob); code != http.StatusForbidden {
		t.Fatalf("a member who isn't an owner exported the organisation: %d", code)
	}
	if code, _ := export(agent); code == http.StatusOK {
		t.Fatal("an agent's key exported the organisation")
	}
	if code, _ := call(t, ada, "GET", srv.URL+"/o/alpha/ui/export-org", "", nil); code == http.StatusOK {
		t.Fatal("following a link exported the organisation")
	}
	if _, page := call(t, bob, "GET", srv.URL+"/o/alpha/settings", "", nil); strings.Contains(page, "ui/export-org") {
		t.Fatal("Settings offers the export to someone who isn't an owner")
	}
	if _, page := call(t, ada, "GET", srv.URL+"/o/alpha/settings", "", nil); !strings.Contains(page, `data-download="ui/export-org"`) {
		t.Fatal("Settings gives an owner no way to export the organisation")
	}

	code, body := export(ada)
	if code != http.StatusOK {
		t.Fatalf("export: %d %s", code, body)
	}
	var out OrgExport
	do(json.Unmarshal([]byte(body), &out))
	if out.Organisation.Slug != "alpha" || len(out.Members) != 2 || out.Members[1].Email != "bob@example.com" || out.Members[1].Role != "member" {
		t.Fatalf("organisation and members: %+v %+v", out.Organisation, out.Members)
	}
	var contacts, companies, events, facts int64
	s.in(o.ID).Model(&Contact{}).Count(&contacts)
	s.in(o.ID).Model(&Fact{}).Count(&facts)
	s.in(o.ID).Model(&Company{}).Count(&companies)
	s.in(o.ID).Model(&Event{}).Count(&events)
	if contacts == 0 || int64(len(out.Contacts)) != contacts || int64(len(out.Companies)) != companies || int64(len(out.History)) != events {
		t.Fatalf("%d of %d contacts, %d of %d companies, %d of %d history lines", len(out.Contacts), contacts, len(out.Companies), companies, len(out.History), events)
	}
	if facts < 2 || int64(len(out.Facts)) != facts || len(out.Enquiries) != 1 || out.Erased != 1 {
		t.Fatalf("%d facts, %d enquiries, %d erased", len(out.Facts), len(out.Enquiries), out.Erased)
	}
	if len(out.Keys) != 1 || out.Keys[0].Name != "scout" || out.Keys[0].CreatedBy != "ada" {
		t.Fatalf("keys: %+v", out.Keys)
	}
	if out.SignOn == nil || out.SignOn.Issuer != "https://id.larkmoor.example" || out.SignOn.ClientID != "prospector-client" || len(out.SignOn.Domains) != 1 {
		t.Fatalf("sign-on: %+v", out.SignOn)
	}
	if out.Mail == nil || out.Mail.Host != "smtp.alpha.example" || out.Mail.Username != "mailer" || strings.Contains(body, "smtp.beta.example") {
		t.Fatalf("mail server: %+v", out.Mail)
	}
	if !strings.Contains(body, "+44 20 7946 0000") || !strings.Contains(body, "Could you send pricing?") {
		t.Fatal("the export is missing a person's details or what they wrote")
	}

	var keys []APIKey
	do(s.db.Find(&keys).Error)
	var sessions []Session
	do(s.db.Find(&sessions).Error)
	var conn SSOConnection
	do(s.db.Where("org_id = ?", o.ID).First(&conn).Error)
	var users []User
	do(s.db.Find(&users).Error)
	var erasures []Erasure
	do(s.in(o.ID).Find(&erasures).Error)
	secrets := []string{key, theirKey, "s3cret-client-value", conn.ClientSecret, "dns-proof-token", "correct horse battery", "sealed-mail-password"}
	for _, k := range keys {
		secrets = append(secrets, k.Hash, k.Prefix)
	}
	for _, sess := range sessions {
		secrets = append(secrets, sess.TokenHash)
	}
	for _, u := range users {
		secrets = append(secrets, u.PasswordHash)
	}
	for _, e := range erasures {
		secrets = append(secrets, e.Hash)
	}
	for _, secret := range secrets {
		if secret == "" {
			t.Fatal("the test holds an empty secret, which would match anything")
		}
		if strings.Contains(body, secret) {
			t.Errorf("the export holds a secret: %.12s…", secret)
		}
	}
	for _, theirs := range []string{"Betamax", "their-bot", "carol", "Carol", "other organisation", "org_id", "OrgID"} {
		if strings.Contains(body, theirs) {
			t.Errorf("the export holds %q, which belongs to another organisation or to nobody outside", theirs)
		}
	}

	var noted []Event
	do(s.in(o.ID).Where("action = ?", "export:org").Find(&noted).Error)
	if len(noted) != 1 || noted[0].User == "" || noted[0].Value != nil {
		t.Fatalf("one export should be on the record, by name: %+v", noted)
	}
}
