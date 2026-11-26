package main

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	countryInName = regexp.MustCompile(`\s*\(.*\)\s*$`)
	httpURL       = regexp.MustCompile(`^https?://\S+$`)
)

// companyName strips the "(UK, group)" tags the drafts put after maker names.
func companyName(maker string) string {
	return strings.TrimSpace(countryInName.ReplaceAllString(maker, ""))
}

func domainOf(u string) string {
	p, err := url.Parse(strings.TrimSpace(u))
	if err != nil || p.Host == "" {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(p.Host), "www.")
}

// ensureCompany finds a company by name (case-insensitive) or creates it.
func ensureCompany(tx *gorm.DB, org int64, c Company) (*Company, bool, error) {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		return nil, false, errors.New("company name is required")
	}
	if err := holdOrg(tx, org); err != nil {
		return nil, false, err
	}
	var found Company
	err := tx.Where("org_id = ? and lower(name) = ?", org, strings.ToLower(c.Name)).First(&found).Error
	if err == nil {
		return &found, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	c.OrgID = org
	if c.Stage == "" {
		c.Stage = CompanyReview
	}
	res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&c)
	if res.Error != nil {
		return nil, false, res.Error
	}
	if res.RowsAffected == 0 {
		// The index found one the look-up didn't: the two fold capitals differently outside ASCII.
		err := tx.Where("org_id = ? and (name = ? or lower(name) = lower(?))", org, c.Name, c.Name).First(&found).Error
		return &found, false, err
	}
	return &c, true, nil
}

// companyAt finds the company an agent means by its website when the name is new,
// so one company found under two spellings stays one row. A site that several
// companies share (a group's brands, a marketplace) identifies none of them.
func companyAt(tx *gorm.DB, org int64, domain string) (*Company, error) {
	if domain == "" {
		return nil, nil
	}
	var found []Company
	if err := tx.Where("org_id = ? and lower(domain) = ?", org, domain).Limit(2).Find(&found).Error; err != nil || len(found) != 1 {
		return nil, err
	}
	return &found[0], nil
}

// bareDomain reads "https://www.Example.com/about" and "example.com" alike.
func bareDomain(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if d := domainOf(v); d != "" {
		return d
	}
	v, _, _ = strings.Cut(v, "/")
	return strings.TrimPrefix(v, "www.")
}

func (s *Store) CompanyByID(org, id int64) (*Company, error) {
	var c Company
	return &c, s.in(org).First(&c, id).Error
}

// companyRef resolves an agent's "company" argument: an id or a name.
func (s *Store) companyRef(org int64, ref string) (*Company, error) {
	var c Company
	q := s.in(org)
	var id int64
	if _, err := fmt.Sscan(ref, &id); err == nil && fmt.Sprint(id) == strings.TrimSpace(ref) {
		q = q.Where("id = ?", id)
	} else {
		q = q.Where("lower(name) = ?", strings.ToLower(strings.TrimSpace(ref)))
	}
	if err := q.First(&c).Error; err != nil {
		return nil, missing(err, "no company %q in this organisation", ref)
	}
	return &c, nil
}

func (s *Store) Facts(org, company int64) ([]Fact, error) {
	var f []Fact
	return f, s.in(org).Where("company_id = ?", company).Order("id").Find(&f).Error
}

func (s *Store) People(org, company int64) ([]Contact, error) {
	var c []Contact
	return c, s.in(org).Where("company_id = ? and key not like 'org:%'", company).Order("status desc, id").Find(&c).Error
}

type HistoryItem struct {
	At     time.Time
	Actor  string
	Action string
	Value  string
	Person string
}

const historyPageSize = 50

// History is one page of what happened at a company, newest first.
func (s *Store) History(org, company int64, page int) ([]HistoryItem, Page, error) {
	total, err := s.historyCount(org, company)
	if err != nil {
		return nil, Page{}, err
	}
	pg := pageOf(page, historyPageSize, total)
	out, err := s.historyAt(org, company, pg.offset(), pg.Size)
	return out, pg, err
}

