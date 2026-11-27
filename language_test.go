package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// translatedInto swaps in languages to choose from, as Prospector has none translated yet. en-XA
// stands in for a translation, so what is in it shows. The swap is why these tests stay
// serial.
func translatedInto(t *testing.T, translated ...string) {
	t.Helper()
	was := locales
	locales = localeSet{Source: was.Source, Translated: translated}
	t.Cleanup(func() { locales = was })
}

// inLanguage is the language a page or a Problem came back in, for a browser asking for
// accept.
func inLanguage(t *testing.T, c *http.Client, u, accept string) (string, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Accept-Language", accept)
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	body := string(b)
	if _, rest, ok := strings.Cut(body, `<html lang="`); ok {
		return rest[:strings.IndexByte(rest, '"')], body
	}
	return res.Header.Get("Content-Language"), body
}

func TestAMembersOwnLanguageOutranksTheirBrowsers(t *testing.T) {
	translatedInto(t, "de", "en-XA")
	_, _, srv, client := signedIn(t)
	me := srv.URL + "/o/alpha/api/v1/me"
	page := srv.URL + "/o/alpha/today"
	if got, _ := inLanguage(t, client, page, "de"); got != "de" {
		t.Errorf("before choosing, a browser asking for German: %s", got)
	}
	if code, body := call(t, client, "PATCH", me, "application/json", map[string]string{"language": "en-XA"}); code != http.StatusOK || !strings.Contains(body, `"language":"en-XA"`) {
		t.Fatalf("choosing a language: %d %s", code, body)
	}
	if got, _ := inLanguage(t, client, page, "de"); got != "en-XA" {
		t.Errorf("a chosen language, from a browser asking for German: %s", got)
	}
	if got, body := inLanguage(t, client, srv.URL+"/o/alpha/api/v1/companies/9999999/overview", "de"); got != "en-XA" || !strings.Contains(body, `"detail":"[`) {
		t.Errorf("the server's own words: %s %s", got, body)
	}
	// A pick with ?lang= still outranks it, until it is forgotten.
	if got, _ := inLanguage(t, client, page+"?lang=de", "de"); got != "de" {
		t.Errorf("picked by name: %s", got)
	}
	if got, _ := inLanguage(t, client, page+"?lang=", "de"); got != "en-XA" {
		t.Errorf("a pick forgotten: %s", got)
	}
	for _, no := range []string{"xx", "fr"} {
		if code, _ := call(t, client, "PATCH", me, "application/json", map[string]string{"language": no}); code != http.StatusBadRequest {
			t.Errorf("choosing %q, which Prospector isn't in: %d", no, code)
		}
	}
	if code, body := call(t, client, "PATCH", me, "application/json", map[string]string{"signature": "Ada"}); code != http.StatusOK || !strings.Contains(body, `"language":"en-XA"`) {
		t.Errorf("changing the signature changed the language: %d %s", code, body)
	}
	if code, _ := call(t, client, "PATCH", me, "application/json", map[string]string{"language": ""}); code != http.StatusOK {
		t.Fatalf("leaving it to the browser: %d", code)
	}
	if got, _ := inLanguage(t, client, page, "de"); got != "de" {
		t.Errorf("left to the browser again: %s", got)
	}
}

func TestAnOrganisationSpeaksToItsOutsidersInItsLanguage(t *testing.T) {
	translatedInto(t, "de", "en-XA")
	s, srv, member, box := mailSite(t, nil)
	settings := srv.URL + "/o/alpha/api/v1/settings"
	if code, _ := call(t, member, "PATCH", settings, "application/json", map[string]string{"customer_language": "xx"}); code != http.StatusBadRequest {
		t.Errorf("a language Prospector isn't in: %d", code)
	}
	if code, body := call(t, member, "PATCH", settings, "application/json", map[string]string{"customer_language": "en-XA"}); code != http.StatusOK || !strings.Contains(body, `"customer_language":"en-XA"`) {
		t.Fatalf("choosing the organisation's language: %d %s", code, body)
	}

	// Mail to a new member, who has no language of their own yet.
	if code, body := call(t, member, "POST", srv.URL+"/o/alpha/api/v1/members", "application/json", map[string]string{"email": "grace@example.com", "name": "Grace"}); code != http.StatusCreated {
		t.Fatalf("adding a member: %d %s", code, body)
	}
	if m := box.take(t, 1)[0]; !strings.HasPrefix(m.Subject, "[") {
		t.Errorf("a new member's invitation isn't in the organisation's language: %q", m.Subject)
	}

	// A customer's link and its page.
	o := must(s.OrgBySlug("alpha"))
	o.Settings.OnboardingSteps = "them: Send your product files"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	hannah := must(s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	ob := must(s.StartOnboarding(o.ID, OnboardingIn{Company: *hannah.CompanyID, Name: "Leith site", Start: today()}, "Ada Lovelace"))
	if code, body := call(t, member, "POST", srv.URL+"/o/alpha/api/v1/onboardings/"+itoa(ob.ID)+"/invites", "application/json", map[string]any{"contact": hannah.ID}); code != http.StatusNoContent {
		t.Fatalf("inviting a customer: %d %s", code, body)
	}
	mail := box.take(t, 1)[0]
	if !strings.HasPrefix(mail.Subject, "[") {
		t.Errorf("a customer's link isn't in the organisation's language: %q", mail.Subject)
	}
	link := srv.URL + customerLink.FindStringSubmatch(mail.Body)[1]
	outsider := newMailClient()
	for accept, want := range map[string]string{"ja": "en-XA", "": "en-XA", "de-AT": "de"} {
		if got, _ := inLanguage(t, outsider, link, accept); got != want {
			t.Errorf("a customer's page for a browser asking for %q: %s, want %s", accept, got, want)
		}
	}

	// An unsubscribe link's page.
	token := must(s.UnsubscribeLink(o.ID, "sven@nordkap.example"))
	if got, _ := inLanguage(t, outsider, srv.URL+"/u/"+token, "ja"); got != "en-XA" {
		t.Errorf("an unsubscribe page: %s", got)
	}
	if got, _ := inLanguage(t, outsider, srv.URL+"/u/never-made", "ja"); got != locales.Source {
		t.Errorf("an unsubscribe link that was never made: %s", got)
	}
}
