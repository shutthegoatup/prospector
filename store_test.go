package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// tempStore is a fresh SQLite file, or with PROSPECTOR_TEST_PG an emptied Postgres database.
func tempStore(t *testing.T) *Store {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "t.sqlite")
	if pg := os.Getenv("PROSPECTOR_TEST_PG"); pg != "" {
		dsn = pg
		s, err := Open(pg)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.db.Migrator().DropTable(models...); err != nil {
			t.Fatal(err)
		}
		closeStore(s)
	}
	s, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if s.keys, err = parseKeys("1:" + testKey); err != nil {
		t.Fatal(err)
	}
	// Postgres allows only so many connections, and every test opens its own.
	t.Cleanup(func() { closeStore(s) })
	return s
}

var testKey = strings.Repeat("0a", 32)

func closeStore(s *Store) {
	if db, err := s.db.DB(); err == nil {
		db.Close()
	}
}

func mustOrg(t *testing.T, s *Store, slug string) *Org {
	t.Helper()
	o, err := s.CreateOrg(slug, strings.ToUpper(slug[:1])+slug[1:])
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// readFixtures parses testdata/, invented companies and people only.
func readFixtures(t *testing.T) *ImportPayload {
	t.Helper()
	p, err := ReadRepo("testdata", "example.co")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFixturesParse(t *testing.T) {
	p := readFixtures(t)
	byKey := map[string]Draft{}
	for _, d := range p.Drafts {
		byKey[d.Key] = d
	}
	hannah, jonas := byKey["Hannah Pryce"], byKey["Jonas Weber"]
	if hannah.Email != "hannah.pryce@larkmoor.example" || hannah.Subject != "Your Day Cruiser 45 configurator" || !strings.Contains(hannah.Note, "\n\nI work on") {
		t.Fatalf("numbered draft parsed wrong: %+v", hannah)
	}
	if jonas.Fit != "A" || jonas.Email != "jonas.weber@ostrel.example" || jonas.Followup == "" || jonas.Phone == "" {
		t.Fatalf("sectioned draft parsed wrong: %+v", jonas)
	}
	if len(p.Companies) != 4 || len(p.SentLog) != 1 {
		t.Fatalf("got %d companies and %d sent-log lines", len(p.Companies), len(p.SentLog))
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "acme")
	if _, err := s.Import(o.ID, readFixtures(t)); err != nil {
		t.Fatal(err)
	}
	want, err := s.SnapshotText(o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(want, "insert into contacts") != 4 {
		t.Fatalf("expected 2 people and 2 companies without one:\n%s", want)
	}
	s2 := tempStore(t)
	o2 := mustOrg(t, s2, "acme")
	if _, err := s2.Restore(o2.ID, want); err != nil {
		t.Fatal(err)
	}
	got, err := s2.SnapshotText(o2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("exported snapshot differs from the one restored")
	}
	s, o = s2, o2
	var linked int64
	s.in(o.ID).Model(&Contact{}).Where("company_id is not null").Count(&linked)
	if linked == 0 {
		t.Fatal("restore linked no contacts to companies")
	}
}

func TestImportLegacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outreach.sqlite")
	old, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	old.db.Exec("drop table contacts")
	old.db.Exec(snapshotSchema)
	old.db.Exec("alter table contacts add column accepted text")
	old.db.Exec("insert into contacts (id, key, file, maker, person, first, linkedin_added, accepted) values (7, 'Ada Lovelace', 'x.md', 'Analytical Engines (UK)', 'Ada Lovelace, Countess', 'Ada', '2026-11-20', '2026-11-22')")
	s := tempStore(t)
	o := mustOrg(t, s, "acme")
	n, err := s.ImportLegacy(o.ID, path)
	if err != nil || n != 1 {
		t.Fatalf("imported %d: %v", n, err)
	}
	var c Contact
	s.in(o.ID).First(&c, 7)
	if c.Key != "Ada Lovelace" || deref(c.Accepted) != "2026-11-22" || c.CompanyID == nil {
		t.Fatalf("legacy row not carried over: %+v", c)
	}
}

func TestImportKeepsRecordedDates(t *testing.T) {
	p := readFixtures(t)
	s := tempStore(t)
	o := mustOrg(t, s, "acme")
	if _, err := s.Import(o.ID, p); err != nil {
		t.Fatal(err)
	}
	c, err := s.FindOne(o.ID, p.Drafts[len(p.Drafts)-1].Person)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Mark(o.ID, c.ID, "email", "2026-01-02", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Import(o.ID, p); err != nil {
		t.Fatal(err)
	}
	var after Contact
	s.db.First(&after, c.ID)
	if deref(after.EmailSent) != "2026-01-02" || after.CompanyID == nil {
		t.Fatalf("re-import lost email_sent or the company link: %+v", after)
	}
}

func TestOrganisationsAreIsolated(t *testing.T) {
	s := tempStore(t)
	a, b := mustOrg(t, s, "alpha"), mustOrg(t, s, "beta")
	if _, err := s.QuickAddPerson(a, QuickAdd{Name: "Ada Lovelace", Company: "Analytical Engines"}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.QuickAddPerson(b, QuickAdd{Name: "Ada Lovelace", Company: "Analytical Engines"}, "test"); err != nil {
		t.Fatalf("the same person in a second org: %v", err)
	}
	ca, _ := s.FindOne(a.ID, "Ada")
	if err := s.Mark(b.ID, ca.ID, "linkedin", today(), "test"); err == nil {
		t.Fatal("org beta marked org alpha's contact")
	}
	if err := s.Update(b.ID, ca.ID, "email", "x@y.z", "test"); err == nil {
		t.Fatal("org beta edited org alpha's contact")
	}
	if hits, _, _ := s.AgentSearch(b.ID, Actor{}, "analytical", PageIn{}); len(hits) != 2 {
		t.Fatalf("beta's search should see only its own company and person, got %+v", hits)
	}
	for _, h := range mustSearch(t, s, b.ID, "ada") {
		if h.Kind == "contact" && h.ID == ca.ID {
			t.Fatal("beta's search returned alpha's contact")
		}
	}
}

func mustSearch(t *testing.T, s *Store, org int64, q string) []SearchHit {
	hits, _, err := s.AgentSearch(org, Actor{}, q, PageIn{})
	if err != nil {
		t.Fatal(err)
	}
	return hits
}

func TestAgentResearchToOutreach(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	o.Settings.NeverContact = "blocked.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AgentAddCompany(o.ID, Actor{Name: "agent:bot"}, AddCompanyIn{Name: "Ostrel Aviation", Facts: []FactIn{{Field: "configurator", Value: "3D"}}}); err == nil {
		t.Fatal("a fact without a source was accepted")
	}
	out, err := s.AgentAddCompany(o.ID, Actor{Name: "agent:bot"}, AddCompanyIn{Name: "Ostrel Aviation", Country: "CH",
		Facts: []FactIn{{Field: "configurator", Value: "3D", SourceURL: "https://ostrel.example/configure"}}})
	if err != nil || out.Existed || out.Stage != CompanyReview {
		t.Fatalf("add company: %+v %v", out, err)
	}
	if _, err := s.AgentAddContact(o, Actor{Name: "agent:bot"}, AddContactIn{Company: "Ostrel Aviation", Name: "Jonas Weber"}); err == nil {
		t.Fatal("a contact without a source was accepted")
	}
	if _, err := s.AgentAddContact(o, Actor{Name: "agent:bot"}, AddContactIn{Company: "Ostrel Aviation", Name: "Max Muster", Email: "max@blocked.example", SourceURL: "https://x.example"}); err == nil {
		t.Fatal("never-contact list ignored")
	}
	c, err := s.AgentAddContact(o, Actor{Name: "agent:bot"}, AddContactIn{Company: "ostrel aviation", Name: "Jonas Weber", Title: "Head of Digital", SourceURL: "https://ostrel.example/team"})
	if err != nil || c.Status != ContactSuggested {
		t.Fatalf("add contact: %+v %v", c, err)
	}
	if due, _ := s.Due(o.ID, "linkedin", time.Now(), o.Settings); len(due) != 0 {
		t.Fatal("a suggested contact reached the LinkedIn queue before anyone picked them")
	}
	if rows, _, _ := s.Research(o.ID, "review", 1); len(rows) != 1 || len(rows[0].Facts) != 1 || len(rows[0].People) != 1 {
		t.Fatalf("research review: %+v", rows)
	}
	if err := s.PickContact(o.ID, o, c.ID, "ada"); err != nil {
		t.Fatal(err)
	}
	due, _ := s.Due(o.ID, "linkedin", time.Now(), o.Settings)
	if len(due) != 1 || !strings.Contains(deref(due[0].Note), "Jonas") {
		t.Fatalf("picked contact not queued with a note: %+v", due)
	}
	if err := s.AgentDraft(o.ID, Actor{Name: "agent:bot"}, DraftIn{ContactID: c.ID, Kind: "note", Text: strings.Repeat("x", 301)}); err == nil {
		t.Fatal("a note over 300 characters was accepted")
	}
	corvane, err := s.AgentAddCompany(o.ID, Actor{Name: "agent:bot"}, AddCompanyIn{Name: "Corvane", Facts: []FactIn{{Field: "configurator", Value: "Vela builder", SourceURL: "https://corvane.example"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetCompanyStage(o.ID, corvane.ID, CompanyRejected, "", "ada"); err != nil {
		t.Fatal(err)
	}
	scores := s.AgentScores(o.ID)
	if len(scores) != 1 || scores[0] != (AgentScore{Agent: "bot", Companies: 2, Qualified: 1, Rejected: 1, Contacts: 1, Picked: 1}) {
		t.Fatalf("agent scores: %+v", scores)
	}
}

func TestParseLinkedIn(t *testing.T) {
	name, role, company := ParseLinkedIn("(3) Hannah Pryce | LinkedIn", "Head of Marketing at Larkmoor Yachts | Boats")
	if name != "Hannah Pryce" || role != "Head of Marketing" || company != "Larkmoor Yachts" {
		t.Fatalf("got %q %q %q", name, role, company)
	}
}

// signedIn returns a client logged in to a test server with one org.
func signedIn(t *testing.T) (*Store, *Org, *httptest.Server, *http.Client) {
	t.Helper()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	u, err := s.CreateUser("ada@example.com", "Ada Lovelace", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddMember(o.ID, u.ID, "owner", u.Name); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer((&App{s: s}).Routes())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	res, err := client.PostForm(srv.URL+"/login", url.Values{"email": {"ada@example.com"}, "password": {"correct horse battery"}})
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("login: %v %v", res.Status, err)
	}
	return s, o, srv, client
}

func call(t *testing.T, client *http.Client, method, u, ctype string, body any) (int, string) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, u, r)
	if ctype != "" {
		req.Header.Set("content-type", ctype)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	text, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(text)
}

func TestUI(t *testing.T) {
	s, o, srv, client := signedIn(t)
	base := srv.URL + "/o/alpha/"
	for _, page := range []string{"today", "research", "conversations", "companies", "add", "settings", "agents", "tasks", "today?step=linkedin"} {
		if code, body := call(t, client, "GET", base+page, "", nil); code != http.StatusOK || !strings.Contains(body, `<div id="app"></div>`) {
			t.Fatalf("GET %s: %d", page, code)
		}
	}
	if code, body := call(t, client, "GET", base+"api/v1/nav", "", nil); code != http.StatusOK || !strings.Contains(body, `"name":"Alpha"`) {
		t.Fatalf("the sidebar's organisation: %d %s", code, body)
	}
	if code, _ := call(t, http.DefaultClient, "GET", base+"today", "", nil); code != http.StatusOK {
		t.Fatalf("signed-out GET should land on the login page, got %d", code)
	}
	if code, _ := call(t, client, "GET", srv.URL+"/o/nope/today", "", nil); code != http.StatusNotFound {
		t.Fatalf("an org you don't belong to: %d", code)
	}
	if code, _ := call(t, client, "POST", base+"api/v1/people", "text/plain", QuickAdd{Name: "X"}); code != http.StatusUnsupportedMediaType {
		t.Fatalf("non-JSON POST got %d", code)
	}
	code, body := call(t, client, "POST", base+"api/v1/people", "application/json",
		QuickAdd{Linkedin: "https://www.linkedin.com/in/hannah-pryce", Name: "Hannah Pryce", Title: "Head of Marketing", Company: "Larkmoor Yachts"})
	if code != http.StatusCreated || !strings.Contains(body, "LinkedIn queue") {
		t.Fatalf("quick add: %d %s", code, body)
	}
	if _, body := call(t, client, "GET", base+"api/v1/today?step=linkedin", "", nil); !strings.Contains(body, "Hannah Pryce") || !strings.Contains(body, `"open_url":"https://www.linkedin.com/in/hannah-pryce"`) || !strings.Contains(body, `"open_label":"Open + copy"`) {
		t.Fatalf("quick-added person missing from the LinkedIn queue: %s", body)
	}
	c, _ := s.FindOne(o.ID, "Hannah")
	if err := s.Update(o.ID, c.ID, "linkedin", "javascript:alert(1)", "test"); err != nil {
		t.Fatal(err)
	}
	_, body = call(t, client, "GET", base+"api/v1/today?step=linkedin", "", nil)
	if !strings.Contains(body, `"open_url":"https://www.linkedin.com/search/results/people/?keywords=Hannah%20Pryce%20Larkmoor%20Yachts"`) || !strings.Contains(body, `"open_label":"Search + copy"`) || strings.Contains(body, "Open + copy") {
		t.Fatalf("someone without a usable profile link should open a people search: %s", body)
	}
	if strings.Contains(body, `"open_url":"javascript:`) {
		t.Fatal("Today's card opens a javascript: link")
	}
	if _, err := s.AgentAddContact(o, Actor{Name: "agent:x"}, AddContactIn{Company: "Larkmoor Yachts", Name: "Eve", Linkedin: "javascript:alert(1)", SourceURL: "https://x.example"}); err == nil {
		t.Fatal("agent stored a javascript: link")
	}
	if code, _ := call(t, client, "POST", base+"api/v1/marks", "application/json", map[string]any{"contact_id": c.ID, "step": "replied"}); code != http.StatusOK {
		t.Fatal("mark replied")
	}
	if _, body := call(t, client, "GET", base+"api/v1/conversations", "", nil); !strings.Contains(body, "Hannah Pryce") || !strings.Contains(body, `"next_step":""`) {
		t.Fatalf("a reply without a next step isn't flagged: %s", body)
	}
	code, body = call(t, client, "POST", base+"api/v1/keys", "application/json", map[string]any{"name": "bot", "scopes": []string{"read"}})
	if code != http.StatusCreated || !strings.Contains(body, "pk_") {
		t.Fatalf("create key: %d %s", code, body)
	}
}

func TestAPIKeys(t *testing.T) {
	s, o, srv, _ := signedIn(t)
	other := mustOrg(t, s, "beta")
	readOnly, _, _ := s.CreateKey(o.ID, "reader", []string{"read"}, "test")
	writer, _, _ := s.CreateKey(o.ID, "bot", []string{"read", "research:write"}, "test")
	betaKey, _, _ := s.CreateKey(other.ID, "beta-bot", []string{"read", "research:write"}, "test")
	api := srv.URL + "/o/alpha/api/v1/"
	keyed := func(key string) *http.Client {
		return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			r.Header.Set("Authorization", "Bearer "+key)
			return http.DefaultTransport.RoundTrip(r)
		})}
	}
	company := AddCompanyIn{Name: "Fjordline Saunas", Facts: []FactIn{{Field: "configurator", Value: "Kickflip", SourceURL: "https://fjordline.example"}}}
	if code, _ := call(t, http.DefaultClient, "POST", api+"companies", "application/json", company); code != http.StatusUnauthorized {
		t.Fatalf("no key: %d", code)
	}
	if code, _ := call(t, keyed(readOnly), "POST", api+"companies", "application/json", company); code != http.StatusForbidden {
		t.Fatalf("read-only key wrote: %d", code)
	}
	if code, _ := call(t, keyed(betaKey), "GET", api+"due", "", nil); code != http.StatusForbidden {
		t.Fatalf("another org's key read alpha: %d", code)
	}
	if code, body := call(t, keyed(writer), "POST", api+"companies", "application/json", company); code != http.StatusOK || !strings.Contains(body, `"stage":"review"`) {
		t.Fatalf("add company: %d %s", code, body)
	}
	if code, body := call(t, keyed(readOnly), "GET", api+"search?q=fjord", "", nil); code != http.StatusOK || !strings.Contains(body, "Fjordline") {
		t.Fatalf("search: %d %s", code, body)
	}
	var keys []APIKey
	s.in(o.ID).Find(&keys)
	if err := s.RevokeKey(o.ID, keys[1].ID); err != nil {
		t.Fatal(err)
	}
	if code, _ := call(t, keyed(writer), "GET", api+"due", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("revoked key still works: %d", code)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMCP(t *testing.T) {
	s, o, srv, _ := signedIn(t)
	o.Settings.AgentGuidance = "Never suggest anyone at Brandtech."
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	key, _, _ := s.CreateKey(o.ID, "claude", []string{"read", "research:write"}, "test")
	transport := &mcp.StreamableClientTransport{Endpoint: srv.URL + "/o/alpha/mcp", HTTPClient: &http.Client{
		Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			r.Header.Set("Authorization", "Bearer "+key)
			return http.DefaultTransport.RoundTrip(r)
		})}}
	ctx := context.Background()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if strings.Contains(strings.Join(names, " "), "draft_message") || strings.Contains(strings.Join(names, " "), " mark") || !strings.Contains(strings.Join(names, " "), "add_company") {
		t.Fatalf("tools don't follow the key's scopes: %v", names)
	}
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "add_company", Arguments: map[string]any{
		"name": "Larkmoor Yachts", "facts": []map[string]string{{"field": "configurator", "value": "Day Cruiser 45 builder", "source_url": "https://larkmoor.example/build"}}}})
	if err != nil || res.IsError {
		t.Fatalf("add_company: %v %+v", err, res)
	}
	fact := map[string]any{"company": "Larkmoor Yachts", "field": "configurator", "value": "Day Cruiser 45 builder", "source_url": "https://larkmoor.example/build"}
	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "add_fact", Arguments: fact})
	if err != nil || res.IsError || !strings.Contains(fmt.Sprint(res.StructuredContent), "existed:true") {
		t.Fatalf("a fact sent twice should say it existed: %v %+v", err, res)
	}
	var keyed int64
	if s.in(o.ID).Model(&Event{}).Where("key_id > 0").Count(&keyed); keyed != 1 {
		t.Fatalf("want the one change recorded against its key, got %d events", keyed)
	}
	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "get_company", Arguments: map[string]any{"company": "Larkmoor Yachts"}})
	if seen := fmt.Sprint(res.StructuredContent); err != nil || res.IsError || !strings.Contains(seen, "by_you:true") || !strings.Contains(seen, "action:add:company") {
		t.Fatalf("get_company should show the key its own earlier work: %v %+v", err, res)
	}
	res, _ = session.CallTool(ctx, &mcp.CallToolParams{Name: "add_contact", Arguments: map[string]any{"company": "Larkmoor Yachts", "name": "Hannah Pryce"}})
	if res == nil || !res.IsError {
		t.Fatal("add_contact without a source should be a tool error")
	}
	init := session.InitializeResult()
	if !strings.Contains(init.Instructions, "Contacts need care") || strings.Contains(init.Instructions, "Drafts need") ||
		!strings.Contains(init.Instructions, "Never suggest anyone at Brandtech.") {
		t.Fatalf("instructions don't follow the key's scopes: %q", init.Instructions)
	}
	g, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "guidance", Arguments: map[string]any{}})
	if err != nil || g.IsError || !strings.Contains(fmt.Sprint(g.StructuredContent), "Never suggest anyone at Brandtech.") {
		t.Fatalf("guidance tool: %v %+v", err, g)
	}
	if rows, _, _ := s.Research(o.ID, "review", 1); len(rows) != 1 || rows[0].FoundBy != "agent:claude" {
		t.Fatalf("MCP-added company not waiting in Research: %+v", rows)
	}
}