func (s *Store) historyCount(org, company int64) (int, error) {
	var total int64
	err := s.in(org).Model(&Event{}).Where("company_id = ?", company).Count(&total).Error
	return int(total), err
}

func (s *Store) historyAt(org, company int64, offset, limit int) ([]HistoryItem, error) {
	var events []Event
	if err := s.in(org).Where("company_id = ?", company).Order("at desc, id desc").Limit(limit).Offset(offset).Find(&events).Error; err != nil {
		return nil, err
	}
	names := map[int64]string{}
	var people []Contact
	if err := s.in(org).Where("company_id = ?", company).Find(&people).Error; err != nil {
		return nil, err
	}
	for _, p := range people {
		names[p.ID] = displayName(p)
	}
	out := make([]HistoryItem, len(events))
	for i, e := range events {
		out[i] = HistoryItem{At: e.At, Actor: e.User, Action: e.Action, Value: deref(e.Value), Person: names[e.ContactID]}
	}
	return out, nil
}

// Touches counts every time someone at a company was reached out to, whichever page
// of its history is on show.
func (s *Store) Touches(org, company int64) (int, error) {
	var n int64
	err := s.in(org).Model(&Event{}).Where("company_id = ? and action in ?", company,
		[]string{"mark:linkedin", "mark:email", "mark:followup", "mark:call"}).Count(&n).Error
	return int(n), err
}

// ResearchRow is one company in the Research tab with what's known about it.
type ResearchRow struct {
	Company
	Facts      []Fact
	People     []Contact
	Pending    map[int64][]PendingDetail
	Lookups    map[int64][]Lookup
	Incomplete bool
}

var ResearchFilters = []struct{ Key, Label string }{
	{"review", "To review"}, {"contact", "Needs a contact"}, {"verify", "Needs verifying"}, {"details", "Details to check"}, {"rejected", "Rejected"},
}

// N counts from 1.
type Page struct{ N, Size, Total int }

// pageOf clamps n, so a page that emptied since it was linked shows the last one instead.
func pageOf(n, size, total int) Page {
	p := Page{N: n, Size: size, Total: total}
	p.N = max(1, min(p.N, p.Pages()))
	return p
}

func (p Page) Pages() int  { return max(1, (p.Total+p.Size-1)/p.Size) }
func (p Page) offset() int { return (p.N - 1) * p.Size }

const researchPageSize = 30

func (s *Store) researchQuery(org int64, filter string, task int64) (*gorm.DB, string, error) {
	q := s.in(org).Model(&Company{})
	if task != 0 {
		// What a task found is what was done for it, or noted for it, at a company.
		done := s.db.Model(&Event{}).Select("company_id").Where("org_id = ? and task_id = ?", org, task)
		noted := s.db.Model(&TaskNote{}).Select("company_id").Where("org_id = ? and task_id = ?", org, task)
		q = q.Where("(id in (?) or id in (?))", done, noted)
	}
	switch filter {
	case "review":
		return q.Where("stage = ?", CompanyReview), "created_at desc, id desc", nil
	case "contact":
		withPeople := s.db.Model(&Contact{}).Select("company_id").
			Where("org_id = ? and company_id is not null and key not like 'org:%' and status <> 'suggested'", org)
		return q.Where("stage = ? and hold is null and id not in (?)", CompanyQualified, withPeople), "fit, name, id", nil
	case "verify":
		guess := s.db.Model(&Contact{}).Select("company_id").
			Where("org_id = ? and company_id is not null and (email_status = 'guess' or flags like '%INFERRED%') and email_sent is null and hold is null", org)
		return q.Where("id in (?)", guess), "name, id", nil
	case "details":
		waiting := s.db.Model(&Contact{}).Select("company_id").
			Where("org_id = ? and company_id is not null and id in (?)", org,
				s.db.Model(&Suggestion{}).Select("contact_id").Where("org_id = ? and decision = ''", org))
		return q.Where("id in (?)", waiting), "name, id", nil
	case "rejected":
		return q.Where("stage = ?", CompanyRejected), "updated_at desc, id desc", nil
	}
	return nil, "", fmt.Errorf("unknown filter %q", filter)
}

