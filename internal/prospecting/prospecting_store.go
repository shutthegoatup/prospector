package prospecting

import (
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/sales"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// ProspectingStore is the prospecting area's queries: companies and the people at them,
// what is found out about each, and searching them.
type ProspectingStore struct {
	*store.Core
	// An organisation's settings decide who is left out, and a person's stage is the
	// conversation's deal's.
	Identity *identity.IdentityStore
	Sales    *sales.DealStore
}

func (s *ProspectingStore) CompanyCount(org int64) (int64, error) {
	var n int64
	return n, s.InOrg(org).Model(&store.Company{}).Count(&n).Error
}

// CompanyNotes is one page of the notes left on a company, newest first, and how many there are.
func (s *ProspectingStore) CompanyNotes(org, company int64, offset, limit int) ([]store.TaskNote, int64, error) {
	var total int64
	if err := s.InOrg(org).Model(&store.TaskNote{}).Where("company_id = ?", company).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var notes []store.TaskNote
	return notes, total, s.InOrg(org).Where("company_id = ?", company).Order("id desc").Limit(limit).Offset(offset).Find(&notes).Error
}

func (s *ProspectingStore) CompanyByID(org, id int64) (*store.Company, error) {
	var c store.Company
	return &c, s.InOrg(org).First(&c, id).Error
}

// CompanyRef resolves an agent's "company" argument: an id or a name.
func (s *ProspectingStore) CompanyRef(org int64, ref string) (*store.Company, error) {
	var c store.Company
	q := s.InOrg(org)
	var id int64
	if _, err := fmt.Sscan(ref, &id); err == nil && fmt.Sprint(id) == strings.TrimSpace(ref) {
		q = q.Where("id = ?", id)
	} else {
		q = q.Where("lower(name) = ?", strings.ToLower(strings.TrimSpace(ref)))
	}
	if err := q.First(&c).Error; err != nil {
		return nil, store.Missing(err, "no company %q in this organisation", ref)
	}
	return &c, nil
}

func (s *ProspectingStore) Facts(org, company int64) ([]store.Fact, error) {
	var f []store.Fact
	return f, s.InOrg(org).Where("company_id = ?", company).Order("id").Find(&f).Error
}

func (s *ProspectingStore) People(org, company int64) ([]store.Contact, error) {
	var c []store.Contact
	return c, s.InOrg(org).Where("company_id = ? and key not like 'org:%'", company).Order("status desc, id").Find(&c).Error
}

// History is one page of what happened at a company, newest first.
func (s *ProspectingStore) History(org, company int64, page int) ([]HistoryItem, Page, error) {
	total, err := s.HistoryCount(org, company)
	if err != nil {
		return nil, Page{}, err
	}
	pg := PageOf(page, HistoryPageSize, total)
	out, err := s.HistoryAt(org, company, pg.Offset(), pg.Size)
	return out, pg, err
}

func (s *ProspectingStore) HistoryCount(org, company int64) (int, error) {
	var total int64
	err := s.InOrg(org).Model(&store.Event{}).Where("company_id = ?", company).Count(&total).Error
	return int(total), err
}

func (s *ProspectingStore) HistoryAt(org, company int64, offset, limit int) ([]HistoryItem, error) {
	var events []store.Event
	if err := s.InOrg(org).Where("company_id = ?", company).Order("at desc, id desc").Limit(limit).Offset(offset).Find(&events).Error; err != nil {
		return nil, err
	}
	names := map[int64]string{}
	var people []store.Contact
	if err := s.InOrg(org).Where("company_id = ?", company).Find(&people).Error; err != nil {
		return nil, err
	}
	for _, p := range people {
		names[p.ID] = store.DisplayName(p)
	}
	out := make([]HistoryItem, len(events))
	for i, e := range events {
		out[i] = HistoryItem{At: e.At, Actor: e.User, Action: e.Action, Value: store.Deref(e.Value), Person: names[e.ContactID]}
	}
	return out, nil
}

// Touches counts every time someone at a company was reached out to, whichever page
// of its history is on show.
func (s *ProspectingStore) Touches(org, company int64) (int, error) {
	var n int64
	err := s.InOrg(org).Model(&store.Event{}).Where("company_id = ? and action in ?", company,
		[]string{"mark:linkedin", "mark:email", "mark:followup", "mark:call"}).Count(&n).Error
	return int(n), err
}

// researchQuery is the companies under one of Research's filters, for one offering, 0
// being the organisation's own: its stages are the companies' own, another's are its
// verdicts. People count towards the offering they are being reached for.
func (s *ProspectingStore) researchQuery(org, offering int64, filter string, task int64) (*gorm.DB, string, error) {
	q := s.InOrg(org).Model(&store.Company{})
	if task != 0 {
		// What a task found is what was done for it, or noted for it, at a company.
		done := s.DB.Model(&store.Event{}).Select("company_id").Where("org_id = ? and task_id = ?", org, task)
		noted := s.DB.Model(&store.TaskNote{}).Select("company_id").Where("org_id = ? and task_id = ?", org, task)
		q = q.Where("(id in (?) or id in (?))", done, noted)
	}
	people := s.DB.Model(&store.Contact{}).Select("company_id").Where("org_id = ? and company_id is not null and offering_id = ?", org, offering)
	staged := func(stage string) *gorm.DB {
		if offering == 0 {
			return q.Where("stage = ?", stage)
		}
		return q.Where("id in (?)", s.DB.Model(&store.Verdict{}).Select("company_id").Where("org_id = ? and offering_id = ? and stage = ?", org, offering, stage))
	}
	// A company the never-contact list covers is kept, but nobody is asked to review it
	// or find someone there.
	if filter == "review" || filter == "contact" {
		o, err := s.Identity.OrgByID(org)
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
		waiting := people.Where("id in (?)", s.DB.Model(&store.Suggestion{}).Select("contact_id").Where("org_id = ? and decision = ''", org))
		return q.Where("id in (?)", waiting), "name, id", nil
	case "rejected":
		if offering == 0 {
			return staged(CompanyRejected), "updated_at desc, id desc", nil
		}
		latest := fmt.Sprintf("(select v.updated_at from verdicts v where v.org_id = companies.org_id and v.company_id = companies.id and v.offering_id = %d) desc, id desc", offering)
		return staged(CompanyRejected), latest, nil
	}
	return nil, "", say.Errorf("unknown filter %q", filter)
}

// researchRows fills in what is known about each company, in a query a table rather
// than a few a company.
func (s *ProspectingStore) researchRows(org int64, companies []store.Company) ([]ResearchRow, error) {
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
	var facts []store.Fact
	if err := s.InOrg(org).Where("company_id in ?", ids).Order("id").Find(&facts).Error; err != nil {
		return nil, err
	}
	var people []store.Contact
	if err := s.InOrg(org).Where("company_id in ? and key not like 'org:%'", ids).Order("status desc, id").Find(&people).Error; err != nil {
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

// DetailsToCheck can't be paged in SQL: whether a suggestion still needs checking
// depends on comparing it with what the person holds now, the way a person would.
func (s *ProspectingStore) DetailsToCheck(org, offering, task int64) ([]ResearchRow, error) {
	q, order, err := s.researchQuery(org, offering, "details", task)
	if err != nil {
		return nil, err
	}
	var companies []store.Company
	if err := q.Order(order).Limit(500).Find(&companies).Error; err != nil {
		return nil, err
	}
	rows, err := s.researchRows(org, companies)
	// A suggestion the person was since given some other way leaves nothing to check.
	return slices.DeleteFunc(rows, func(r ResearchRow) bool { return len(r.Pending) == 0 }), err
}

func (s *ProspectingStore) Research(org int64, filter string, page int) ([]ResearchRow, Page, error) {
	return s.researchFor(org, filter, page, 0)
}

// researchFor is Research for the organisation's own offering, narrowed to what one task
// found, or all of it for task 0.
func (s *ProspectingStore) researchFor(org int64, filter string, page int, task int64) ([]ResearchRow, Page, error) {
	const offering = 0
	if filter == "details" {
		rows, err := s.DetailsToCheck(org, offering, task)
		if err != nil {
			return nil, Page{}, err
		}
		pg := PageOf(page, ResearchPageSize, len(rows))
		return rows[pg.Offset():min(len(rows), pg.Offset()+pg.Size)], pg, nil
	}
	q, order, err := s.researchQuery(org, offering, filter, task)
	if err != nil {
		return nil, Page{}, err
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, Page{}, err
	}
	pg := PageOf(page, ResearchPageSize, int(total))
	var companies []store.Company
	if err := q.Order(order).Limit(pg.Size).Offset(pg.Offset()).Find(&companies).Error; err != nil {
		return nil, pg, err
	}
	rows, err := s.researchRows(org, companies)
	return rows, pg, err
}

// rankedRows are all of one search's companies to review, best fit first: fit is worked
// out from facts, so they are ranked here rather than in SQL. A search with more than
// searchFitCap gives none, and false.
func (s *ProspectingStore) rankedRows(org, offering int64, set store.OrgSettings, task int64) ([]ResearchRow, bool, error) {
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
	var companies []store.Company
	if err := q.Order(order).Limit(searchFitCap).Find(&companies).Error; err != nil {
		return nil, false, err
	}
	rows, err := s.researchRows(org, companies)
	if err != nil {
		return nil, false, err
	}
	fits := make(map[int64]Fit, len(rows))
	for _, r := range rows {
		fits[r.ID] = FitOf(AssetsFound(set, r.Facts))
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
func (s *ProspectingStore) ResearchList(org, offering int64, set store.OrgSettings, filter string, task int64, in paging.Page) ([]ResearchRow, bool, string, error) {
	switch {
	case filter == "review" && task != 0:
		rows, ranked, err := s.rankedRows(org, offering, set, task)
		if err != nil {
			return nil, false, "", err
		}
		if ranked {
			page, next, err := paging.ListPage(rows, in.Limit, in.Cursor)
			return page, true, next, err
		}
	case filter == "details":
		rows, err := s.DetailsToCheck(org, offering, task)
		if err != nil {
			return nil, false, "", err
		}
		page, next, err := paging.ListPage(rows, in.Limit, in.Cursor)
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
	var companies []store.Company
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

func (s *ProspectingStore) ResearchCount(org int64, filter string) int {
	return s.researchCount(org, 0, filter, 0)
}

// DetailsCount is how many companies detailsToCheck lists, from the same companies in
// the same order, reading only their people and what was suggested for them: not the
// facts and lookups the list shows of each.
func (s *ProspectingStore) DetailsCount(org, offering, task int64) (int, error) {
	q, order, err := s.researchQuery(org, offering, "details", task)
	if err != nil {
		return 0, err
	}
	var ids []int64
	if err := q.Order(order).Limit(500).Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
		return 0, err
	}
	var people []store.Contact
	if err := s.InOrg(org).Where("company_id in ? and key not like 'org:%'", ids).Find(&people).Error; err != nil {
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

func (s *ProspectingStore) researchCount(org, offering int64, filter string, task int64) int {
	if filter == "details" {
		n, _ := s.DetailsCount(org, offering, task)
		return n
	}
	q, _, err := s.researchQuery(org, offering, filter, task)
	if err != nil {
		return 0
	}
	var n int64
	_ = store.Logged(q.Count(&n))
	return int(n)
}

func (s *ProspectingStore) ResearchCounts(org int64) map[string]int {
	return s.ResearchCountsFor(org, 0, 0)
}

func (s *ProspectingStore) ResearchCountsFor(org, offering, task int64) map[string]int {
	out := map[string]int{}
	for _, f := range ResearchFilters {
		out[f.Key] = s.researchCount(org, offering, f.Key, task)
	}
	return out
}

func (s *ProspectingStore) SetCompanyStage(org, id int64, stage, fit, actor string) error {
	if stage != CompanyQualified && stage != CompanyRejected && stage != CompanyReview {
		return say.Errorf("unknown stage %q", stage)
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{"stage": stage, "found_for": 0}
		if fit != "" {
			updates["fit"] = fit
		}
		res := tx.Model(&store.Company{}).Where("org_id = ? and id = ?", org, id).Updates(updates)
		if res.Error != nil || res.RowsAffected == 0 {
			return store.Missing(res.Error, "no company %d", id)
		}
		return s.Log(tx, org, 0, &id, actor, "company:"+stage, store.NonEmpty(fit))
	})
}

// PickContact moves a suggested person into outreach, with a note from the org's template.
func (s *ProspectingStore) PickContact(org int64, o *store.Org, id int64, actor string) error {
	return s.PickContactFor(org, o, id, nil, actor)
}

// PickContactFor picks someone for an offering, whatever they were suggested for, or for
// what they were suggested for when it is nil.
func (s *ProspectingStore) PickContactFor(org int64, o *store.Org, id int64, offeringID *int64, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var c store.Contact
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
			return store.Missing(err, "no contact %d", id)
		}
		updates := map[string]any{"status": ""}
		if offeringID != nil && *offeringID != c.OfferingID {
			// The offering is held before the person, as an agent adding them holds it.
			if *offeringID != 0 {
				if _, err := HeldOffering(tx, org, *offeringID); err != nil {
					return err
				}
			}
			// Only while still suggested, checked as it is written, so two picks for different
			// offerings can't both land.
			res := tx.Model(&store.Contact{}).Where("org_id = ? and id = ? and status = ? and offering_id = ?", org, id, store.ContactSuggested, c.OfferingID).
				Update("offering_id", *offeringID)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return store.Conflict{Text: say.Errorf("%s is already being reached for another offering", store.DisplayName(c))}
			}
			c.OfferingID = *offeringID
		}
		template := o.Settings.WithDefaults().NoteTemplate
		var offering *store.Offering
		if c.OfferingID != 0 {
			var err error
			if offering, err = HeldOffering(tx, org, c.OfferingID); err != nil {
				return err
			}
			if offering.NoteTemplate != "" {
				template = offering.NoteTemplate
			}
		}
		if store.Deref(c.Note) == "" {
			updates["note"] = Fill(template, store.Deref(c.First), store.CompanyName(store.Deref(c.Maker)), o.Name)
		}
		if err := tx.Model(&c).Updates(updates).Error; err != nil {
			return err
		}
		if c.CompanyID != nil && offering != nil {
			if err := qualifyFor(tx, org, offering, *c.CompanyID, actor); err != nil {
				return err
			}
		} else if c.CompanyID != nil {
			if err := FoundForOwn(tx, org, *c.CompanyID); err != nil {
				return err
			}
			res := tx.Model(&store.Company{}).Where("org_id = ? and id = ? and stage = ?", org, *c.CompanyID, CompanyReview).Update("stage", CompanyQualified)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected > 0 {
				if err := s.Log(tx, org, 0, c.CompanyID, actor, "company:"+CompanyQualified, nil); err != nil {
					return err
				}
			}
		}
		return s.Log(tx, org, id, c.CompanyID, actor, "pick", nil)
	})
}

func (s *ProspectingStore) Conversations(org int64) (map[string][]store.Contact, error) {
	var rows []store.Contact
	if err := s.InOrg(org).Where(store.InConversation).Order("next_date nulls first, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string][]store.Contact{}
	for _, c := range rows {
		out[c.ConversationStage()] = append(out[c.ConversationStage()], c)
	}
	return out, nil
}

func (s *ProspectingStore) Companies(org int64, q, segment, stage string, page int) ([]CompanyListRow, Page, error) {
	db, people := s.companiesQuery(org, q, segment, stage)
	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, Page{}, err
	}
	pg := PageOf(page, CompaniesPageSize, int(total))
	rows, err := s.companyRows(db, people, pg.Offset(), pg.Size)
	return rows, pg, err
}

// CompaniesList is a page of the Companies list for the API, with how many match in all.
func (s *ProspectingStore) CompaniesList(org int64, q, segment, stage string, in paging.Page) ([]CompanyListRow, int, string, error) {
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
func (s *ProspectingStore) companiesQuery(org int64, q, segment, stage string) (db, people *gorm.DB) {
	people = s.DB.Model(&store.Contact{}).Select("company_id, count(*) as people, "+furthestSQL+" as furthest").
		Where("org_id = ? and company_id is not null and key not like 'org:%'", org).Group("company_id")
	db = s.DB.Model(&store.Company{}).Where("companies.org_id = ?", org)
	if q = strings.TrimSpace(strings.ToLower(q)); q != "" {
		// A union, so each half is answered by its own search index.
		company, args := s.CompanyMatch(q)
		person, personArgs := s.ContactMatch(q, false)
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

func (s *ProspectingStore) companyRows(db, people *gorm.DB, offset, limit int) ([]CompanyListRow, error) {
	var companies []store.Company
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

func (s *ProspectingStore) Segments(org int64) ([]string, error) {
	var out []string
	return out, store.Logged(s.InOrg(org).Model(&store.Company{}).Where("segment <> ''").Distinct().Order("segment").Pluck("segment", &out))
}

func (s *ProspectingStore) Week(org int64, now time.Time) Stats {
	since := now.AddDate(0, 0, -7)
	day := since.Format("2006-01-02")
	var st Stats
	// From the contacts' own dates, so history brought over from an import counts.
	for _, col := range []string{"linkedin_added", "email_sent", "followup_sent", "called"} {
		var n int64
		_ = store.Logged(s.InOrg(org).Model(&store.Contact{}).Where(col+" >= ?", day).Count(&n))
		st.Touches += n
	}
	_ = store.Logged(s.InOrg(org).Model(&store.Contact{}).Where("replied >= ?", day).Count(&st.Replies))
	_ = store.Logged(s.InOrg(org).Model(&store.Event{}).Where("at >= ? and action = ? and value = ?", since, "set:stage", "meeting").Count(&st.Meetings))
	_ = store.Logged(s.InOrg(org).Model(&store.Company{}).Where("created_at >= ? and found_by <> 'import'", since).Count(&st.Researched))
	return st
}

// AgentActivity is one page of what agents did, newest first.
func (s *ProspectingStore) AgentActivity(org int64, page int) ([]AgentEvent, Page) {
	_, total := s.AgentEvents(org, 0, 0)
	pg := PageOf(page, ActivityPageSize, total)
	events, _ := s.AgentEvents(org, pg.Offset(), pg.Size)
	return events, pg
}

// AgentEvents is limit of what agents did from offset, newest first, and how many they
// did in all; a limit of 0 reads none, only the count.
func (s *ProspectingStore) AgentEvents(org int64, offset, limit int) ([]AgentEvent, int) {
	agents := func() *gorm.DB { return s.InOrg(org).Model(&store.Event{}).Where(`"user" like 'agent:%'`) }
	var total int64
	_ = store.Logged(agents().Count(&total))
	if limit == 0 {
		return nil, int(total)
	}
	var events []store.Event
	_ = store.Logged(agents().Order("at desc, id desc").Limit(limit).Offset(offset).Find(&events))
	out := make([]AgentEvent, len(events))
	for i, e := range events {
		out[i] = AgentEvent{e.At, strings.TrimPrefix(e.User, "agent:"), e.Action, store.Deref(e.Value)}
	}
	return out, int(total)
}

// AgentEventsBefore is limit of what agents did, newest first, before the event a cursor
// left off at (its time in microseconds and id; 0 is the start), with where the next
// page starts, 0 once there is none, and how many there are in all. It reads from where
// it left off rather than counting past every row before it.
func (s *ProspectingStore) AgentEventsBefore(org, atMicro, id int64, limit int) (events []AgentEvent, nextAt, nextID int64, total int) {
	agents := func() *gorm.DB { return s.InOrg(org).Model(&store.Event{}).Where(`"user" like 'agent:%'`) }
	var n int64
	_ = store.Logged(agents().Count(&n))
	q := agents().Order("at desc, id desc").Limit(limit + 1)
	if atMicro > 0 {
		at := time.UnixMicro(atMicro)
		q = q.Where("at < ? or (at = ? and id < ?)", at, at, id)
	}
	var rows []store.Event
	_ = store.Logged(q.Find(&rows))
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		nextAt, nextID = last.At.UnixMicro(), last.ID
	}
	events = make([]AgentEvent, len(rows))
	for i, e := range rows {
		events[i] = AgentEvent{e.At, strings.TrimPrefix(e.User, "agent:"), e.Action, store.Deref(e.Value)}
	}
	return events, nextAt, nextID, int(n)
}

func (s *ProspectingStore) QuickAddPerson(o *store.Org, in QuickAdd, actor string) (*store.Contact, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Linkedin = strings.TrimSpace(in.Linkedin)
	if in.Name == "" {
		return nil, say.New("a name is needed")
	}
	if in.Linkedin != "" && !HttpURL.MatchString(in.Linkedin) {
		return nil, say.New("the LinkedIn link should start with https://")
	}
	if err := s.Allowed(o, in.Email, in.Linkedin); err != nil {
		return nil, err
	}
	if err := ErasedIn(s.DB, o.ID, in.Name, in.Company, in.Email, in.Linkedin); err != nil {
		return nil, err
	}
	var out store.Contact
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := store.HoldOrg(tx, o.ID); err != nil {
			return err
		}
		var companyID *int64
		maker := strings.TrimSpace(in.Company)
		if maker != "" {
			c, _, err := EnsureCompany(tx, o.ID, store.Company{Name: maker, Stage: CompanyQualified, FoundBy: actor})
			if err != nil {
				return err
			}
			companyID = &c.ID
		}
		first := store.FirstField(in.Name)
		note := strings.TrimSpace(in.Note)
		if note == "" {
			note = Fill(o.Settings.WithDefaults().NoteTemplate, first, maker, o.Name)
		}
		person := in.Name
		if in.Title != "" {
			person += ", " + strings.TrimSpace(in.Title)
		}
		out = store.Contact{OrgID: o.ID, Key: UniqueKey(tx, o.ID, store.PersonKey(in.Name)), File: "added by " + actor,
			Person: &person, First: &first, Maker: store.NonEmpty(maker), Linkedin: store.NonEmpty(in.Linkedin),
			Email: store.NonEmpty(strings.TrimSpace(in.Email)), Note: &note, Title: strings.TrimSpace(in.Title), CompanyID: companyID}
		if in.Added {
			out.LinkedinAdded = store.Str(store.Today())
		}
		if err := tx.Create(&out).Error; err != nil {
			return err
		}
		if err := s.Log(tx, o.ID, out.ID, companyID, actor, "add:person", nil); err != nil {
			return err
		}
		if strings.TrimSpace(in.Note) != "" {
			// Recorded as an edit so an agent's draft doesn't replace a note the person wrote.
			return s.Log(tx, o.ID, out.ID, companyID, actor, "set:note", nil)
		}
		return nil
	})
	return &out, err
}

// Allowed applies the org's never-contact list to an email address or link.
func (s *ProspectingStore) Allowed(o *store.Org, email, link string) error {
	return o.Settings.Listed(email, link)
}

// NeverContactCovers names the people the list is keeping out of the outreach queues:
// those who would otherwise be open for a next step.
func (s *ProspectingStore) NeverContactCovers(o *store.Org) []string {
	if strings.TrimSpace(o.Settings.NeverContact) == "" {
		return nil
	}
	var people []store.Contact
	_ = store.Logged(s.InOrg(o.ID).Where(openContacts).Where(companyNotRejected).Order("id").Find(&people))
	var names []string
	for _, p := range people {
		if o.Settings.Listed(store.Deref(p.Email), store.Deref(p.Linkedin)) != nil {
			names = append(names, store.DisplayName(p))
		}
	}
	return names
}

// SuggestDetails keeps what an agent sent for someone already here and that they
// don't have, and returns the fields now waiting. One already suggested, whether it
// is still waiting or was dismissed, isn't raised again. Someone matched by address
// or profile under another name gets nothing: the rest may be a colleague's.
func (s *ProspectingStore) SuggestDetails(tx *gorm.DB, who store.Actor, p store.Contact, in AddContactIn) ([]string, error) {
	if !strings.EqualFold(store.PersonKey(in.Name), store.PersonKey(store.DisplayName(p))) {
		return []string{}, nil
	}
	sent := map[string]string{"email": in.Email, "phone": in.Phone, "linkedin": in.Linkedin}
	var before []store.Suggestion
	if err := tx.Where("org_id = ? and contact_id = ?", p.OrgID, p.ID).Find(&before).Error; err != nil {
		return nil, err
	}
	waiting := []string{}
	for _, field := range Suggestable {
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
		if made || !sameDetail(field, value, DetailOf(p, field)) && waitingFor(before, field, value) {
			waiting = append(waiting, field)
		}
	}
	return waiting, nil
}

// suggestOne keeps one detail found for someone, from a page or a vendor.
func (s *ProspectingStore) suggestOne(tx *gorm.DB, who store.Actor, p store.Contact, field, value, status, source, vendor string) (bool, error) {
	var before []store.Suggestion
	if err := tx.Where("org_id = ? and contact_id = ?", p.OrgID, p.ID).Find(&before).Error; err != nil {
		return false, err
	}
	return s.suggest(tx, who, p, before, field, value, status, source, vendor)
}

// suggest keeps a detail they don't have that wasn't suggested before. An agent trying
// one guess after another mustn't bury the person's page.
func (s *ProspectingStore) suggest(tx *gorm.DB, who store.Actor, p store.Contact, before []store.Suggestion, field, value, status, source, vendor string) (bool, error) {
	if sameDetail(field, value, DetailOf(p, field)) || len(value) > MaxDetail {
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
	if pending >= MaxWaiting {
		return false, nil
	}
	row := store.Suggestion{OrgID: p.OrgID, ContactID: p.ID, Field: field, Value: value, EmailStatus: status, SourceURL: source,
		Vendor: vendor, By: who.Name, KeyID: who.KeyID, At: time.Now()}
	if err := tx.Create(&row).Error; err != nil {
		return false, err
	}
	// The detail itself stays out of the history.
	return true, s.LogAs(tx, p.OrgID, p.ID, p.CompanyID, who, "suggest:"+field, nil)
}

// Pending groups the suggestions still waiting by the person they are for.
func (s *ProspectingStore) Pending(org int64, people []store.Contact) (map[int64][]PendingDetail, error) {
	out := map[int64][]PendingDetail{}
	if len(people) == 0 {
		return out, nil
	}
	byID := map[int64]store.Contact{}
	ids := make([]int64, len(people))
	for i, p := range people {
		byID[p.ID], ids[i] = p, p.ID
	}
	var rows []store.Suggestion
	if err := s.DB.Where("org_id = ? and decision = '' and contact_id in ?", org, ids).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		// They have since been given it some other way.
		if sameDetail(r.Field, r.Value, DetailOf(byID[r.ContactID], r.Field)) {
			continue
		}
		out[r.ContactID] = append(out[r.ContactID], PendingDetail{Suggestion: r, Current: DetailOf(byID[r.ContactID], r.Field)})
	}
	return out, nil
}

// DecideSuggestion accepts a suggestion, which sets the detail as if the person had
// typed it, or dismisses it. Either way it is settled once.
func (s *ProspectingStore) DecideSuggestion(org, id int64, decision, actor string) error {
	if decision != SuggestionAccepted && decision != SuggestionDismissed {
		return say.Errorf("unknown decision %q", decision)
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var sg store.Suggestion
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&sg).Error; err != nil {
			return store.Missing(err, "no suggestion %d", id)
		}
		if sg.Decision != "" {
			return store.Conflict{Text: say.Errorf("that suggestion was already %s by %s", sg.Decision, sg.DecidedBy)}
		}
		now := time.Now()
		res := tx.Model(&store.Suggestion{}).Where("id = ? and decision = ''", sg.ID).
			Updates(map[string]any{"decision": decision, "decided_by": actor, "decided_at": &now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return say.New("someone else just settled that suggestion")
		}
		if decision == SuggestionDismissed {
			// A restore can leave a suggestion behind without its person.
			var c store.Contact
			if err := tx.Where("org_id = ? and id = ?", org, sg.ContactID).Limit(1).Find(&c).Error; err != nil {
				return err
			}
			return s.Log(tx, org, c.ID, c.CompanyID, actor, "dismiss:"+sg.Field, nil)
		}
		if err := s.set(tx, org, sg.ContactID, sg.Field, sg.Value, actor); err != nil {
			return err
		}
		if sg.Field == "email" {
			return tx.Model(&store.Contact{}).Where("org_id = ? and id = ?", org, sg.ContactID).Update("email_status", sg.EmailStatus).Error
		}
		return nil
	})
}

// IndexSearch never stops the server starting: without the index, search scans as it always did.
func (s *ProspectingStore) IndexSearch() {
	for _, stmt := range postgresSearch {
		if err := s.DB.Exec(stmt).Error; err != nil {
			log.Printf("search index unavailable, searching by scan: %v", err)
			return
		}
	}
}

// term is already lower case.
func (s *ProspectingStore) CompanyMatch(term string) (string, []any) {
	return pgCompanyText + " like ?", []any{"%" + term + "%"}
}

func (s *ProspectingStore) ContactMatch(term string, maker bool) (string, []any) {
	text := pgPersonText
	if maker {
		text = pgContactText
	}
	return text + " like ?", []any{"%" + term + "%"}
}

// HeldWhenFound records how a known company stood when a task first came to it, so the
// task can say it wasn't a new find. What happens to it afterwards doesn't change that.
func (s *ProspectingStore) HeldWhenFound(tx *gorm.DB, org int64, who store.Actor, c *store.Company, stage string) error {
	if who.Task == 0 || stage == CompanyReview {
		return nil
	}
	var before int64
	if err := tx.Model(&store.Event{}).Where("org_id = ? and task_id = ? and company_id = ? and action not like 'note:%'", org, who.Task, c.ID).
		Count(&before).Error; err != nil || before > 0 {
		return err
	}
	why, err := heldAs(tx, org, c, stage)
	if err != nil {
		return err
	}
	return s.LogAs(tx, org, 0, &c.ID, who, ActionHeld, &why)
}

// HeldWhenReached is heldWhenFound for a company an agent reaches through a fact or a
// person rather than by adding it, judged by the task's offering as add_company is.
func (s *ProspectingStore) HeldWhenReached(tx *gorm.DB, org int64, who store.Actor, c *store.Company) error {
	if who.Task == 0 {
		return nil
	}
	stage := c.Stage
	if who.Offering != 0 {
		stages, err := OfferingStages(tx, org, who.Offering, []int64{c.ID})
		if err != nil {
			return err
		}
		stage = stages[c.ID]
	}
	return s.HeldWhenFound(tx, org, who, c, stage)
}

// Withheld counts a person refused for a task. Only the reason is kept: the event
// outlives an erasure and goes out in exports, so nothing in it may say who it was.
// The refusal stands whether or not this is written.
func (s *ProspectingStore) Withheld(org int64, who store.Actor, why string) {
	if who.Task == 0 {
		return
	}
	if err := s.LogAs(s.DB, org, 0, nil, who, ActionWithheld, &why); err != nil {
		log.Printf("counting a refusal for task %d: %v", who.Task, err)
	}
}
