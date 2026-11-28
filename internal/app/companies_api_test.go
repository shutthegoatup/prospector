package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/store"
)

func TestTheCompaniesListComesThroughTheAPIWithItsFilters(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	other := mustOrg(t, s, "beta")
	scout := store.Actor{Name: "agent:scout", KeyID: 1}
	repliedPerson(t, s, o, "Hannah Pryce")
	must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Corvane Marine", Segment: "tenders"}))
	must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Belair Kitchens", Segment: "kitchens"}))
	must(s.AgentAddCompany(other.ID, scout, research.AddCompanyIn{Name: "Ostrava Boats", Segment: "dinghies"}))
	base := srv.URL + "/o/alpha/api/v1/"
	list := func(query string) api.ListCompanies200JSONResponse {
		t.Helper()
		code, body := call(t, client, "GET", base+"companies"+query, "", nil)
		var out api.ListCompanies200JSONResponse
		if code != http.StatusOK || json.Unmarshal([]byte(body), &out.Body) != nil {
			t.Fatalf("companies%s: %d %s", query, code, body)
		}
		return out
	}

	all := list("")
	if all.Body.Total != 3 || len(all.Body.Items) != 3 || all.Body.Items[0].Name != "Belair Kitchens" || all.Body.NextCursor != "" {
		t.Fatalf("every company of this organisation's, by name: %+v", all.Body)
	}
	if lark := all.Body.Items[2]; lark.Name != "Larkmoor Yachts" || lark.People != 1 || lark.Pipeline != api.PipelineReplied {
		t.Fatalf("a company stands where its furthest contact does: %+v", lark)
	}
	if got := list("?stage=replied"); got.Body.Total != 1 || got.Body.Items[0].Name != "Larkmoor Yachts" {
		t.Fatalf("by stage: %+v", got.Body)
	}
	if got := list("?segment=tenders"); got.Body.Total != 1 || got.Body.Items[0].Name != "Corvane Marine" {
		t.Fatalf("by segment: %+v", got.Body)
	}
	if got := list("?q=hannah"); got.Body.Total != 1 || got.Body.Items[0].Name != "Larkmoor Yachts" {
		t.Fatalf("a search finds the company of someone there: %+v", got.Body)
	}
	first := list("?limit=2")
	if first.Body.Total != 3 || len(first.Body.Items) != 2 || first.Body.NextCursor == "" {
		t.Fatalf("a page of two: %+v", first.Body)
	}
	if rest := list("?limit=2&cursor=" + first.Body.NextCursor); len(rest.Body.Items) != 1 || rest.Body.Items[0].Name != "Larkmoor Yachts" || rest.Body.NextCursor != "" {
		t.Fatalf("the page after: %+v", rest.Body)
	}
	if code, _ := call(t, client, "GET", base+"companies?stage=somewhere", "", nil); code != http.StatusBadRequest {
		t.Fatalf("a stage that isn't one: %d", code)
	}

	code, body := call(t, client, "GET", base+"segments", "", nil)
	if code != http.StatusOK || !strings.Contains(body, `"items":["kitchens","tenders"]`) {
		t.Fatalf("this organisation's segments, in order: %d %s", code, body)
	}
}