// researchRows fills in what is known about each company, in a query a table rather
// than a few a company.
func (s *Store) researchRows(org int64, companies []Company) ([]ResearchRow, error) {
	rows := make([]ResearchRow, len(companies))
	if len(companies) == 0 {
		return rows, nil
	}
	at := map[int64]int{}
	ids := make([]int64, len(companies))
	for i, c := range companies {
		rows[i].Company, rows[i].Pending, rows[i].Lookups = c, map[int64][]PendingDetail{}, map[int64][]Lookup{}
		at[c.ID], ids[i] = i, c.ID
	}
	var facts []Fact
	if err := s.in(org).Where("company_id in ?", ids).Order("id").Find(&facts).Error; err != nil {
		return nil, err
	}
	var people []Contact
	if err := s.in(org).Where("company_id in ? and key not like 'org:%'", ids).Order("status desc, id").Find(&people).Error; err != nil {
		return nil, err
	}
	pending, err := s.Pending(org, people)
	if err != nil {
		return nil, err
	}
	personIDs := make([]int64, len(people))
	for i, p := range people {
		personIDs[i] = p.ID
	}
	looked, err := s.lookups(org, personIDs)
	if err != nil {
		return nil, err
	}
	for _, f := range facts {
		r := &rows[at[f.CompanyID]]
		r.Facts = append(r.Facts, f)
	}
	for _, p := range people {
		r := &rows[at[*p.CompanyID]]
		r.People = append(r.People, p)
		if waiting := pending[p.ID]; len(waiting) > 0 {
			r.Pending[p.ID] = waiting
		}
		if l := looked[p.ID]; len(l) > 0 {
			r.Lookups[p.ID] = l
		}
	}
	return rows, nil
}

// detailsToCheck can't be paged in SQL: whether a suggestion still needs checking
// depends on comparing it with what the person holds now, the way a person would.
func (s *Store) detailsToCheck(org, task int64) ([]ResearchRow, error) {
	q, order, err := s.researchQuery(org, "details", task)
	if err != nil {
		return nil, err
	}
	var companies []Company
	if err := q.Order(order).Limit(500).Find(&companies).Error; err != nil {
		return nil, err
	}
	rows, err := s.researchRows(org, companies)
	// A suggestion the person was since given some other way leaves nothing to check.
	return slices.DeleteFunc(rows, func(r ResearchRow) bool { return len(r.Pending) == 0 }), err
}

func (s *Store) Research(org int64, filter string, page int) ([]ResearchRow, Page, error) {
	return s.researchFor(org, filter, page, 0)
}

// researchFor is Research narrowed to what one task found, or all of it for task 0.
func (s *Store) researchFor(org int64, filter string, page int, task int64) ([]ResearchRow, Page, error) {
	if filter == "details" {
		rows, err := s.detailsToCheck(org, task)
		if err != nil {
			return nil, Page{}, err
		}
		pg := pageOf(page, researchPageSize, len(rows))
		return rows[pg.offset():min(len(rows), pg.offset()+pg.Size)], pg, nil
	}
	q, order, err := s.researchQuery(org, filter, task)
	if err != nil {
		return nil, Page{}, err
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, Page{}, err
	}
	pg := pageOf(page, researchPageSize, int(total))
	var companies []Company
	if err := q.Order(order).Limit(pg.Size).Offset(pg.offset()).Find(&companies).Error; err != nil {
		return nil, pg, err
	}
	rows, err := s.researchRows(org, companies)
	return rows, pg, err
}

// searchFitCap is how many companies to review one search may have and still be ranked
// by fit; past it, they come newest first as everywhere else.
const searchFitCap = 500

// rankedResearch is one search's companies to review, best fit first, a page of them.
func (s *Store) rankedResearch(org int64, set OrgSettings, task int64, page int) ([]ResearchRow, Page, bool, error) {
	rows, ranked, err := s.rankedRows(org, set, task)
	if err != nil {
		return nil, Page{}, false, err
	}
	if !ranked {
		rows, pg, err := s.researchFor(org, "review", page, task)
		return rows, pg, false, err
	}
	pg := pageOf(page, researchPageSize, len(rows))
	return rows[pg.offset():min(len(rows), pg.offset()+pg.Size)], pg, true, nil
}

