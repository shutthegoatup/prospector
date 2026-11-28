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
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// ProspectingStore is the prospecting area's queries: companies and the people at them,
// what is found out about each, and searching them.
type ProspectingStore struct {
	*store.Core
	// An organisation's settings decide who is left out.
	Identity *identity.IdentityStore
	// The records it finds and qualifies are the CRM's.
	CRM *CRMStore
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
		return s.Log(tx, org, 0, &id, actor, store.CompanyTo.Of(stage), store.NonEmpty(fit))
	})
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
	agents := func() *gorm.DB { return s.InOrg(org).Model(&store.Event{}).Where(`"user" like ?`, store.AgentsLike) }
	var total int64
	_ = store.Logged(agents().Count(&total))
	if limit == 0 {
		return nil, int(total)
	}
	var events []store.Event
	_ = store.Logged(agents().Order("at desc, id desc").Limit(limit).Offset(offset).Find(&events))
	out := make([]AgentEvent, len(events))
	for i, e := range events {
		out[i] = AgentEvent{e.At, store.AgentName(e.User), e.Action, store.Deref(e.Value)}
	}
	return out, int(total)
}

// AgentEventsBefore is limit of what agents did, newest first, before the event a cursor
// left off at (its time in microseconds and id; 0 is the start), with where the next
// page starts, 0 once there is none, and how many there are in all. It reads from where
// it left off rather than counting past every row before it.
func (s *ProspectingStore) AgentEventsBefore(org, atMicro, id int64, limit int) (events []AgentEvent, nextAt, nextID int64, total int) {
	agents := func() *gorm.DB { return s.InOrg(org).Model(&store.Event{}).Where(`"user" like ?`, store.AgentsLike) }
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
		events[i] = AgentEvent{e.At, store.AgentName(e.User), e.Action, store.Deref(e.Value)}
	}
	return events, nextAt, nextID, int(n)
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
	return true, s.LogAs(tx, p.OrgID, p.ID, p.CompanyID, who, store.Suggested.Of(field), nil)
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
			if sg.Decision == SuggestionAccepted {
				return store.Conflict{Text: say.Errorf("that suggestion was already accepted by %s", sg.DecidedBy)}
			}
			return store.Conflict{Text: say.Errorf("that suggestion was already dismissed by %s", sg.DecidedBy)}
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
			return s.Log(tx, org, c.ID, c.CompanyID, actor, store.Dismissed.Of(sg.Field), nil)
		}
		if err := s.CRM.SetContactField(tx, org, sg.ContactID, sg.Field, sg.Value, actor); err != nil {
			return err
		}
		if sg.Field == "email" {
			return tx.Model(&store.Contact{}).Where("org_id = ? and id = ?", org, sg.ContactID).Update("email_status", sg.EmailStatus).Error
		}
		return nil
	})
}

// HeldWhenFound records how a known company stood when a task first came to it, so the
// task can say it wasn't a new find. What happens to it afterwards doesn't change that.
func (s *ProspectingStore) HeldWhenFound(tx *gorm.DB, org int64, who store.Actor, c *store.Company, stage string) error {
	if who.Task == 0 || stage == CompanyReview {
		return nil
	}
	var before int64
	if err := tx.Model(&store.Event{}).Where("org_id = ? and task_id = ? and company_id = ? and action not like ?", org, who.Task, c.ID, store.Noted.Like()).
		Count(&before).Error; err != nil || before > 0 {
		return err
	}
	why, err := heldAs(tx, org, c, stage)
	if err != nil {
		return err
	}
	return s.LogAs(tx, org, 0, &c.ID, who, store.ActTaskHeld, &why)
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
	if err := s.LogAs(s.DB, org, 0, nil, who, store.ActTaskWithheld, &why); err != nil {
		log.Printf("counting a refusal for task %d: %v", who.Task, err)
	}
}
