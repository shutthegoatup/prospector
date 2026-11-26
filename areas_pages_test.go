package main

import (
	"encoding/json"
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

var (
	tagRe   = regexp.MustCompile(`<[a-z][a-z0-9]*\s[^>]*>`)
	attrRe  = regexp.MustCompile(`([a-z-]+)="([^"]*)"`)
	numbers = regexp.MustCompile(`\b\d+\b`)
)

// control is a request a page would make when someone used one of its controls.
type control struct{ method, path, body string }

// controls are the requests the page script would send for page's buttons, forms and fields.
func controls(page string) []control {
	var out []control
	for _, tag := range tagRe.FindAllString(page, -1) {
		a := map[string]string{}
		for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
			a[m[1]] = html.UnescapeString(m[2])
		}
		method := a["data-method"]
		if method == "" {
			method = "POST"
		}
		switch {
		case a["data-post"] != "":
			body := a["data-body"]
			if body == "" {
				body = "{}"
			}
			out = append(out, control{method, a["data-post"], body})
		case a["data-form"] != "":
			out = append(out, control{method, a["data-form"], "{}"})
		case a["data-mark"] != "":
			out = append(out, control{"POST", "ui/mark", `{"id":` + a["data-id"] + `,"step":"` + a["data-mark"] + `"}`})
		case a["data-field"] != "":
			change, _ := json.Marshal(map[string]string{a["data-field"]: ""})
			switch {
			case a["data-deal"] != "":
				out = append(out, control{"PATCH", "api/v1/deals/" + a["data-deal"], string(change)})
			case a["data-sub"] != "":
				out = append(out, control{"PATCH", "api/v1/subscriptions/" + a["data-sub"], string(change)})
			case a["data-enquiry"] != "":
				out = append(out, control{"PATCH", "api/v1/enquiries/" + a["data-enquiry"], string(change)})
			case a["data-campaign"] != "":
				out = append(out, control{"PATCH", "api/v1/campaigns/" + a["data-campaign"], string(change)})
			case a["data-step"] != "":
				out = append(out, control{"PATCH", "api/v1/onboarding-steps/" + a["data-step"], `{"due":""}`})
			case a["data-id"] != "":
				// A person's own deal is changed through them; anything else is their own field.
				if of, ok := strings.CutPrefix(a["data-field"], "deal_"); ok {
					deal, _ := json.Marshal(map[string]string{of: ""})
					out = append(out, control{"PATCH", "api/v1/contacts/" + a["data-id"] + "/deal", string(deal)})
				} else {
					out = append(out, control{"PATCH", "api/v1/contacts/" + a["data-id"], string(change)})
				}
			}
		}
	}
	return out
}