// rankedRows are all of one search's companies to review, best fit first: fit is worked
// out from facts, so they are ranked here rather than in SQL. A search with more than
// searchFitCap gives none, and false.
func (s *Store) rankedRows(org int64, set OrgSettings, task int64) ([]ResearchRow, bool, error) {
	q, order, err := s.researchQuery(org, "review", task)
	if err != nil {
		return nil, false, err
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, false, err
	}
	if total > searchFitCap {
		return nil, false, nil
	}
	var companies []Company
	if err := q.Order(order).Limit(searchFitCap).Find(&companies).Error; err != nil {
		return nil, false, err
	}
	rows, err := s.researchRows(org, companies)
	if err != nil {
		return nil, false, err
	}
	fits := make(map[int64]Fit, len(rows))
	for _, r := range rows {
		fits[r.ID] = fitOf(assetsFound(set, r.Facts))
	}
	slices.SortStableFunc(rows, func(a, b ResearchRow) int {
		switch {
		case fits[a.ID].better(fits[b.ID]):
			return -1
		case fits[b.ID].better(fits[a.ID]):
			return 1
		}
		return 0
	})
	return rows, true, nil
}

// researchList is a page of the Research queue for the API: the rows the page shows, in
// its order, with a cursor rather than a page number.
func (s *Store) researchList(org int64, set OrgSettings, filter string, task int64, in PageIn) ([]ResearchRow, bool, string, error) {
	switch {
	case filter == "review" && task != 0:
		rows, ranked, err := s.rankedRows(org, set, task)
		if err != nil {
			return nil, false, "", err
		}
		if ranked {
			page, next, err := listPage(rows, in.Limit, in.Cursor)
			return page, true, next, err
		}
	case filter == "details":
		rows, err := s.detailsToCheck(org, task)
		if err != nil {
			return nil, false, "", err
		}
		page, next, err := listPage(rows, in.Limit, in.Cursor)
		return page, false, next, err
	}
	at, err := in.at(1)
	if err != nil {
		return nil, false, "", err
	}
	q, order, err := s.researchQuery(org, filter, task)
	if err != nil {
		return nil, false, "", err
	}
	var companies []Company
	if err := q.Order(order).Limit(in.size() + 1).Offset(int(at[0])).Find(&companies).Error; err != nil {
		return nil, false, "", err
	}
	next := ""
	if len(companies) > in.size() {
		companies, next = companies[:in.size()], cursorOf(at[0]+int64(in.size()))
	}
	rows, err := s.researchRows(org, companies)
	return rows, false, next, err
}

func (s *Store) ResearchCount(org int64, filter string) int {
	return s.researchCount(org, filter, 0)
}

func (s *Store) researchCount(org int64, filter string, task int64) int {
	if filter == "details" {
		rows, _ := s.detailsToCheck(org, task)
		return len(rows)
	}
	q, _, err := s.researchQuery(org, filter, task)
	if err != nil {
		return 0
	}
	var n int64
	logged(q.Count(&n))
	return int(n)
}

func (s *Store) ResearchCounts(org int64) map[string]int {
	return s.researchCountsFor(org, 0)
}

func (s *Store) researchCountsFor(org, task int64) map[string]int {
	out := map[string]int{}
	for _, f := range ResearchFilters {
		out[f.Key] = s.researchCount(org, f.Key, task)
	}
	return out
}

func (s *Store) SetCompanyStage(org, id int64, stage, fit, actor string) error {
	if stage != CompanyQualified && stage != CompanyRejected && stage != CompanyReview {
		return fmt.Errorf("unknown stage %q", stage)
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{"stage": stage}
		if fit != "" {
			updates["fit"] = fit
		}
		res := tx.Model(&Company{}).Where("org_id = ? and id = ?", org, id).Updates(updates)
		if res.Error != nil || res.RowsAffected == 0 {
			return missing(res.Error, "no company %d", id)
		}
		return s.log(tx, org, 0, &id, actor, "company:"+stage, nonEmpty(fit))
	})
}

