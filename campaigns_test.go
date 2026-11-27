package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/internal/paging"
)

// person adds someone at a company with an address, in conversation unless cold.
func person(t *testing.T, s *Store, o *Org, name, company, email string, replied bool) *Contact {
	t.Helper()
	c := must(s.QuickAddPerson(o, QuickAdd{Name: name, Company: company, Email: email}, "Ada Lovelace"))
	if replied {
		if err := s.Mark(o.ID, c.ID, "replied", today(), "Ada Lovelace"); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func leftOut(people []Recipient) map[string]string {
	out := map[string]string{}
	for _, p := range people {
		out[p.Name] = ""
		if p.LeftOut != nil {
			out[p.Name] = p.LeftOut.Error()
		}
	}
	return out
}

func TestACampaignGoesOnlyToPeopleWithARelationship(t *testing.T) {
	t.Parallel()
	s, o, _, _ := signedIn(t)
	person(t, s, o, "Hannah Pryce", "Larkmoor Yachts", "hannah@larkmoor.example", true)
	person(t, s, o, "Tom Ellery", "Larkmoor Yachts", "", true)
	person(t, s, o, "Jo Marsh", "Larkmoor Yachts", "jo@larkmoor.example", true)
	person(t, s, o, "Jo Marsh's colleague", "Larkmoor Yachts", "JO@larkmoor.example", true)
	person(t, s, o, "Sven Dahl", "Nordkap Boats", "sven@nordkap.example", true)
	person(t, s, o, "Cold Prospect", "Quayside Ltd", "cold@quayside.example", false)
	declined := person(t, s, o, "Said No", "Quayside Ltd", "no@quayside.example", true)
	if err := s.Mark(o.ID, declined.ID, "declined", today(), "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	must(s.Suppress(o.ID, "jo@larkmoor.example", "Ada Lovelace"))
	o.Settings.NeverContact = "nordkap.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}

	c := must(s.AddCampaign(o.ID, CampaignIn{Name: "Spring price list", Subject: "Our spring prices", Body: "Hello"}, "Ada Lovelace"))
	people := must(s.Audience(o, *c))
	got := leftOut(people)
	for name, want := range map[string]string{
		"Hannah Pryce":         "",
		"Tom Ellery":           "no email address",
		"Jo Marsh":             "on the list not to be mailed",
		"Jo Marsh's colleague": "on the list not to be mailed",
		"Sven Dahl":            "on the never-contact list",
	} {
		if have, there := got[name]; !there || have != want {
			t.Errorf("%s: left out %q (there: %v), want %q", name, have, there, want)
		}
	}
	for _, never := range []string{"Cold Prospect", "Said No"} {
		if _, there := got[never]; there {
			t.Errorf("%s is in the audience of a campaign to people in conversation", never)
		}
	}
	if people[0].Name != "Hannah Pryce" || people[0].LeftOut != nil {
		t.Errorf("those it goes to don't come first: %+v", people)
	}

	// Two people sharing an address get one mail.
	listed, _, err := s.Suppressions(o.ID, paging.Page{})
	if err != nil || len(listed) != 1 {
		t.Fatalf("the list: %+v (%v)", listed, err)
	}
	if err := s.Unsuppress(o.ID, listed[0].ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	got = leftOut(must(s.Audience(o, *c)))
	if got["Jo Marsh"] != "" || got["Jo Marsh's colleague"] != "same address as Jo Marsh" {
		t.Errorf("a shared address: %q and %q", got["Jo Marsh"], got["Jo Marsh's colleague"])
	}

	// Customers: everyone at a company with a subscription that hasn't ended, whether or
	// not they are in conversation; nobody at one whose subscription has.
	var hannah Contact
	if err := s.DB.First(&hannah, people[0].ContactID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSubscription(o.ID, SubscriptionIn{Company: *hannah.CompanyID, Name: "Standard plan", Start: "2026-01-12", TermMonths: 12}, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSubscription(o.ID, SubscriptionIn{Company: *declined.CompanyID, Name: "Old plan", Start: "2020-01-01", TermMonths: 12, Then: ThenEnds}, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	c = must(s.ChangeCampaign(o.ID, c.ID, []Change{{Field: "audience", Value: AudienceCustomers}}, "Ada Lovelace"))
	got = leftOut(must(s.Audience(o, *c)))
	for _, name := range []string{"Hannah Pryce", "Tom Ellery", "Jo Marsh"} {
		if _, there := got[name]; !there {
			t.Errorf("%s, at a customer, is not in its audience", name)
		}
	}
	for _, name := range []string{"Sven Dahl", "Cold Prospect", "Said No"} {
		if _, there := got[name]; there {
			t.Errorf("%s, at no current customer or declined, is in the customers' audience", name)
		}
	}
}

func TestCampaignDraftsAreKeptByMembersAndKeys(t *testing.T) {
	t.Parallel()
	s, o, srv, member := signedIn(t)
	other := mustOrg(t, s, "beta")
	key := func(org int64, scopes ...string) string {
		secret, _, err := s.CreateKey(org, "robot "+strings.Join(scopes, "+"), scopes, "Ada Lovelace")
		if err != nil {
			t.Fatal(err)
		}
		return secret
	}
	reader, writer, stranger := key(o.ID, "read"), key(o.ID, "campaigns:write"), key(other.ID, "read", "campaigns:write")
	person(t, s, o, "Hannah Pryce", "Larkmoor Yachts", "hannah@larkmoor.example", true)
	list := srv.URL + "/o/alpha/api/v1/campaigns"

	if code, body := asKey(t, "POST", list, reader, map[string]any{"name": "Spring"}); code != http.StatusForbidden || !strings.Contains(body, "campaigns:write") {
		t.Errorf("a read key writing a campaign: %d %s", code, body)
	}
	for why, bad := range map[string]map[string]any{"no name": {"name": " "}, "an audience that isn't one": {"name": "Spring", "audience": "everyone"}, "a subject too long": {"name": "Spring", "subject": strings.Repeat("a", 201)}} {
		if code, _ := asKey(t, "POST", list, writer, bad); code != http.StatusBadRequest {
			t.Errorf("a campaign with %s: %d", why, code)
		}
	}
	code, body := asKey(t, "POST", list, writer, map[string]any{"name": "Spring price list", "subject": "Our\r\nspring  prices", "body": "Hello"})
	if code != http.StatusCreated || !strings.Contains(body, `"audience":"conversations"`) || !strings.Contains(body, `"subject":"Our spring prices"`) || !strings.Contains(body, `"status":"draft"`) {
		t.Fatalf("writing a campaign: %d %s", code, body)
	}
	c := must(s.Campaigns(o.ID))[0]
	one := list + "/" + itoa(c.ID)

	// Saved together or not at all.
	if code, _ := asKey(t, "PATCH", one, writer, map[string]any{"name": "Renamed", "audience": "everyone"}); code != http.StatusBadRequest {
		t.Errorf("a change with one part refused: %d", code)
	}
	if now := must(s.Campaign(o.ID, c.ID)); now.Name != "Spring price list" {
		t.Errorf("the part before the refused one was saved: %q", now.Name)
	}
	if code, body := asKey(t, "PATCH", one, writer, map[string]any{"body": "New prices from March.", "audience": "customers"}); code != http.StatusOK || !strings.Contains(body, "New prices") || !strings.Contains(body, `"updated_by":"agent:robot campaigns:write"`) {
		t.Errorf("changing a draft: %d %s", code, body)
	}
	if code, body := asKey(t, "PATCH", one, writer, map[string]any{"audience": "conversations"}); code != http.StatusOK {
		t.Errorf("changing it back: %d %s", code, body)
	}

	code, body = asKey(t, "GET", one+"/audience", reader, nil)
	if code != http.StatusOK || !strings.Contains(body, `"going":1`) || !strings.Contains(body, `"left_out":0`) || !strings.Contains(body, "hannah@larkmoor.example") {
		t.Errorf("its audience: %d %s", code, body)
	}
	if code, body := asKey(t, "GET", list, reader, nil); code != http.StatusOK || !strings.Contains(body, "Spring price list") {
		t.Errorf("listing campaigns: %d %s", code, body)
	}

	theirs := srv.URL + "/o/beta/api/v1/campaigns/" + itoa(c.ID)
	for what, try := range map[string][2]string{"reading its audience": {"GET", theirs + "/audience"}, "changing it": {"PATCH", theirs}, "removing it": {"DELETE", theirs}} {
		var sent any
		if try[0] == "PATCH" {
			sent = map[string]any{"name": "Theirs now"}
		}
		if code, body := asKey(t, try[0], try[1], stranger, sent); code != http.StatusNotFound {
			t.Errorf("another organisation %s by number: %d %s", what, code, body)
		}
	}
	if code, _ := asKey(t, "GET", list, stranger, nil); code < 400 {
		t.Errorf("another organisation's key listing ours: %d", code)
	}

	// One that isn't a draft any more is kept as it went.
	if err := s.DB.Model(&Campaign{}).Where("id = ?", c.ID).Update("status", "sent").Error; err != nil {
		t.Fatal(err)
	}
	if code, _ := asKey(t, "PATCH", one, writer, map[string]any{"name": "Changed after"}); code != http.StatusBadRequest {
		t.Errorf("changing one that went: %d", code)
	}
	if code, _ := asKey(t, "DELETE", one, writer, nil); code != http.StatusBadRequest {
		t.Errorf("removing one that went: %d", code)
	}
	if err := s.DB.Model(&Campaign{}).Where("id = ?", c.ID).Update("status", CampaignDraft).Error; err != nil {
		t.Fatal(err)
	}

	// What the campaigns page reads: the draft, who it would go to, and why it can't go yet.
	if _, body := call(t, member, "GET", list, "", nil); !strings.Contains(body, `"name":"Spring price list"`) || !strings.Contains(body, `"not_ready":"`) || strings.Contains(body, `"not_ready":""`) {
		t.Errorf("the campaigns list: %s", body)
	}
	if _, body := call(t, member, "GET", one+"/audience", "", nil); !strings.Contains(body, `"going":1`) {
		t.Errorf("the draft should go to 1 person: %s", body)
	}
	if code, body := call(t, member, "DELETE", one, "", nil); code != http.StatusNoContent {
		t.Errorf("removing a draft: %d %s", code, body)
	}
}

// The postal address is a setting like the others, and saving the rest keeps it.
func TestThePostalAddressIsKept(t *testing.T) {
	t.Parallel()
	s, o, srv, member := signedIn(t)
	save := func(postal string) {
		t.Helper()
		if code, body := call(t, member, "PATCH", srv.URL+"/o/alpha/api/v1/settings", "application/json", map[string]any{"name": "Alpha", "color": "#56c5f4", "postal_address": postal}); code != http.StatusOK {
			t.Fatalf("saving settings: %d %s", code, body)
		}
	}
	save("Alpha Ltd\n1 Example Street\nLeith EH6 1AA")
	if now := must(s.OrgBySlug("alpha")); now.Settings.PostalAddress != "Alpha Ltd\n1 Example Street\nLeith EH6 1AA" {
		t.Errorf("the postal address saved as %q", now.Settings.PostalAddress)
	}
	if _, body := call(t, member, "GET", srv.URL+"/o/alpha/api/v1/settings", "", nil); !strings.Contains(body, `"postal_address":"Alpha Ltd\n1 Example Street\nLeith EH6 1AA"`) {
		t.Errorf("Settings doesn't read the postal address back, so the next save would lose it: %s", body)
	}
	_ = o
}

// What the review of drafts found: a never-contact LinkedIn profile, a subscription not
// yet started, and a body's last line.
func TestTheAudienceHoldsAtItsEdges(t *testing.T) {
	t.Parallel()
	s, o, srv, member := signedIn(t)
	listedByProfile := must(s.QuickAddPerson(o, QuickAdd{Name: "Profile Listed", Company: "Larkmoor Yachts", Email: "listed@larkmoor.example", Linkedin: "https://www.linkedin.com/in/li-never"}, "Ada Lovelace"))
	if err := s.Mark(o.ID, listedByProfile.ID, "replied", today(), "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	o.Settings.NeverContact = "linkedin.com/in/li-never"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	c := must(s.AddCampaign(o.ID, CampaignIn{Name: "Spring", Body: "Prices change.\n-- \nAda\n\n\n"}, "Ada Lovelace"))
	if got := leftOut(must(s.Audience(o, *c)))["Profile Listed"]; got != "on the never-contact list" {
		t.Errorf("someone whose LinkedIn profile is on the never-contact list: left out %q", got)
	}
	if c.Body != "Prices change.\n-- \nAda" {
		t.Errorf("the body was saved as %q", c.Body)
	}

	later := person(t, s, o, "Not Yet", "Quayside Ltd", "notyet@quayside.example", false)
	if _, err := s.AddSubscription(o.ID, SubscriptionIn{Company: *later.CompanyID, Name: "Starts later", Start: "2099-01-01", TermMonths: 12}, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	c = must(s.ChangeCampaign(o.ID, c.ID, []Change{{Field: "audience", Value: AudienceCustomers}}, "Ada Lovelace"))
	if _, there := leftOut(must(s.Audience(o, *c)))["Not Yet"]; there {
		t.Error("someone at a company whose subscription has not started is in the customers' audience")
	}

	if code, _ := call(t, member, "PATCH", srv.URL+"/o/alpha/api/v1/settings", "application/json", map[string]any{"name": "Alpha", "color": "#56c5f4", "postal_address": strings.Repeat("a", 301)}); code != http.StatusBadRequest {
		t.Errorf("a postal address past 300 characters: %d", code)
	}
}