func TestAgentMarks(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	if _, err := s.AgentAddCompany(o.ID, Actor{Name: "agent:bot"}, AddCompanyIn{Name: "Larkmoor Yachts",
		Facts: []FactIn{{Field: "configurator", Value: "Day Cruiser 45", SourceURL: "https://larkmoor.example"}}}); err != nil {
		t.Fatal(err)
	}
	c, err := s.AgentAddContact(o, Actor{Name: "agent:bot"}, AddContactIn{Company: "Larkmoor Yachts", Name: "Hannah Pryce", SourceURL: "https://larkmoor.example/team"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AgentMark(o.ID, Actor{Name: "agent:bot"}, MarkIn{ContactID: c.ID, Step: "declined"}); err == nil {
		t.Fatal("an agent marked a contact nobody has picked yet")
	}
	if err := s.PickContact(o.ID, o, c.ID, "ada"); err != nil {
		t.Fatal(err)
	}
	if err := s.AgentMark(o.ID, Actor{Name: "agent:bot"}, MarkIn{ContactID: c.ID, Step: "accepted", Date: "2999-01-01"}); err == nil {
		t.Fatal("a future date was accepted")
	}
	if err := s.AgentMark(o.ID, Actor{Name: "agent:bot"}, MarkIn{ContactID: c.ID, Step: "sent"}); err == nil {
		t.Fatal("an unknown step was accepted")
	}
	if err := s.AgentMark(o.ID, Actor{Name: "agent:bot"}, MarkIn{ContactID: c.ID, Step: "accepted", Note: "seen on LinkedIn"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AgentMark(o.ID, Actor{Name: "agent:bot"}, MarkIn{ContactID: c.ID, Step: "declined", Date: "2026-11-24", Note: "Allan says no"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Mark(o.ID, c.ID, "accepted", today(), "ada"); err != nil {
		t.Fatal(err)
	}
	if got := s.AgentMarks(o.ID, []int64{c.ID})[c.ID]; len(got) != 1 || got["declined"] != "agent:bot" {
		t.Fatalf("agent marks should keep the agent's decline and drop the accept Ada re-marked: %v", got)
	}
	var h []HistoryItem
	for _, it := range must(firstOf(s.History(o.ID, must(s.AgentCompany(o.ID, Actor{}, "Larkmoor Yachts", PageIn{})).Company.ID, 1))) {
		if it.Action == "mark:declined" {
			h = append(h, it)
		}
	}
	if len(h) != 1 || h[0].Actor != "agent:bot" || historyText(h[0]) != "marked not interested · Hannah Pryce: Allan says no" {
		t.Fatalf("decline history: %+v %q", h, historyText(h[0]))
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