// PickContact moves a suggested person into outreach, with a note from the org's template.
func (s *Store) PickContact(org int64, o *Org, id int64, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var c Contact
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
			return missing(err, "no contact %d", id)
		}
		updates := map[string]any{"status": ""}
		if deref(c.Note) == "" {
			updates["note"] = fill(o.Settings.withDefaults().NoteTemplate, deref(c.First), companyName(deref(c.Maker)), o.Name)
		}
		if err := tx.Model(&c).Updates(updates).Error; err != nil {
			return err
		}
		if c.CompanyID != nil {
			res := tx.Model(&Company{}).Where("org_id = ? and id = ? and stage = ?", org, *c.CompanyID, CompanyReview).Update("stage", CompanyQualified)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected > 0 {
				if err := s.log(tx, org, 0, c.CompanyID, actor, "company:"+CompanyQualified, nil); err != nil {
					return err
				}
			}
		}
		return s.log(tx, org, id, c.CompanyID, actor, "pick", nil)
	})
}

func fill(tpl, first, company, org string) string {
	return strings.NewReplacer("{first}", first, "{company}", company, "{org}", org).Replace(tpl)
}

// Conversation stages, in board order.
var Stages = []struct{ Key, Label string }{
	{"replied", "Replied"}, {"meeting", "Meeting"}, {"proposal", "Proposal"}, {"won", "Won"}, {"lost", "Lost"},
}

func (s *Store) Conversations(org int64) (map[string][]Contact, error) {
	var rows []Contact
	if err := s.in(org).Where(inConversation).Order("next_date nulls first, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string][]Contact{}
	for _, c := range rows {
		out[c.ConversationStage()] = append(out[c.ConversationStage()], c)
	}
	return out, nil
}

// NeedsNextStep is a live conversation with no next step, or one that's due.
func NeedsNextStep(c Contact, day string) bool {
	if c.Stage == "won" || c.Stage == "lost" {
		return false
	}
	return c.NextStep == "" || (c.NextDate != nil && *c.NextDate <= day)
}

type CompanyListRow struct {
	Company
	People   int
	Pipeline string
}

const companiesPageSize = 50

// A company stands where its furthest contact does. The stages run in this order, so a
// company with one lost conversation and another person still in outreach is in outreach.
var pipelineOrder = []string{"research", "lost", "outreach", "replied", "meeting", "proposal", "won"}

// furthestSQL must agree with furthest; TestPipelineInSQL holds them together.
const furthestSQL = `max(case
	when declined is not null then case when replied is not null or linkedin_added is not null or email_sent is not null or called is not null then 2 else 0 end
	when replied is not null then case stage when 'lost' then 1 when 'meeting' then 4 when 'proposal' then 5 when 'won' then 6 else 3 end
	when linkedin_added is not null or email_sent is not null or called is not null then 2
	else 0 end)`

func furthest(p Contact) int {
	switch {
	case p.Declined != nil:
		// Someone who declined before anyone reached them was never in the pipeline.
		if p.Touched() || p.Replied != nil {
			return 2
		}
		return 0
	case p.Replied != nil:
		switch p.Stage {
		case "lost":
			return 1
		case "meeting":
			return 4
		case "proposal":
			return 5
		case "won":
			return 6
		}
		return 3
	case p.Touched():
		return 2
	}
	return 0
}

func pipelineAt(c Company, rank int) string {
	if rank == 0 && c.Stage == CompanyRejected {
		return "rejected"
	}
	return pipelineOrder[rank]
}

// pipeline is where a company stands, from its own stage and its furthest contact.
func pipeline(c Company, people []Contact) string {
	best := 0
	for _, p := range people {
		best = max(best, furthest(p))
	}
	return pipelineAt(c, best)
}

func (s *Store) Companies(org int64, q, segment, stage string, page int) ([]CompanyListRow, Page, error) {
	db, people := s.companiesQuery(org, q, segment, stage)
	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, Page{}, err
	}
	pg := pageOf(page, companiesPageSize, int(total))
	rows, err := s.companyRows(db, people, pg.offset(), pg.Size)
	return rows, pg, err
}