// Hiding another area's controls is only worth having if no page still offers one.
func TestNoPageOffersWorkItsViewerWouldBeRefused(t *testing.T) {
	s, o, srv, owner := signedIn(t)
	o.Settings.OnboardingSteps = "them: Send your product files, 7 days\nus: Build the configurator, 21 days"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	today := time.Now().Format("2006-01-02")
	c := repliedPerson(t, s, o, "Hannah Pryce")
	must(s.QuickAddPerson(o, QuickAdd{Name: "Tobias Wren", Company: "Fjordline Saunas", Linkedin: "https://www.linkedin.com/in/tobias-wren"}, "Ada Lovelace"))
	must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, AddCompanyIn{Name: "Corvane Boats", Facts: []FactIn{{Field: "configurator", Value: "Day boat builder", SourceURL: "https://corvane.example"}}}))
	must(s.AddEnquiry(o.ID, "form", EnquiryIn{Kind: "support", Name: "Edsger Dijkstra", Email: "edsger@eindhoven.example", Message: "Colours look pale on phones"}))
	must(s.AddSubscription(o.ID, SubscriptionIn{Company: *c.CompanyID, Name: "Configurator, annual", Start: "2026-01-12", TermMonths: 12}, "Ada Lovelace"))
	must(s.StartOnboarding(o.ID, OnboardingIn{Company: *c.CompanyID, Start: today}, "Ada Lovelace"))
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders with a configurator.", "Ada Lovelace"))
	must(s.AddCampaign(o.ID, CampaignIn{Name: "Spring price list", Audience: AudienceConversations}, "Ada Lovelace"))
	must(s.Suppress(o.ID, "nobody@quillon.example", "Ada Lovelace"))
	must(s.AgentAddContact(o, Actor{Name: "agent:scout"}, AddContactIn{Company: "Corvane Boats", Name: "Iris Vane", Title: "Head of Digital", SourceURL: "https://corvane.example/team"}))
	must(s.AgentNote(o.ID, Actor{Name: "agent:scout"}, NoteIn{Task: task.ID, Kind: "next", Text: "Boat shows list more builders."}))
	must(s.StartDeal(o.ID, c.ID, "renewal", "Ada Lovelace"))
	won := must(s.QuickAddPerson(o, QuickAdd{Name: "Ines Moreau", Company: "Belair Kitchens"}, "Ada Lovelace"))
	if err := s.Mark(o.ID, won.ID, "replied", today, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(o.ID, won.ID, "stage", "won", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	runs := must(s.Onboardings(o.ID, *c.CompanyID, today))
	if err := s.SetOnboardingStep(o.ID, runs[0].Steps[0].ID, "done", "yes", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}

	pages := []string{"today", "research", "conversations", "companies/" + itoa(*c.CompanyID), "add", "enquiries", "subscriptions", "onboarding", "campaigns", "tasks", "tasks/" + itoa(task.ID), "settings", "app/research", "app/campaigns", "app/onboarding", "app/subscriptions"}
	base := srv.URL + "/o/alpha/"
	offered := func(cl *http.Client) []control {
		var all []control
		for _, p := range pages {
			code, page := call(t, cl, "GET", base+p, "", nil)
			if code != http.StatusOK {
				t.Fatalf("%s: %d", p, code)
			}
			all = append(all, controls(page)...)
		}
		return all
	}
	// has matches a control exactly, as "METHOD path" with ids written as N, so one
	// control can't stand in for another that shares its prefix.
	has := func(all []control, want string) bool {
		for _, c := range all {
			if c.method+" "+numbers.ReplaceAllString(c.path, "N") == want {
				return true
			}
		}
		return false
	}

	everything := offered(owner)
	for _, want := range []string{
		"POST ui/mark", "PATCH api/v1/contacts/N", "PATCH api/v1/companies/N", "POST api/v1/contacts/N/pick", "POST ui/note/delete",
		"PATCH api/v1/deals/N", "DELETE api/v1/deals/N", "POST api/v1/deals", "POST api/v1/onboardings",
		"PATCH api/v1/enquiries/N", "POST api/v1/enquiries/N/answers", "POST api/v1/enquiries/N/conversation",
		"PATCH api/v1/subscriptions/N", "PATCH api/v1/onboarding-steps/N", "PATCH api/v1/campaigns/N",
		"POST api/v1/tasks/N/requests", "POST api/v1/suppressions", "DELETE api/v1/suppressions/N",
	} {
		if !has(everything, want) {
			t.Fatalf("the seeded pages offer an owner no %s control, so the test proves nothing about it", want)
		}
	}

	for _, who := range []struct {
		email string
		areas []string
		sees  []string
		not   []string
	}{
		{"grace@example.com", []string{areaSupport},
			[]string{"PATCH api/v1/enquiries/N", "POST api/v1/enquiries/N/answers", "PATCH api/v1/subscriptions/N", "PATCH api/v1/onboarding-steps/N", "POST api/v1/onboardings", "POST api/v1/enquiries/N/conversation", "POST api/v1/suppressions"},
			[]string{"POST ui/mark", "PATCH api/v1/deals/N", "PATCH api/v1/companies/N", "POST api/v1/contacts/N/pick", "POST ui/note/delete", "POST api/v1/tasks/N/requests", "DELETE api/v1/suppressions/N"}},
		{"alan@example.com", []string{areaSales},
			[]string{"POST ui/mark", "PATCH api/v1/deals/N", "PATCH api/v1/companies/N", "POST api/v1/contacts/N/pick", "POST ui/note/delete", "POST api/v1/tasks/N/requests", "POST api/v1/onboardings", "POST api/v1/suppressions"},
			[]string{"PATCH api/v1/enquiries/N", "POST api/v1/enquiries/N/answers", "PATCH api/v1/subscriptions/N", "PATCH api/v1/onboarding-steps/N"}},
		{"carol@example.com", nil, nil,
			[]string{"POST ui/mark", "PATCH api/v1/enquiries/N", "POST api/v1/enquiries/N/conversation", "POST api/v1/onboardings", "POST api/v1/suppressions"}},
	} {
		u := mustUser(t, s, o, who.email, who.email, "member")
		if err := s.SetAreas(o.ID, u.ID, who.areas); err != nil {
			t.Fatal(err)
		}
		cl := signInAs(t, srv, who.email, "correct horse battery")
		mine := offered(cl)
		for _, p := range who.sees {
			if !has(mine, p) {
				t.Errorf("%s isn't offered %s, which is theirs to do", who.email, p)
			}
		}
		for _, p := range who.not {
			if has(mine, p) {
				t.Errorf("%s is offered %s, which isn't theirs to do", who.email, p)
			}
		}
		// The area is checked before any row is looked for, so aiming at none keeps each
		// person's pages as seeded for the next.
		for _, c := range mine {
			code, body := call(t, cl, c.method, base+numbers.ReplaceAllString(c.path, "999999"), "application/json", json.RawMessage(numbers.ReplaceAllString(c.body, "999999")))
			if code == http.StatusForbidden && strings.Contains(body, "Team list") {
				t.Errorf("%s is offered %s %s, and refused it: %s", who.email, c.method, c.path, body)
			}
		}
		if _, sidebar := call(t, cl, "GET", base+"companies", "", nil); (len(who.areas) < 2) != strings.Contains(sidebar, ">view</span>") {
			t.Errorf("%s: the sidebar's view tags don't match their areas", who.email)
		}
	}
}
