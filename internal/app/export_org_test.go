package app

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/internal/privacy"
	"github.com/shutthegoatup/prospector/internal/store"
)

func TestExportOrg(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	other := mustOrg(t, s, "beta")
	do := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, org := range []*store.Org{o, other} {
		if _, err := s.Import(org.ID, readFixtures(t), "import"); err != nil {
			t.Fatal(err)
		}
	}
	do(s.DB.Model(&store.Org{}).Where("id = ?", other.ID).Update("name", "Betamax Holdings").Error)
	hannah := must(s.FindOne(o.ID, "Hannah"))
	do(s.Update(o.ID, hannah.ID, "phone", "+44 20 7946 0000", "ada"))
	do(s.DB.Create(&store.Fact{OrgID: o.ID, CompanyID: *hannah.CompanyID, ContactID: &hannah.ID, Field: "role", Value: "Head of Marketing", SourceURL: "https://larkmoor.example/team"}).Error)
	do(s.DB.Create(&[]store.Enquiry{
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
	do(s.AddMember(other.ID, outsider.ID, "owner", outsider.Name, "Owner One"))
	do(s.SaveSSO(&store.SSOConnection{OrgID: o.ID, Issuer: "https://id.larkmoor.example", ClientID: "prospector-client", ClientSecret: "s3cret-client-value"}, "ada"))
	do(s.DB.Create(&store.SSODomain{OrgID: o.ID, Domain: "alpha.example", Token: "dns-proof-token"}).Error)
	do(s.SaveOrgMail(o.ID, &store.OrgMail{OrgID: o.ID, Host: "smtp.alpha.example", Port: 587, Username: "mailer", Password: "sealed-mail-password", Sender: "Alpha <hello@alpha.example>"}, "Owner One"))
	do(s.SaveOrgMail(other.ID, &store.OrgMail{OrgID: other.ID, Host: "smtp.beta.example", Port: 587, Sender: "beta@beta.example"}, "Owner One"))

	jar, _ := cookiejar.New(nil)
	bob := &http.Client{Jar: jar}
	u := must(s.CreateUser("bob@example.com", "Bob", "correct horse battery"))
	do(s.AddMember(o.ID, u.ID, "member", u.Name, "Owner One"))
	res, err := signInWith(bob, srv.URL, "bob@example.com", "correct horse battery")
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("login: %v", err)
	}
	res.Body.Close()
	agent := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		r.Header.Set("Authorization", "Bearer "+key)
		return http.DefaultTransport.RoundTrip(r)
	})}
	export := func(client *http.Client) (int, string) {
		return call(t, client, "POST", srv.URL+"/o/alpha/api/v1/export", "application/json", struct{}{})
	}
	if code, _ := export(bob); code != http.StatusForbidden {
		t.Fatalf("a member who isn't an owner exported the organisation: %d", code)
	}
	if code, _ := export(agent); code == http.StatusOK {
		t.Fatal("an agent's key exported the organisation")
	}
	if code, _ := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/export", "", nil); code == http.StatusOK {
		t.Fatal("following a link exported the organisation")
	}
	// Settings offers the export to whoever the nav says is an owner.
	if !navOwner(t, ada, srv.URL) || navOwner(t, bob, srv.URL) {
		t.Fatal("the nav should offer the export to an owner alone")
	}
	if code, _ := call(t, bob, "POST", srv.URL+"/o/alpha/api/v1/export", "application/json", struct{}{}); code != http.StatusForbidden {
		t.Fatalf("a member who isn't an owner exported the organisation through the API: %d", code)
	}

	code, body := export(ada)
	if code != http.StatusOK {
		t.Fatalf("export: %d %s", code, body)
	}
	var out privacy.OrgExport
	do(json.Unmarshal([]byte(body), &out))
	if out.Organisation.Slug != "alpha" || len(out.Members) != 2 || out.Members[1].Email != "bob@example.com" || out.Members[1].Role != "member" {
		t.Fatalf("organisation and members: %+v %+v", out.Organisation, out.Members)
	}
	var contacts, companies, events, facts int64
	s.InOrg(o.ID).Model(&store.Contact{}).Count(&contacts)
	s.InOrg(o.ID).Model(&store.Fact{}).Count(&facts)
	s.InOrg(o.ID).Model(&store.Company{}).Count(&companies)
	s.InOrg(o.ID).Model(&store.Event{}).Count(&events)
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

	var keys []store.APIKey
	do(s.DB.Find(&keys).Error)
	var sessions []store.Session
	do(s.DB.Find(&sessions).Error)
	var conn store.SSOConnection
	do(s.DB.Where("org_id = ?", o.ID).First(&conn).Error)
	var users []store.User
	do(s.DB.Find(&users).Error)
	var erasures []store.Erasure
	do(s.InOrg(o.ID).Find(&erasures).Error)
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

	var noted []store.Event
	do(s.InOrg(o.ID).Where("action = ?", "export:org").Find(&noted).Error)
	if len(noted) != 1 || noted[0].User == "" || noted[0].Value != nil {
		t.Fatalf("one export should be on the record, by name: %+v", noted)
	}
}