// companiesList is a page of the Companies list for the API, with how many match in all.
func (s *Store) companiesList(org int64, q, segment, stage string, in PageIn) ([]CompanyListRow, int, string, error) {
	at, err := in.at(1)
	if err != nil {
		return nil, 0, "", err
	}
	db, people := s.companiesQuery(org, q, segment, stage)
	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, "", err
	}
	rows, err := s.companyRows(db, people, int(at[0]), in.size())
	next := ""
	if end := at[0] + int64(in.size()); end < total {
		next = cursorOf(end)
	}
	return rows, int(total), next, err
}

// companiesQuery is the companies the filters leave, and each company's standing from its people.
func (s *Store) companiesQuery(org int64, q, segment, stage string) (db, people *gorm.DB) {
	people = s.db.Model(&Contact{}).Select("company_id, count(*) as people, "+furthestSQL+" as furthest").
		Where("org_id = ? and company_id is not null and key not like 'org:%'", org).Group("company_id")
	db = s.db.Model(&Company{}).Where("companies.org_id = ?", org)
	if q = strings.TrimSpace(strings.ToLower(q)); q != "" {
		// A union, so each half is answered by its own search index.
		company, args := s.companyMatch(q)
		person, personArgs := s.contactMatch(q, false)
		matched := "select id from companies where org_id = ? and " + company +
			" union select company_id from contacts where org_id = ? and company_id is not null and " + person
		args = append(append(append([]any{org}, args...), org), personArgs...)
		db = db.Where("companies.id in ("+matched+")", args...)
	}
	if segment != "" {
		db = db.Where("companies.segment = ?", segment)
	}
	// Only a stage filter needs every company's standing; otherwise it's worked out for the page alone.
	if stage != "" {
		db = db.Joins("left join (?) p on p.company_id = companies.id", people)
		switch stage {
		case "research":
			db = db.Where("coalesce(p.furthest, 0) = 0 and companies.stage <> ?", CompanyRejected)
		case "rejected":
			db = db.Where("coalesce(p.furthest, 0) = 0 and companies.stage = ?", CompanyRejected)
		default:
			db = db.Where("p.furthest = ?", slices.Index(pipelineOrder, stage))
		}
	}
	return db, people
}

func (s *Store) companyRows(db, people *gorm.DB, offset, limit int) ([]CompanyListRow, error) {
	var companies []Company
	if err := db.Select("companies.*").Order("companies.name, companies.id").Limit(limit).Offset(offset).Find(&companies).Error; err != nil {
		return nil, err
	}
	ids := make([]int64, len(companies))
	for i, c := range companies {
		ids[i] = c.ID
	}
	var standing []struct {
		CompanyID        int64
		People, Furthest int
	}
	if err := people.Where("company_id in ?", ids).Scan(&standing).Error; err != nil {
		return nil, err
	}
	at := map[int64]int{}
	for i, st := range standing {
		at[st.CompanyID] = i
	}
	out := make([]CompanyListRow, len(companies))
	for i, c := range companies {
		out[i] = CompanyListRow{Company: c, Pipeline: pipelineAt(c, 0)}
		if j, ok := at[c.ID]; ok {
			out[i].People, out[i].Pipeline = standing[j].People, pipelineAt(c, standing[j].Furthest)
		}
	}
	return out, nil
}

func (s *Store) Segments(org int64) []string {
	out, _ := s.segments(org)
	return out
}

func (s *Store) segments(org int64) ([]string, error) {
	var out []string
	return out, logged(s.in(org).Model(&Company{}).Where("segment <> ''").Distinct().Order("segment").Pluck("segment", &out))
}

type Stats struct{ Touches, Replies, Meetings, Researched int64 }

func (s *Store) Week(org int64, now time.Time) Stats {
	since := now.AddDate(0, 0, -7)
	day := since.Format("2006-01-02")
	var st Stats
	// From the contacts' own dates, so history brought over from an import counts.
	for _, col := range []string{"linkedin_added", "email_sent", "followup_sent", "called"} {
		var n int64
		logged(s.in(org).Model(&Contact{}).Where(col+" >= ?", day).Count(&n))
		st.Touches += n
	}
	logged(s.in(org).Model(&Contact{}).Where("replied >= ?", day).Count(&st.Replies))
	logged(s.in(org).Model(&Event{}).Where("at >= ? and action = ? and value = ?", since, "set:stage", "meeting").Count(&st.Meetings))
	logged(s.in(org).Model(&Company{}).Where("created_at >= ? and found_by <> 'import'", since).Count(&st.Researched))
	return st
}

