package main

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/paging"
)

// prospectingStore is the prospecting area's queries: companies and the people at them,
// what is found out about each, and searching them.
type prospectingStore struct {
	*core
	// An organisation's settings decide who is left out, and a person's stage is the
	// conversation's deal's.
	identity *identityStore
	deals    *dealStore
}

func (s *prospectingStore) CompanyCount(org int64) (int64, error) {
	var n int64
	return n, s.in(org).Model(&Company{}).Count(&n).Error
}

// CompanyNotes is one page of the notes left on a company, newest first, and how many there are.
func (s *prospectingStore) CompanyNotes(org, company int64, offset, limit int) ([]TaskNote, int64, error) {
	var total int64
	if err := s.in(org).Model(&TaskNote{}).Where("company_id = ?", company).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var notes []TaskNote
	return notes, total, s.in(org).Where("company_id = ?", company).Order("id desc").Limit(limit).Offset(offset).Find(&notes).Error
}

func (s *prospectingStore) CompanyByID(org, id int64) (*Company, error) {
	var c Company
	return &c, s.in(org).First(&c, id).Error
}

// companyRef resolves an agent's "company" argument: an id or a name.
func (s *prospectingStore) companyRef(org int64, ref string) (*Company, error) {
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

func (s *prospectingStore) Facts(org, company int64) ([]Fact, error) {
	var f []Fact
	return f, s.in(org).Where("company_id = ?", company).Order("id").Find(&f).Error
}

func (s *prospectingStore) People(org, company int64) ([]Contact, error) {
	var c []Contact
	return c, s.in(org).Where("company_id = ? and key not like 'org:%'", company).Order("status desc, id").Find(&c).Error
}

// History is one page of what happened at a company, newest first.
func (s *prospectingStore) History(org, company int64, page int) ([]HistoryItem, Page, error) {
	total, err := s.HistoryCount(org, company)
	if err != nil {
		return nil, Page{}, err
	}
	pg := pageOf(page, historyPageSize, total)
	out, err := s.HistoryAt(org, company, pg.offset(), pg.Size)
	return out, pg, err
}

func (s *prospectingStore) HistoryCount(org, company int64) (int, error) {
	var total int64
	err := s.in(org).Model(&Event{}).Where("company_id = ?", company).Count(&total).Error
	return int(total), err
}

func (s *prospectingStore) HistoryAt(org, company int64, offset, limit int) ([]HistoryItem, error) {
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
func (s *prospectingStore) Touches(org, company int64) (int, error) {
	var n int64
	err := s.in(org).Model(&Event{}).Where("company_id = ? and action in ?", company,
		[]string{"mark:linkedin", "mark:email", "mark:followup", "mark:call"}).Count(&n).Error
	return int(n), err
}

// researchQuery is the companies under one of Research's filters, for one offering, 0
// being the organisation's own: its stages are the companies' own, another's are its
// verdicts. People count towards the offering they are being reached for.
func (s *prospectingStore) researchQuery(org, offering int64, filter string, task int64) (*gorm.DB, string, error) {
	q := s.in(org).Model(&Company{})
	if task != 0 {
		// What a task found is what was done for it, or noted for it, at a company.
		done := s.db.Model(&Event{}).Select("company_id").Where("org_id = ? and task_id = ?", org, task)
		noted := s.db.Model(&TaskNote{}).Select("company_id").Where("org_id = ? and task_id = ?", org, task)
		q = q.Where("(id in (?) or id in (?))", done, noted)
	}
	people := s.db.Model(&Contact{}).Select("company_id").Where("org_id = ? and company_id is not null and offering_id = ?", org, offering)
	staged := func(stage string) *gorm.DB {
		if offering == 0 {
			return q.Where("stage = ?", stage)
		}
		return q.Where("id in (?)", s.db.Model(&Verdict{}).Select("company_id").Where("org_id = ? and offering_id = ? and stage = ?", org, offering, stage))
	}
	// A company the never-contact list covers is kept, but nobody is asked to review it
	// or find someone there.
	if filter == "review" || filter == "contact" {
		o, err := s.identity.OrgByID(org)
		if err != nil {
			return nil, "", err
		}
		q = notListed(q, o.Settings)
	}
	switch filter {
	case "review":
		if offering == 0 {
			return q.Where("stage = ? and found_for = 0", CompanyReview), "created_at desc, id desc", nil
		}
		return staged(CompanyReview), "created_at desc, id desc", nil
	case "contact":
		picked := people.Where("key not like 'org:%' and status <> 'suggested'")
		return staged(CompanyQualified).Where("hold is null and id not in (?)", picked), "fit, name, id", nil
	case "verify":
		guess := people.Where("(email_status = 'guess' or flags like '%INFERRED%') and email_sent is null and hold is null")
		return q.Where("id in (?)", guess), "name, id", nil
	case "details":
		waiting := people.Where("id in (?)", s.db.Model(&Suggestion{}).Select("contact_id").Where("org_id = ? and decision = ''", org))
		return q.Where("id in (?)", waiting), "name, id", nil
	case "rejected":
		if offering == 0 {
			return staged(CompanyRejected), "updated_at desc, id desc", nil
		}
		latest := fmt.Sprintf("(select v.updated_at from verdicts v where v.org_id = companies.org_id and v.company_id = companies.id and v.offering_id = %d) desc, id desc", offering)
		return staged(CompanyRejected), latest, nil
	}
	return nil, "", fmt.Errorf("unknown filter %q", filter)
}

// researchRows fills in what is known about each company, in a query a table rather
// than a few a company.
func (s *prospectingStore) researchRows(org int64, companies []Company) ([]ResearchRow, error) {
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
	looked, err := s.lookupsOf(org, personIDs)
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
func (s *prospectingStore) detailsToCheck(org, offering, task int64) ([]ResearchRow, error) {
	q, order, err := s.researchQuery(org, offering, "details", task)
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

func (s *prospectingStore) Research(org int64, filter string, page int) ([]ResearchRow, Page, error) {
	return s.researchFor(org, filter, page, 0)
}

// researchFor is Research for the organisation's own offering, narrowed to what one task
// found, or all of it for task 0.
func (s *prospectingStore) researchFor(org int64, filter string, page int, task int64) ([]ResearchRow, Page, error) {
	const offering = 0
	if filter == "details" {
		rows, err := s.detailsToCheck(org, offering, task)
		if err != nil {
			return nil, Page{}, err
		}
		pg := pageOf(page, researchPageSize, len(rows))
		return rows[pg.offset():min(len(rows), pg.offset()+pg.Size)], pg, nil
	}
	q, order, err := s.researchQuery(org, offering, filter, task)
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

// rankedRows are all of one search's companies to review, best fit first: fit is worked
// out from facts, so they are ranked here rather than in SQL. A search with more than
// searchFitCap gives none, and false.
func (s *prospectingStore) rankedRows(org, offering int64, set OrgSettings, task int64) ([]ResearchRow, bool, error) {
	q, order, err := s.researchQuery(org, offering, "review", task)
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

// ResearchList is a page of the Research queue for the API: the rows the page shows, in
// its order, with a cursor rather than a page number.
func (s *prospectingStore) ResearchList(org, offering int64, set OrgSettings, filter string, task int64, in paging.Page) ([]ResearchRow, bool, string, error) {
	switch {
	case filter == "review" && task != 0:
		rows, ranked, err := s.rankedRows(org, offering, set, task)
		if err != nil {
			return nil, false, "", err
		}
		if ranked {
			page, next, err := listPage(rows, in.Limit, in.Cursor)
			return page, true, next, err
		}
	case filter == "details":
		rows, err := s.detailsToCheck(org, offering, task)
		if err != nil {
			return nil, false, "", err
		}
		page, next, err := listPage(rows, in.Limit, in.Cursor)
		return page, false, next, err
	}
	at, err := in.At(1)
	if err != nil {
		return nil, false, "", err
	}
	q, order, err := s.researchQuery(org, offering, filter, task)
	if err != nil {
		return nil, false, "", err
	}
	var companies []Company
	if err := q.Order(order).Limit(in.Size() + 1).Offset(int(at[0])).Find(&companies).Error; err != nil {
		return nil, false, "", err
	}
	next := ""
	if len(companies) > in.Size() {
		companies, next = companies[:in.Size()], paging.Cursor(at[0]+int64(in.Size()))
	}
	rows, err := s.researchRows(org, companies)
	return rows, false, next, err
}

func (s *prospectingStore) ResearchCount(org int64, filter string) int {
	return s.researchCount(org, 0, filter, 0)
}

// detailsCount is how many companies detailsToCheck lists, from the same companies in
// the same order, reading only their people and what was suggested for them: not the
// facts and lookups the list shows of each.
func (s *prospectingStore) detailsCount(org, offering, task int64) (int, error) {
	q, order, err := s.researchQuery(org, offering, "details", task)
	if err != nil {
		return 0, err
	}
	var ids []int64
	if err := q.Order(order).Limit(500).Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
		return 0, err
	}
	var people []Contact
	if err := s.in(org).Where("company_id in ? and key not like 'org:%'", ids).Find(&people).Error; err != nil {
		return 0, err
	}
	pending, err := s.Pending(org, people)
	if err != nil {
		return 0, err
	}
	waiting := map[int64]bool{}
	for _, p := range people {
		if len(pending[p.ID]) > 0 {
			waiting[*p.CompanyID] = true
		}
	}
	return len(waiting), nil
}

func (s *prospectingStore) researchCount(org, offering int64, filter string, task int64) int {
	if filter == "details" {
		n, _ := s.detailsCount(org, offering, task)
		return n
	}
	q, _, err := s.researchQuery(org, offering, filter, task)
	if err != nil {
		return 0
	}
	var n int64
	_ = logged(q.Count(&n))
	return int(n)
}

func (s *prospectingStore) ResearchCounts(org int64) map[string]int {
	return s.ResearchCountsFor(org, 0, 0)
}

func (s *prospectingStore) ResearchCountsFor(org, offering, task int64) map[string]int {
	out := map[string]int{}
	for _, f := range ResearchFilters {
		out[f.Key] = s.researchCount(org, offering, f.Key, task)
	}
	return out
}

func (s *prospectingStore) SetCompanyStage(org, id int64, stage, fit, actor string) error {
	if stage != CompanyQualified && stage != CompanyRejected && stage != CompanyReview {
		return fmt.Errorf("unknown stage %q", stage)
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{"stage": stage, "found_for": 0}
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
func (s *prospectingStore) PickContact(org int64, o *Org, id int64, actor string) error {
	return s.PickContactFor(org, o, id, nil, actor)
}

// PickContactFor picks someone for an offering, whatever they were suggested for, or for
// what they were suggested for when it is nil.
func (s *prospectingStore) PickContactFor(org int64, o *Org, id int64, offeringID *int64, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var c Contact
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
			return missing(err, "no contact %d", id)
		}
		updates := map[string]any{"status": ""}
		if offeringID != nil && *offeringID != c.OfferingID {
			// The offering is held before the person, as an agent adding them holds it.
			if *offeringID != 0 {
				if _, err := heldOffering(tx, org, *offeringID); err != nil {
					return err
				}
			}
			// Only while still suggested, checked as it is written, so two picks for different
			// offerings can't both land.
			res := tx.Model(&Contact{}).Where("org_id = ? and id = ? and status = ? and offering_id = ?", org, id, ContactSuggested, c.OfferingID).
				Update("offering_id", *offeringID)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return conflict(fmt.Sprintf("%s is already being reached for another offering", displayName(c)))
			}
			c.OfferingID = *offeringID
		}
		template := o.Settings.withDefaults().NoteTemplate
		var offering *Offering
		if c.OfferingID != 0 {
			var err error
			if offering, err = heldOffering(tx, org, c.OfferingID); err != nil {
				return err
			}
			if offering.NoteTemplate != "" {
				template = offering.NoteTemplate
			}
		}
		if deref(c.Note) == "" {
			updates["note"] = fill(template, deref(c.First), companyName(deref(c.Maker)), o.Name)
		}
		if err := tx.Model(&c).Updates(updates).Error; err != nil {
			return err
		}
		if c.CompanyID != nil && offering != nil {
			if err := qualifyFor(tx, org, offering, *c.CompanyID, actor); err != nil {
				return err
			}
		} else if c.CompanyID != nil {
			if err := foundForOwn(tx, org, *c.CompanyID); err != nil {
				return err
			}
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

func (s *prospectingStore) Conversations(org int64) (map[string][]Contact, error) {
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

func (s *prospectingStore) Companies(org int64, q, segment, stage string, page int) ([]CompanyListRow, Page, error) {
	db, people := s.companiesQuery(org, q, segment, stage)
	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, Page{}, err
	}
	pg := pageOf(page, companiesPageSize, int(total))
	rows, err := s.companyRows(db, people, pg.offset(), pg.Size)
	return rows, pg, err
}

// CompaniesList is a page of the Companies list for the API, with how many match in all.
func (s *prospectingStore) CompaniesList(org int64, q, segment, stage string, in paging.Page) ([]CompanyListRow, int, string, error) {
	at, err := in.At(1)
	if err != nil {
		return nil, 0, "", err
	}
	db, people := s.companiesQuery(org, q, segment, stage)
	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, "", err
	}
	rows, err := s.companyRows(db, people, int(at[0]), in.Size())
	next := ""
	if end := at[0] + int64(in.Size()); end < total {
		next = paging.Cursor(end)
	}
	return rows, int(total), next, err
}

// companiesQuery is the companies the filters leave, and each company's standing from its people.
func (s *prospectingStore) companiesQuery(org int64, q, segment, stage string) (db, people *gorm.DB) {
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

func (s *prospectingStore) companyRows(db, people *gorm.DB, offset, limit int) ([]CompanyListRow, error) {
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

func (s *prospectingStore) Segments(org int64) ([]string, error) {
	var out []string
	return out, logged(s.in(org).Model(&Company{}).Where("segment <> ''").Distinct().Order("segment").Pluck("segment", &out))
}

func (s *prospectingStore) Week(org int64, now time.Time) Stats {
	since := now.AddDate(0, 0, -7)
	day := since.Format("2006-01-02")
	var st Stats
	// From the contacts' own dates, so history brought over from an import counts.
	for _, col := range []string{"linkedin_added", "email_sent", "followup_sent", "called"} {
		var n int64
		_ = logged(s.in(org).Model(&Contact{}).Where(col+" >= ?", day).Count(&n))
		st.Touches += n
	}
	_ = logged(s.in(org).Model(&Contact{}).Where("replied >= ?", day).Count(&st.Replies))
	_ = logged(s.in(org).Model(&Event{}).Where("at >= ? and action = ? and value = ?", since, "set:stage", "meeting").Count(&st.Meetings))
	_ = logged(s.in(org).Model(&Company{}).Where("created_at >= ? and found_by <> 'import'", since).Count(&st.Researched))
	return st
}

// AgentActivity is one page of what agents did, newest first.
func (s *prospectingStore) AgentActivity(org int64, page int) ([]AgentEvent, Page) {
	_, total := s.AgentEvents(org, 0, 0)
	pg := pageOf(page, activityPageSize, total)
	events, _ := s.AgentEvents(org, pg.offset(), pg.Size)
	return events, pg
}

// AgentEvents is limit of what agents did from offset, newest first, and how many they
// did in all; a limit of 0 reads none, only the count.
func (s *prospectingStore) AgentEvents(org int64, offset, limit int) ([]AgentEvent, int) {
	agents := func() *gorm.DB { return s.in(org).Model(&Event{}).Where(`"user" like 'agent:%'`) }
	var total int64
	_ = logged(agents().Count(&total))
	if limit == 0 {
		return nil, int(total)
	}
	var events []Event
	_ = logged(agents().Order("at desc, id desc").Limit(limit).Offset(offset).Find(&events))
	out := make([]AgentEvent, len(events))
	for i, e := range events {
		out[i] = AgentEvent{e.At, strings.TrimPrefix(e.User, "agent:"), e.Action, deref(e.Value)}
	}
	return out, int(total)
}

// AgentEventsBefore is limit of what agents did, newest first, before the event a cursor
// left off at (its time in microseconds and id; 0 is the start), with where the next
// page starts, 0 once there is none, and how many there are in all. It reads from where
// it left off rather than counting past every row before it.
func (s *prospectingStore) AgentEventsBefore(org, atMicro, id int64, limit int) (events []AgentEvent, nextAt, nextID int64, total int) {
	agents := func() *gorm.DB { return s.in(org).Model(&Event{}).Where(`"user" like 'agent:%'`) }
	var n int64
	_ = logged(agents().Count(&n))
	q := agents().Order("at desc, id desc").Limit(limit + 1)
	if atMicro > 0 {
		at := time.UnixMicro(atMicro)
		q = q.Where("at < ? or (at = ? and id < ?)", at, at, id)
	}
	var rows []Event
	_ = logged(q.Find(&rows))
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		nextAt, nextID = last.At.UnixMicro(), last.ID
	}
	events = make([]AgentEvent, len(rows))
	for i, e := range rows {
		events[i] = AgentEvent{e.At, strings.TrimPrefix(e.User, "agent:"), e.Action, deref(e.Value)}
	}
	return events, nextAt, nextID, int(n)
}

func (s *prospectingStore) QuickAddPerson(o *Org, in QuickAdd, actor string) (*Contact, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Linkedin = strings.TrimSpace(in.Linkedin)
	if in.Name == "" {
		return nil, errors.New("a name is needed")
	}
	if in.Linkedin != "" && !httpURL.MatchString(in.Linkedin) {
		return nil, errors.New("the LinkedIn link should start with https://")
	}
	if err := s.Allowed(o, in.Email, in.Linkedin); err != nil {
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

// Allowed applies the org's never-contact list to an email address or link.
func (s *prospectingStore) Allowed(o *Org, email, link string) error {
	return o.Settings.listed(email, link)
}

// NeverContactCovers names the people the list is keeping out of the outreach queues:
// those who would otherwise be open for a next step.
func (s *prospectingStore) NeverContactCovers(o *Org) []string {
	if strings.TrimSpace(o.Settings.NeverContact) == "" {
		return nil
	}
	var people []Contact
	_ = logged(s.in(o.ID).Where(openContacts).Where(companyNotRejected).Order("id").Find(&people))
	var names []string
	for _, p := range people {
		if o.Settings.listed(deref(p.Email), deref(p.Linkedin)) != nil {
			names = append(names, displayName(p))
		}
	}
	return names
}

// suggestDetails keeps what an agent sent for someone already here and that they
// don't have, and returns the fields now waiting. One already suggested, whether it
// is still waiting or was dismissed, isn't raised again. Someone matched by address
// or profile under another name gets nothing: the rest may be a colleague's.
func (s *prospectingStore) suggestDetails(tx *gorm.DB, who Actor, p Contact, in AddContactIn) ([]string, error) {
	if !strings.EqualFold(personKey(in.Name), personKey(displayName(p))) {
		return []string{}, nil
	}
	sent := map[string]string{"email": in.Email, "phone": in.Phone, "linkedin": in.Linkedin}
	var before []Suggestion
	if err := tx.Where("org_id = ? and contact_id = ?", p.OrgID, p.ID).Find(&before).Error; err != nil {
		return nil, err
	}
	waiting := []string{}
	for _, field := range suggestable {
		value := strings.TrimSpace(sent[field])
		if value == "" {
			continue
		}
		status := ""
		if field == "email" {
			status = in.EmailStatus
		}
		made, err := s.suggest(tx, who, p, before, field, value, status, strings.TrimSpace(in.SourceURL), "")
		if err != nil {
			return nil, err
		}
		if made || !sameDetail(field, value, detailOf(p, field)) && waitingFor(before, field, value) {
			waiting = append(waiting, field)
		}
	}
	return waiting, nil
}

// suggestOne keeps one detail found for someone, from a page or a vendor.
func (s *prospectingStore) suggestOne(tx *gorm.DB, who Actor, p Contact, field, value, status, source, vendor string) (bool, error) {
	var before []Suggestion
	if err := tx.Where("org_id = ? and contact_id = ?", p.OrgID, p.ID).Find(&before).Error; err != nil {
		return false, err
	}
	return s.suggest(tx, who, p, before, field, value, status, source, vendor)
}

// suggest keeps a detail they don't have that wasn't suggested before. An agent trying
// one guess after another mustn't bury the person's page.
func (s *prospectingStore) suggest(tx *gorm.DB, who Actor, p Contact, before []Suggestion, field, value, status, source, vendor string) (bool, error) {
	if sameDetail(field, value, detailOf(p, field)) || len(value) > maxDetail {
		return false, nil
	}
	pending := 0
	for _, b := range before {
		if b.Field == field && sameDetail(field, b.Value, value) {
			return false, nil
		}
		if b.Field == field && b.Decision == "" {
			pending++
		}
	}
	if pending >= maxWaiting {
		return false, nil
	}
	row := Suggestion{OrgID: p.OrgID, ContactID: p.ID, Field: field, Value: value, EmailStatus: status, SourceURL: source,
		Vendor: vendor, By: who.Name, KeyID: who.KeyID, At: time.Now()}
	if err := tx.Create(&row).Error; err != nil {
		return false, err
	}
	// The detail itself stays out of the history.
	return true, s.logAs(tx, p.OrgID, p.ID, p.CompanyID, who, "suggest:"+field, nil)
}

// Pending groups the suggestions still waiting by the person they are for.
func (s *prospectingStore) Pending(org int64, people []Contact) (map[int64][]PendingDetail, error) {
	out := map[int64][]PendingDetail{}
	if len(people) == 0 {
		return out, nil
	}
	byID := map[int64]Contact{}
	ids := make([]int64, len(people))
	for i, p := range people {
		byID[p.ID], ids[i] = p, p.ID
	}
	var rows []Suggestion
	if err := s.db.Where("org_id = ? and decision = '' and contact_id in ?", org, ids).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		// They have since been given it some other way.
		if sameDetail(r.Field, r.Value, detailOf(byID[r.ContactID], r.Field)) {
			continue
		}
		out[r.ContactID] = append(out[r.ContactID], PendingDetail{Suggestion: r, Current: detailOf(byID[r.ContactID], r.Field)})
	}
	return out, nil
}

// DecideSuggestion accepts a suggestion, which sets the detail as if the person had
// typed it, or dismisses it. Either way it is settled once.
func (s *prospectingStore) DecideSuggestion(org, id int64, decision, actor string) error {
	if decision != SuggestionAccepted && decision != SuggestionDismissed {
		return fmt.Errorf("unknown decision %q", decision)
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var sg Suggestion
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&sg).Error; err != nil {
			return missing(err, "no suggestion %d", id)
		}
		if sg.Decision != "" {
			return conflict("that suggestion was already " + sg.Decision + " by " + sg.DecidedBy)
		}
		now := time.Now()
		res := tx.Model(&Suggestion{}).Where("id = ? and decision = ''", sg.ID).
			Updates(map[string]any{"decision": decision, "decided_by": actor, "decided_at": &now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errors.New("someone else just settled that suggestion")
		}
		if decision == SuggestionDismissed {
			// A restore can leave a suggestion behind without its person.
			var c Contact
			if err := tx.Where("org_id = ? and id = ?", org, sg.ContactID).Limit(1).Find(&c).Error; err != nil {
				return err
			}
			return s.log(tx, org, c.ID, c.CompanyID, actor, "dismiss:"+sg.Field, nil)
		}
		if err := s.set(tx, org, sg.ContactID, sg.Field, sg.Value, actor); err != nil {
			return err
		}
		if sg.Field == "email" {
			return tx.Model(&Contact{}).Where("org_id = ? and id = ?", org, sg.ContactID).Update("email_status", sg.EmailStatus).Error
		}
		return nil
	})
}

// indexSearch never stops the server starting: without the index, search scans as it always did.
func (s *prospectingStore) indexSearch() {
	for _, stmt := range postgresSearch {
		if err := s.db.Exec(stmt).Error; err != nil {
			log.Printf("search index unavailable, searching by scan: %v", err)
			return
		}
	}
}

// term is already lower case.
func (s *prospectingStore) companyMatch(term string) (string, []any) {
	return pgCompanyText + " like ?", []any{"%" + term + "%"}
}

func (s *prospectingStore) contactMatch(term string, maker bool) (string, []any) {
	text := pgPersonText
	if maker {
		text = pgContactText
	}
	return text + " like ?", []any{"%" + term + "%"}
}

// heldWhenFound records how a known company stood when a task first came to it, so the
// task can say it wasn't a new find. What happens to it afterwards doesn't change that.
func (s *prospectingStore) heldWhenFound(tx *gorm.DB, org int64, who Actor, c *Company, stage string) error {
	if who.Task == 0 || stage == CompanyReview {
		return nil
	}
	var before int64
	if err := tx.Model(&Event{}).Where("org_id = ? and task_id = ? and company_id = ? and action not like 'note:%'", org, who.Task, c.ID).
		Count(&before).Error; err != nil || before > 0 {
		return err
	}
	why, err := heldAs(tx, org, c, stage)
	if err != nil {
		return err
	}
	return s.logAs(tx, org, 0, &c.ID, who, actionHeld, &why)
}

// heldWhenReached is heldWhenFound for a company an agent reaches through a fact or a
// person rather than by adding it, judged by the task's offering as add_company is.
func (s *prospectingStore) heldWhenReached(tx *gorm.DB, org int64, who Actor, c *Company) error {
	if who.Task == 0 {
		return nil
	}
	stage := c.Stage
	if who.Offering != 0 {
		stages, err := offeringStages(tx, org, who.Offering, []int64{c.ID})
		if err != nil {
			return err
		}
		stage = stages[c.ID]
	}
	return s.heldWhenFound(tx, org, who, c, stage)
}

// withheld counts a person refused for a task. Only the reason is kept: the event
// outlives an erasure and goes out in exports, so nothing in it may say who it was.
// The refusal stands whether or not this is written.
func (s *prospectingStore) withheld(org int64, who Actor, why string) {
	if who.Task == 0 {
		return
	}
	if err := s.logAs(s.db, org, 0, nil, who, actionWithheld, &why); err != nil {
		log.Printf("counting a refusal for task %d: %v", who.Task, err)
	}
}
