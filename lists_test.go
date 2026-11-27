package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func seedCompanies(t *testing.T, s *Store, org int64, n int, stage, segment string) []Company {
	t.Helper()
	rows := make([]Company, n)
	for i := range rows {
		rows[i] = Company{OrgID: org, Name: fmt.Sprintf("Maker %03d", i+1), Stage: stage, Segment: segment}
	}
	if err := s.db.CreateInBatches(&rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func seedContact(t *testing.T, s *Store, c Contact) Contact {
	t.Helper()
	c.File = "test"
	if err := s.db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCompaniesArePaged(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	seedCompanies(t, s, o.ID, 620, CompanyQualified, "Yachts & boats")

	rows, pg, err := s.Companies(o.ID, "", "", "", 1)
	if err != nil || len(rows) != companiesPageSize || pg.Total != 620 || rows[0].Name != "Maker 001" {
		t.Fatalf("first page: %d rows, %+v, %v", len(rows), pg, err)
	}
	rows, pg, _ = s.Companies(o.ID, "", "", "", pg.Pages())
	if len(rows) != 20 || rows[len(rows)-1].Name != "Maker 620" {
		t.Fatalf("the last company, past the old 500 cap, isn't on the last page: %d rows, %+v", len(rows), pg)
	}
	if _, pg, _ = s.Companies(o.ID, "", "", "", 999); pg.N != pg.Pages() {
		t.Fatalf("a page past the end should show the last one, got %+v", pg)
	}
	if _, pg, _ = s.Companies(o.ID, "", "", "", -3); pg.N != 1 {
		t.Fatalf("a page before the start should show the first, got %+v", pg)
	}

	// The Companies page walks the list 50 at a time by its cursors, and counts the pages from the total.
	list := srv.URL + "/o/alpha/api/v1/companies?segment=Yachts+%26+boats&limit=50"
	var first, second struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
		Total      int    `json:"total"`
	}
	_, body := call(t, client, "GET", list, "", nil)
	if json.Unmarshal([]byte(body), &first) != nil || first.Total != 620 || len(first.Items) != 50 || first.Items[0].Name != "Maker 001" || first.NextCursor == "" {
		t.Fatalf("the segment's first page: %s", body)
	}
	_, body = call(t, client, "GET", list+"&cursor="+url.QueryEscape(first.NextCursor), "", nil)
	if json.Unmarshal([]byte(body), &second) != nil || second.Total != 620 || len(second.Items) != 50 || second.Items[0].Name != "Maker 051" || second.NextCursor == "" {
		t.Errorf("the segment's second page: %s", body)
	}
	if code, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/companies?cursor=nonsense", "", nil); code != http.StatusBadRequest {
		t.Errorf("a cursor that isn't one: %d %s", code, body)
	}
}

func TestPipelineInSQL(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	day := str("2026-01-05")
	people := map[string][]Contact{
		"Nobody yet":         nil,
		"Untouched":          {{}},
		"Turned down":        nil,
		"Turned down, later": {{LinkedinAdded: day}},
		"Added":              {{LinkedinAdded: day}},
		"Emailed":            {{EmailSent: day}, {}},
		"Called":             {{Called: day}},
		"Replied":            {{Replied: day}, {LinkedinAdded: day}},
		"Meeting":            {{Replied: day, Stage: "meeting"}, {Replied: day, Stage: "lost"}},
		"Proposal":           {{Replied: day, Stage: "proposal"}, {Replied: day, Stage: "meeting"}},
		"Won":                {{Replied: day, Stage: "won"}, {Replied: day, Stage: "proposal"}},
		"Lost":               {{Replied: day, Stage: "lost"}, {}},
		"Lost one, one live": {{Replied: day, Stage: "lost"}, {EmailSent: day}},
		// Someone who declined before anyone reached them was never in the pipeline.
		"Declined unreached":     {{Declined: day}},
		"Declined after a touch": {{Declined: day, EmailSent: day}},
		"Declined after a reply": {{Declined: day, Replied: day, Stage: "meeting"}},
		"Declined, one in talks": {{Declined: day, EmailSent: day}, {Replied: day, Stage: "proposal"}},
	}
	want := map[string]string{"Nobody yet": "research", "Untouched": "research", "Turned down": "rejected",
		"Turned down, later": "outreach", "Added": "outreach", "Emailed": "outreach", "Called": "outreach",
		"Replied": "replied", "Meeting": "meeting", "Proposal": "proposal", "Won": "won", "Lost": "lost",
		"Lost one, one live": "outreach", "Declined unreached": "research", "Declined after a touch": "outreach",
		"Declined after a reply": "outreach", "Declined, one in talks": "proposal"}
	for name, list := range people {
		c := Company{OrgID: o.ID, Name: name, Stage: CompanyQualified}
		if strings.HasPrefix(name, "Turned down") {
			c.Stage = CompanyRejected
		}
		if err := s.db.Create(&c).Error; err != nil {
			t.Fatal(err)
		}
		for i, p := range list {
			p.OrgID, p.CompanyID, p.Key = o.ID, &c.ID, fmt.Sprintf("%s %d", name, i)
			seedContact(t, s, p)
		}
		// A company-level row isn't a person, and mustn't count as one or move the stage.
		seedContact(t, s, Contact{OrgID: o.ID, CompanyID: &c.ID, Key: "org:" + name, Replied: day, Stage: "won"})
	}

	rows, pg, err := s.Companies(o.ID, "", "", "", 1)
	if err != nil || pg.Total != len(people) {
		t.Fatalf("%+v %v", pg, err)
	}
	for _, r := range rows {
		listed, _ := s.People(o.ID, r.ID)
		if r.Pipeline != want[r.Name] || r.Pipeline != pipeline(r.Company, listed) || r.People != len(people[r.Name]) {
			t.Errorf("%s: list says %s with %d people, its own page says %s, want %s with %d",
				r.Name, r.Pipeline, r.People, pipeline(r.Company, listed), want[r.Name], len(people[r.Name]))
		}
	}
	for _, stage := range pipelineStages {
		n := 0
		for _, st := range want {
			if st == stage {
				n++
			}
		}
		rows, pg, err := s.Companies(o.ID, "", "", stage, 1)
		if err != nil || len(rows) != n || pg.Total != n {
			t.Errorf("stage %s: %d rows, total %d, want %d (%v)", stage, len(rows), pg.Total, n, err)
		}
		for _, r := range rows {
			if r.Pipeline != stage {
				t.Errorf("stage %s lists %s, which is at %s", stage, r.Name, r.Pipeline)
			}
		}
	}
	if rows, _, _ := s.Companies(o.ID, "", "", "nonsense", 1); len(rows) != 0 {
		t.Fatal("an unknown stage matched companies")
	}
}

func TestStageFilterPagesInSQL(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	// Every tenth company is in outreach, so the filter's matches are spread far past one page.
	for i, c := range seedCompanies(t, s, o.ID, 700, CompanyQualified, "") {
		if i%10 == 9 {
			seedContact(t, s, Contact{OrgID: o.ID, CompanyID: &c.ID, Key: c.Name, LinkedinAdded: str("2026-01-05")})
		}
	}
	rows, pg, err := s.Companies(o.ID, "", "", "outreach", 2)
	if err != nil || pg.Total != 70 || len(rows) != 20 || rows[len(rows)-1].Name != "Maker 700" {
		t.Fatalf("outreach, page 2: %d rows, %+v, %v", len(rows), pg, err)
	}
	if _, pg, _ := s.Companies(o.ID, "", "", "research", 1); pg.Total != 630 {
		t.Fatalf("research: %+v", pg)
	}
}

func TestResearchIsPaged(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	seeded := seedCompanies(t, s, o.ID, 230, CompanyReview, "")
	for _, i := range []int{0, 229} {
		c := seeded[i]
		for _, field := range []string{"platform", "team_page"} {
			if err := s.db.Create(&Fact{OrgID: o.ID, CompanyID: c.ID, Field: field, Value: c.Name, SourceURL: "https://maker.example"}).Error; err != nil {
				t.Fatal(err)
			}
		}
		seedContact(t, s, Contact{OrgID: o.ID, CompanyID: &c.ID, Key: c.Name, Person: str("Person at " + c.Name), Status: ContactSuggested})
	}
	if n := s.ResearchCount(o.ID, "review"); n != 230 {
		t.Fatalf("the count is the whole list, not a page of it: %d", n)
	}
	if counts := s.ResearchCounts(o.ID); counts["review"] != 230 || counts["rejected"] != 0 {
		t.Fatalf("%+v", counts)
	}
	// Newest first, so the first company seeded is the last row of the last page.
	rows, pg, err := s.Research(o.ID, "review", 1)
	if err != nil || len(rows) != researchPageSize || pg.Pages() != 8 || rows[0].Name != "Maker 230" {
		t.Fatalf("first page: %d rows, %+v, %v", len(rows), pg, err)
	}
	if len(rows[0].Facts) != 2 || rows[0].Facts[0].Value != "Maker 230" || len(rows[0].People) != 1 || len(rows[1].Facts) != 0 {
		t.Fatalf("facts and people landed on the wrong company: %+v", rows[0])
	}
	rows, pg, _ = s.Research(o.ID, "review", 99)
	last := rows[len(rows)-1]
	if pg.N != 8 || len(rows) != 20 || last.Name != "Maker 001" || len(last.Facts) != 2 || deref(last.People[0].Person) != "Person at Maker 001" {
		t.Fatalf("the last page, past the old 200 cap: %d rows, %+v, %+v", len(rows), pg, last)
	}

	queue := readResearch(t, client, srv.URL+"/o/alpha/api/v1/research?filter=review")
	var more struct {
		NextCursor string `json:"next_cursor"`
	}
	json.Unmarshal([]byte(queue.body), &more)
	if len(queue.Items) == 0 || queue.Items[0].Name != "Maker 230" || more.NextCursor == "" || !strings.Contains(queue.body, `"review":230`) {
		t.Fatalf("research's first page should count all 230 and lead on to the rest: %d items", len(queue.Items))
	}
	next := readResearch(t, client, srv.URL+"/o/alpha/api/v1/research?filter=review&cursor="+url.QueryEscape(more.NextCursor))
	if len(next.Items) == 0 || next.Items[0].Name != fmt.Sprintf("Maker %03d", 230-len(queue.Items)) {
		t.Errorf("research's second page starts at %v", next.names())
	}
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/today/summary", "", nil); !strings.Contains(body, `"review":230`) {
		t.Errorf("Today doesn't count every company waiting for review: %s", body)
	}
}

