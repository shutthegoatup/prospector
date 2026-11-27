package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/internal/paging"
)

// The reads an agent makes with a key: what they answer, how they page, and whom they
// refuse. They hold whichever way the routes are served.
func TestAnAgentsReadsAnswerAsTheyAlwaysHave(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	other := mustOrg(t, s, "beta")
	for i, company := range []string{"Larkmoor Yachts", "Larkspur Marine"} {
		must(s.QuickAddPerson(o, QuickAdd{Name: []string{"Hannah Pryce", "Theo Marsh"}[i], Company: company}, "Ada Lovelace"))
	}
	lark := must(s.CompanyRef(o.ID, "Larkmoor Yachts"))
	reader, _, err := s.CreateKey(o.ID, "reader", []string{"read"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	writer, _, err := s.CreateKey(o.ID, "writer", []string{"drafts:write"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	theirs, _, err := s.CreateKey(other.ID, "theirs", []string{"read"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	get := func(key, path string) (*http.Response, string) {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+"/o/alpha/api/v1/"+path, nil)
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res, string(b)
	}

	res, body := get(reader, "search?q=lark&limit=1")
	var found SearchOut
	if res.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &found) != nil || len(found.Hits) == 0 || found.NextCursor == "" {
		t.Fatalf("a page of a search: %d %s", res.StatusCode, body)
	}
	if !strings.Contains(res.Header.Get("Link"), `rel="next"`) {
		t.Errorf("a search with more to come gives no next link: %q", res.Header.Get("Link"))
	}
	if res, body = get(reader, "search?q=lark&limit=1&cursor="+found.NextCursor); res.StatusCode != http.StatusOK || !strings.Contains(body, `"hits"`) {
		t.Errorf("the next page of a search: %d %s", res.StatusCode, body)
	}

	for _, ref := range []string{strconv.FormatInt(lark.ID, 10), "Larkmoor%20Yachts"} {
		res, body = get(reader, "companies/"+ref)
		var c CompanyDetail
		if res.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &c) != nil || c.Company.ID != lark.ID {
			t.Errorf("a company by %s: %d %s", ref, res.StatusCode, body)
		}
		for _, field := range []string{`"facts"`, `"people"`, `"history"`, `"notes"`} {
			if !strings.Contains(body, field) {
				t.Errorf("a company by %s lacks %s: %s", ref, field, body)
			}
		}
	}
	if res, body = get(reader, "companies/Nowhere%20Boats"); res.StatusCode != http.StatusNotFound || !strings.Contains(body, `"detail"`) {
		t.Errorf("a company not here: %d %s", res.StatusCode, body)
	}

	if res, body = get(reader, "due"); res.StatusCode != http.StatusOK || !strings.Contains(body, `"items"`) {
		t.Errorf("what is due: %d %s", res.StatusCode, body)
	}

	for _, path := range []string{"search?q=lark", "companies/" + strconv.FormatInt(lark.ID, 10), "due"} {
		if res, _ := get("", path); res.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(res.Header.Get("WWW-Authenticate"), "Bearer") {
			t.Errorf("%s with no key: %d %q", path, res.StatusCode, res.Header.Get("WWW-Authenticate"))
		}
		if res, _ := get("pk_nothing", path); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s with an unknown key: %d", path, res.StatusCode)
		}
		if res, _ := get(theirs, path); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s with another organisation's key: %d", path, res.StatusCode)
		}
		if res, _ := get(writer, path); res.StatusCode != http.StatusForbidden || !strings.Contains(res.Header.Get("WWW-Authenticate"), "insufficient_scope") {
			t.Errorf("%s with a key lacking read: %d %q", path, res.StatusCode, res.Header.Get("WWW-Authenticate"))
		}
		// A person signed in has the app's own pages for these; the routes are for keys.
		req, _ := http.NewRequest("GET", srv.URL+"/o/alpha/api/v1/"+path, nil)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s signed in, with no key: %d", path, res.StatusCode)
		}
	}
	if res, _ := get(reader, "search?q=lark&limit=-1"); res.StatusCode != http.StatusBadRequest {
		t.Errorf("a search asking for a negative page: %d", res.StatusCode)
	}
	if res, _ := get(reader, "due?cursor=nonsense"); res.StatusCode != http.StatusBadRequest {
		t.Errorf("what is due from a cursor that isn't one: %d", res.StatusCode)
	}
}

// Served through the spec or not, an agent's reads say exactly what the store does,
// which is what the MCP tools answer too: a field the spec lacked would be lost.
func TestAnAgentsReadsSayEverythingTheStoreDoes(t *testing.T) {
	t.Parallel()
	s, o, srv, _ := signedIn(t)
	secret, k, err := s.CreateKey(o.ID, "scout", []string{"read", "research:write"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	scout := Actor{Name: "agent:scout", KeyID: k.ID}
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders with a configurator.", "Ada Lovelace"))
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor Yachts", Domain: "larkmoor.example", Task: task.ID,
		Facts: []FactIn{{Field: "configurator", Value: "Day cruiser builder", SourceURL: "https://larkmoor.example/build"}}}))
	must(s.AgentAddContact(o, scout, AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", Title: "Head of Marketing",
		SourceURL: "https://larkmoor.example/team", Task: task.ID}))
	must(s.AgentNote(o.ID, scout, NoteIn{Task: task.ID, Company: "Larkmoor Yachts", Kind: "next", Text: "Look at their dealer list."}))
	lark := must(s.CompanyRef(o.ID, "Larkmoor Yachts"))

	base := srv.URL + "/o/alpha/api/v1/"
	code, body := asKey(t, "GET", base+"search?q=lark", secret, nil)
	hits, next := must2(s.AgentSearch(o.ID, scout, "lark", paging.Page{}))
	if code != http.StatusOK {
		t.Fatalf("search: %d %s", code, body)
	}
	sameJSON(t, "a search", body, SearchOut{hits, next})
	if !strings.Contains(body, `"last_agent"`) || !strings.Contains(body, `"by_you":true`) {
		t.Errorf("a search should say which agent last changed the company: %s", body)
	}

	code, body = asKey(t, "GET", fmt.Sprintf("%scompanies/%d", base, lark.ID), secret, nil)
	if code != http.StatusOK {
		t.Fatalf("a company: %d %s", code, body)
	}
	sameJSON(t, "a company", body, must(s.AgentCompany(o.ID, scout, strconv.FormatInt(lark.ID, 10), paging.Page{})))
	for _, want := range []string{`"source_url"`, `"title":"Head of Marketing"`, `"action"`, `"kind":"next"`, `"domain":"larkmoor.example"`} {
		if !strings.Contains(body, want) {
			t.Errorf("a company should carry %s: %s", want, body)
		}
	}

	code, body = asKey(t, "GET", base+"due", secret, nil)
	if code != http.StatusOK {
		t.Fatalf("due: %d %s", code, body)
	}
	sameJSON(t, "what is due", body, must(s.AgentDue(o, paging.Page{})))
}