type AgentEvent struct {
	At     time.Time
	Actor  string
	Action string
	Value  string
}

const activityPageSize = 20

// AgentActivity is one page of what agents did, newest first.
func (s *Store) AgentActivity(org int64, page int) ([]AgentEvent, Page) {
	events, total := s.agentEvents(org, 0, 0)
	pg := pageOf(page, activityPageSize, total)
	events, _ = s.agentEvents(org, pg.offset(), pg.Size)
	return events, pg
}

// agentEvents is limit of what agents did from offset, newest first, and how many they
// did in all; a limit of 0 reads none, only the count.
func (s *Store) agentEvents(org int64, offset, limit int) ([]AgentEvent, int) {
	agents := func() *gorm.DB { return s.in(org).Model(&Event{}).Where(`"user" like 'agent:%'`) }
	var total int64
	logged(agents().Count(&total))
	if limit == 0 {
		return nil, int(total)
	}
	var events []Event
	logged(agents().Order("at desc, id desc").Limit(limit).Offset(offset).Find(&events))
	out := make([]AgentEvent, len(events))
	for i, e := range events {
		out[i] = AgentEvent{e.At, strings.TrimPrefix(e.User, "agent:"), e.Action, deref(e.Value)}
	}
	return out, int(total)
}

// QuickAdd is the "Add person" form and the LinkedIn bookmarklet: one person straight into the LinkedIn queue.
type QuickAdd struct {
	Linkedin string `json:"linkedin"`
	Name     string `json:"name"`
	Title    string `json:"title"`
	Company  string `json:"company"`
	Email    string `json:"email"`
	Note     string `json:"note"`
	Added    bool   `json:"added"`
}

func (s *Store) QuickAddPerson(o *Org, in QuickAdd, actor string) (*Contact, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Linkedin = strings.TrimSpace(in.Linkedin)
	if in.Name == "" {
		return nil, errors.New("a name is needed")
	}
	if in.Linkedin != "" && !httpURL.MatchString(in.Linkedin) {
		return nil, errors.New("the LinkedIn link should start with https://")
	}
	if err := s.allowed(o, in.Email, in.Linkedin); err != nil {
		return nil, err
	}
	if err := erased(s.db, o.ID, in.Name, in.Company, in.Email, in.Linkedin); err != nil {
		return nil, err
	}
	var out Contact
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := holdOrg(tx, o.ID); err != nil {
			return err
		}
		var companyID *int64
		maker := strings.TrimSpace(in.Company)
		if maker != "" {
			c, _, err := ensureCompany(tx, o.ID, Company{Name: maker, Stage: CompanyQualified, FoundBy: actor})
			if err != nil {
				return err
			}
			companyID = &c.ID
		}
		first := firstField(in.Name)
		note := strings.TrimSpace(in.Note)
		if note == "" {
			note = fill(o.Settings.withDefaults().NoteTemplate, first, maker, o.Name)
		}
		person := in.Name
		if in.Title != "" {
			person += ", " + strings.TrimSpace(in.Title)
		}
		out = Contact{OrgID: o.ID, Key: uniqueKey(tx, o.ID, personKey(in.Name)), File: "added by " + actor,
			Person: &person, First: &first, Maker: nonEmpty(maker), Linkedin: nonEmpty(in.Linkedin),
			Email: nonEmpty(strings.TrimSpace(in.Email)), Note: &note, Title: strings.TrimSpace(in.Title), CompanyID: companyID}
		if in.Added {
			out.LinkedinAdded = str(today())
		}
		if err := tx.Create(&out).Error; err != nil {
			return err
		}
		if err := s.log(tx, o.ID, out.ID, companyID, actor, "add:person", nil); err != nil {
			return err
		}
		if strings.TrimSpace(in.Note) != "" {
			// Recorded as an edit so an agent's draft doesn't replace a note the person wrote.
			return s.log(tx, o.ID, out.ID, companyID, actor, "set:note", nil)
		}
		return nil
	})
	return &out, err
}