func names(rows []CompanyListRow) string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return strings.Join(out, ", ")
}

func TestSearch(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	a, b := mustOrg(t, s, "alpha"), mustOrg(t, s, "beta")
	add := func(o *Org, c Company, people ...Contact) Company {
		c.OrgID, c.Stage = o.ID, CompanyQualified
		if err := s.db.Create(&c).Error; err != nil {
			t.Fatal(err)
		}
		for _, p := range people {
			p.OrgID, p.CompanyID, p.Key, p.Maker = o.ID, &c.ID, deref(p.Person), &c.Name
			seedContact(t, s, p)
		}
		return c
	}
	lark := add(a, Company{Name: "Larkmoor Yachts", Domain: "larkmoor.example", Why: `Sells "made to order" hulls`},
		Contact{Person: str("Hannah Pryce, Head of Marketing"), Email: str("hannah@larkmoor.example")})
	add(a, Company{Name: "Fenwick Kitchens", Domain: "fenwick.example", Why: "Configurator is 100% custom_built"},
		Contact{Person: str("Björn Ødegård"), Email: str("bjorn@fenwick.example")})
	add(b, Company{Name: "Larkmoor Yachts", Domain: "larkmoor.example"}, Contact{Person: str("Hannah Pryce")})

	search := func(q string) string {
		t.Helper()
		rows, _, err := s.Companies(a.ID, q, "", "", 1)
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		return names(rows)
	}
	for q, want := range map[string]string{
		"RKMOOR":           "Larkmoor Yachts",                   // inside a word, any case
		"moor y":           "Larkmoor Yachts",                   // across a space
		"fenwick.example":  "Fenwick Kitchens",                  // domain, and a person's email
		"example":          "Fenwick Kitchens, Larkmoor Yachts", // both
		"to order":         "Larkmoor Yachts",                   // why
		`"made to order"`:  "Larkmoor Yachts",                   // quotes are text, not syntax
		"hannah pr":        "Larkmoor Yachts",                   // a person's name
		"björn":            "Fenwick Kitchens",
		"ya":               "Larkmoor Yachts", // too short for the index
		"é":                "",
		"kitchens OR moor": "",
		"lark*":            "",
		"name:lark":        "",
		"":                 "Fenwick Kitchens, Larkmoor Yachts",
	} {
		if got := search(q); got != want {
			t.Errorf("search %q found %q, want %q", q, got, want)
		}
	}
	// The companies list matches a person by name or email, never by the company they're filed under.
	if hits := mustSearch(t, s, a.ID, "kitchens"); len(hits) != 2 {
		t.Errorf("agent search for a company should find it and its person, got %+v", hits)
	}
	if hits := mustSearch(t, s, a.ID, "pryce"); len(hits) != 1 || hits[0].Kind != "contact" {
		t.Errorf("agent search for a person: %+v", hits)
	}
	if hits := mustSearch(t, s, b.ID, "larkmoor"); len(hits) != 2 {
		t.Errorf("beta's search should see only its own company and person, got %+v", hits)
	}

	// The index follows edits and removals made by any route.
	if err := s.db.Model(&Company{}).Where("id = ?", lark.ID).Updates(map[string]any{"name": "Windrush Marine"}).Error; err != nil {
		t.Fatal(err)
	}
	p, _ := s.FindOne(a.ID, "Hannah")
	if err := s.Update(a.ID, p.ID, "person", "Imogen Vale", "test"); err != nil {
		t.Fatal(err)
	}
	if search("windrush") != "Windrush Marine" || search("imogen") != "Windrush Marine" {
		t.Error("search didn't pick up a rename")
	}
	if search("larkmoor yachts") != "" || search("hannah pryce") != "" {
		t.Error("search still finds the old names")
	}
	if err := s.db.Where("org_id = ?", a.ID).Delete(&Contact{}).Error; err != nil {
		t.Fatal(err)
	}
	if search("imogen") != "" || len(mustSearch(t, s, a.ID, "imogen")) != 0 {
		t.Error("search still finds a deleted person")
	}
}

// A database from before the index existed gets one, filled from the rows already there.
func TestSearchIndexIsBuiltForExistingRows(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	if _, err := s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts"}, "test"); err != nil {
		t.Fatal(err)
	}
	indexes := []string{"idx_companies_search", "idx_contacts_person_search", "idx_contacts_search"}
	for _, name := range indexes {
		if err := s.db.Exec("drop index " + name).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.QuickAddPerson(o, QuickAdd{Name: "Imogen Vale", Company: "Windrush Marine"}, "test"); err != nil {
		t.Fatal(err)
	}
	s.indexSearch()
	var built int64
	if err := s.db.Raw("select count(*) from pg_indexes where schemaname = current_schema() and indexname in ?", indexes).Scan(&built).Error; err != nil || built != int64(len(indexes)) {
		t.Fatalf("%d of the %d search indexes were rebuilt: %v", built, len(indexes), err)
	}
	for _, q := range []string{"larkmoor", "pryce", "windrush", "imogen"} {
		if rows, _, err := s.Companies(o.ID, q, "", "", 1); err != nil || len(rows) != 1 {
			t.Errorf("search %q after the index was built: %d rows, %v", q, len(rows), err)
		}
	}
}