func TestACompanysPageComesThroughTheAPI(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	other := mustOrg(t, s, "beta")
	scout := store.Actor{Name: "agent:scout", KeyID: 1}
	co := must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Larkmoor Yachts", Domain: "larkmoor.example",
		Facts: []research.FactIn{{Field: "configurator", Value: "Builds to order online", SourceURL: "https://larkmoor.example/build"}}}))
	hannah := must(s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", SourceURL: "https://larkmoor.example/team"}))
	must(s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", Email: "hannah@larkmoor.example",
		EmailStatus: "guess", SourceURL: "https://larkmoor.example/contact"}))
	if err := s.PickContact(o.ID, o, hannah.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if err := s.Mark(o.ID, hannah.ID, "linkedin", time.Now().Format("2006-01-02"), "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	must(s.AgentNote(o.ID, scout, research.NoteIn{Company: "Larkmoor Yachts", Kind: "next", Text: "Their boat show stand lists a buyer."}))
	theirs := must(s.AgentAddCompany(other.ID, scout, research.AddCompanyIn{Name: "Ostrava Boats"}))
	base := srv.URL + "/o/alpha/api/v1/companies/"

	code, body := call(t, client, "GET", base+itoa(co.ID)+"/overview", "", nil)
	var c api.CompanyOverview
	if code != http.StatusOK || json.Unmarshal([]byte(body), &c) != nil {
		t.Fatalf("the company: %d %s", code, body)
	}
	if c.Name != "Larkmoor Yachts" || c.Stage != api.CompanyOverviewStageQualified || c.Pipeline != api.PipelineOutreach || c.Touches != 1 || c.FoundBy != "agent:scout" {
		t.Fatalf("where it stands: %+v", c)
	}
	if len(c.Facts) != 1 || c.Facts[0].SourceUrl != "https://larkmoor.example/build" {
		t.Fatalf("its facts with their sources: %+v", c.Facts)
	}
	if len(c.People) != 1 || c.People[0].Name != "Hannah Pryce" || c.People[0].Several != "" || len(c.People[0].Pending) != 1 {
		t.Fatalf("its people with what waits for them: %+v", c.People)
	}
	if d := c.People[0].Pending[0]; d.Field != "email" || d.EmailStatus != "guess" {
		t.Fatalf("a guessed email says it's a guess: %+v", d)
	}
	if c.Onboardings == nil || c.Subscriptions == nil || c.Agreements == nil || c.LookingFor == nil {
		t.Fatalf("lists with nothing in them come empty, not missing: %s", body)
	}

	code, body = call(t, client, "GET", base+itoa(co.ID)+"/history?limit=2", "", nil)
	var history api.ListCompanyHistory200JSONResponse
	if code != http.StatusOK || json.Unmarshal([]byte(body), &history.Body) != nil {
		t.Fatalf("its history: %d %s", code, body)
	}
	if history.Body.Total < 3 || len(history.Body.Items) != 2 || history.Body.NextCursor == "" {
		t.Fatalf("a page of its history: %+v", history.Body)
	}
	if h := history.Body.Items[1]; h.Action != "mark:linkedin" || h.Text != "added on LinkedIn · Hannah Pryce" {
		t.Fatalf("what happened, as the page says it: %+v", h)
	}

	code, body = call(t, client, "GET", base+itoa(co.ID)+"/notes", "", nil)
	var notes api.ListCompanyNotes200JSONResponse
	if code != http.StatusOK || json.Unmarshal([]byte(body), &notes.Body) != nil || notes.Body.Total != 1 || notes.Body.Items[0].Kind != "next" {
		t.Fatalf("agents' notes about it: %d %s", code, body)
	}

	for _, path := range []string{"/overview", "/history", "/notes"} {
		if code, _ := call(t, client, "GET", base+itoa(theirs.ID)+path, "", nil); code != http.StatusNotFound {
			t.Errorf("another organisation's company %s: %d", path, code)
		}
	}
}

func TestOnlyReviewersDeleteAgentsNotes(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	scout := store.Actor{Name: "agent:scout", KeyID: 1}
	must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Larkmoor Yachts"}))
	note := func() *store.TaskNote {
		return must(s.AgentNote(o.ID, scout, research.NoteIn{Company: "Larkmoor Yachts", Kind: "dead_end", Text: "No team page."}))
	}
	grace := mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	if err := s.SetAreas(o.ID, grace.ID, []string{"support"}); err != nil {
		t.Fatal(err)
	}
	asGrace := signInAs(t, srv, "grace@example.com", "correct horse battery")
	writer, _ := must2(s.CreateKey(o.ID, "scout", []string{"research:write"}, "Ada Lovelace"))
	reviewer, _ := must2(s.CreateKey(o.ID, "reviewer", []string{"tasks:write"}, "Ada Lovelace"))
	base := srv.URL + "/o/alpha/api/v1/notes/"

	n := note()
	if code, body := call(t, asGrace, "DELETE", base+itoa(n.ID), "", nil); code != http.StatusForbidden || !strings.Contains(body, "sales") {
		t.Fatalf("someone in support only deleting a note: %d %s", code, body)
	}
	if code, body := asKey(t, "DELETE", base+itoa(n.ID), writer, nil); code != http.StatusForbidden || !strings.Contains(body, "tasks:write") {
		t.Fatalf("a research key deleting a note: %d %s", code, body)
	}
	if code, body := call(t, ada, "DELETE", base+itoa(n.ID), "", nil); code != http.StatusNoContent {
		t.Fatalf("deleting a note: %d %s", code, body)
	}
	if code, _ := call(t, ada, "DELETE", base+itoa(n.ID), "", nil); code != http.StatusNotFound {
		t.Fatalf("deleting it again: %d", code)
	}
	if code, body := asKey(t, "DELETE", base+itoa(note().ID), reviewer, nil); code != http.StatusNoContent {
		t.Fatalf("a tasks:write key deleting a note: %d %s", code, body)
	}
}

func TestOnlyAnOwnerExportsOrErasesAPersonThroughTheAPI(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	other := mustOrg(t, s, "beta")
	hannah := repliedPerson(t, s, o, "Hannah Pryce")
	theirs := must(s.QuickAddPerson(other, crm.QuickAdd{Name: "Tom Ellery", Company: "Corvane Marine"}, "someone"))
	mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	asGrace := signInAs(t, srv, "grace@example.com", "correct horse battery")
	reader, _ := must2(s.CreateKey(o.ID, "reader", []string{"read"}, "Ada Lovelace"))
	privacy, _ := must2(s.CreateKey(o.ID, "privacy", []string{"people:privacy"}, "Ada Lovelace"))
	base := srv.URL + "/o/alpha/api/v1/contacts/"

	for _, do := range []string{"/export", "/erase"} {
		if code, body := call(t, asGrace, "POST", base+itoa(hannah.ID)+do, "application/json", nil); code != http.StatusForbidden || !strings.Contains(body, "owner") {
			t.Errorf("a member who isn't an owner %s: %d %s", do, code, body)
		}
		if code, body := asKey(t, "POST", base+itoa(hannah.ID)+do, reader, nil); code != http.StatusForbidden || !strings.Contains(body, "people:privacy") {
			t.Errorf("a read key %s: %d %s", do, code, body)
		}
		if code, _ := call(t, ada, "POST", base+itoa(theirs.ID)+do, "application/json", nil); code != http.StatusNotFound {
			t.Errorf("%s someone in another organisation: %d", do, code)
		}
	}

	code, body := call(t, ada, "POST", base+itoa(hannah.ID)+"/export", "application/json", nil)
	var held map[string]any
	if code != http.StatusOK || json.Unmarshal([]byte(body), &held) != nil || held["company"] != "Larkmoor Yachts" || held["person"] == nil {
		t.Fatalf("an owner exporting what is held on her: %d %s", code, body)
	}
	if code, _ := asKey(t, "POST", base+itoa(hannah.ID)+"/export", privacy, nil); code != http.StatusOK {
		t.Fatalf("a key with people:privacy exporting: %d", code)
	}

	if code, body := call(t, ada, "POST", base+itoa(hannah.ID)+"/erase", "application/json", nil); code != http.StatusNoContent {
		t.Fatalf("an owner erasing her: %d %s", code, body)
	}
	var left int64
	s.InOrg(o.ID).Model(&store.Contact{}).Where("id = ?", hannah.ID).Count(&left)
	if left != 0 {
		t.Fatal("she is still held after being erased")
	}
	if code, _ := asKey(t, "POST", base+itoa(hannah.ID)+"/erase", privacy, nil); code != http.StatusNotFound {
		t.Fatalf("erasing someone already erased: %d", code)
	}
}

func TestReadingCompaniesNeedsTheReadScope(t *testing.T) {
	t.Parallel()
	s, o, srv, _ := signedIn(t)
	co := must(s.AgentAddCompany(o.ID, store.Actor{Name: "agent:scout", KeyID: 1}, research.AddCompanyIn{Name: "Larkmoor Yachts"}))
	writer, _ := must2(s.CreateKey(o.ID, "scout", []string{"research:write"}, "Ada Lovelace"))
	base := srv.URL + "/o/alpha/api/v1/"
	for _, path := range []string{"companies", "segments", "companies/" + itoa(co.ID) + "/overview", "companies/" + itoa(co.ID) + "/history", "companies/" + itoa(co.ID) + "/notes"} {
		if code, body := asKey(t, "GET", base+path, writer, nil); code != http.StatusForbidden || !strings.Contains(body, "read") {
			t.Errorf("a key without read reading %s: %d %s", path, code, body)
		}
	}
}