func uniqueKey(tx *gorm.DB, org int64, key string) string {
	try := key
	for i := 2; ; i++ {
		var n int64
		logged(tx.Model(&Contact{}).Where("org_id = ? and key = ?", org, try).Count(&n))
		if n == 0 {
			return try
		}
		try = fmt.Sprintf("%s (%d)", key, i)
	}
}

// allowed applies the org's never-contact list to an email address or link.
func (s *Store) allowed(o *Org, email, link string) error {
	return o.Settings.listed(email, link)
}

// listed reports the never-contact entry an email address or link falls under, if any.
// Each entry is one thing: a LinkedIn profile, an email address, or a domain. Nothing
// matches by loose substring, because the list also hides people from the queues and
// a stray word must not hide the wrong ones.
func (set OrgSettings) listed(email, link string) error {
	email, profile := strings.ToLower(strings.TrimSpace(email)), linkedinPath(link)
	for _, raw := range strings.FieldsFunc(strings.ToLower(set.NeverContact), func(r rune) bool { return r == ',' || r == '\n' || r == ' ' }) {
		entry := strings.TrimRight(strings.TrimPrefix(schemeRe.ReplaceAllString(strings.TrimSpace(raw), ""), "www."), "/")
		if entry == "" {
			continue
		}
		hit := false
		switch want := linkedinPath(entry); {
		case want != "":
			hit = profile == want || strings.HasPrefix(profile, want+"/")
		case strings.Contains(entry, "@"):
			hit = email == entry
		default:
			host, _, _ := strings.Cut(entry, "/")
			hit = email != "" && (strings.HasSuffix(email, "@"+host) || strings.HasSuffix(email, "."+host))
		}
		if hit {
			return fmt.Errorf("%s is on the never-contact list", raw)
		}
	}
	return nil
}

var schemeRe = regexp.MustCompile(`^[a-z]+://`)

// linkedinPath is what identifies a LinkedIn page whatever the subdomain: "in/hannah-pryce".
func linkedinPath(link string) string {
	_, path, found := strings.Cut(strings.ToLower(strings.TrimSpace(link)), "linkedin.com/")
	if !found {
		return ""
	}
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	return strings.TrimRight(path, "/")
}

// NeverContactCovers names the people the list is keeping out of the outreach queues:
// those who would otherwise be open for a next step.
func (s *Store) NeverContactCovers(o *Org) []string {
	if strings.TrimSpace(o.Settings.NeverContact) == "" {
		return nil
	}
	var people []Contact
	logged(s.in(o.ID).Where(openContacts).Where(companyNotRejected).Order("id").Find(&people))
	var names []string
	for _, p := range people {
		if o.Settings.listed(deref(p.Email), deref(p.Linkedin)) != nil {
			names = append(names, displayName(p))
		}
	}
	return names
}

// LinkedIn page titles look like "(3) Hannah Pryce | LinkedIn"; headlines "Head of Marketing at Larkmoor Yachts".
var (
	unreadPrefix = regexp.MustCompile(`^\(\d+\)\s*`)
	linkedinTail = regexp.MustCompile(`\s*[|–-]\s*LinkedIn.*$`)
)

func ParseLinkedIn(title, headline string) (name, role, company string) {
	name = strings.TrimSpace(linkedinTail.ReplaceAllString(unreadPrefix.ReplaceAllString(title, ""), ""))
	headline = strings.TrimSpace(strings.SplitN(headline, "|", 2)[0])
	if i := strings.LastIndex(headline, " at "); i > 0 {
		return name, strings.TrimSpace(headline[:i]), strings.TrimSpace(headline[i+4:])
	}
	if i := strings.LastIndex(headline, " @ "); i > 0 {
		return name, strings.TrimSpace(headline[:i]), strings.TrimSpace(headline[i+3:])
	}
	return name, headline, ""
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
