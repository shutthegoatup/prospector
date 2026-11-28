package crm

import (
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/sales"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// CRMStore is the records: companies and the people at them, what is known of each,
// their history, and searching them.
type CRMStore struct {
	*store.Core
	// A person's next step and date are their conversation's deal's.
	Sales *sales.DealStore
}

func (s *CRMStore) CompanyCount(org int64) (int64, error) {
	var n int64
	return n, s.InOrg(org).Model(&store.Company{}).Count(&n).Error
}

// CompanyNotes is one page of the notes left on a company, newest first, and how many there are.
func (s *CRMStore) CompanyNotes(org, company int64, offset, limit int) ([]store.TaskNote, int64, error) {
	var total int64
	if err := s.InOrg(org).Model(&store.TaskNote{}).Where("company_id = ?", company).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var notes []store.TaskNote
	return notes, total, s.InOrg(org).Where("company_id = ?", company).Order("id desc").Limit(limit).Offset(offset).Find(&notes).Error
}

func (s *CRMStore) CompanyByID(org, id int64) (*store.Company, error) {
	var c store.Company
	return &c, s.InOrg(org).First(&c, id).Error
}

// CompanyRef resolves an agent's "company" argument: an id or a name.
func (s *CRMStore) CompanyRef(org int64, ref string) (*store.Company, error) {
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

func (s *CRMStore) Facts(org, company int64) ([]store.Fact, error) {
	var f []store.Fact
	return f, s.InOrg(org).Where("company_id = ?", company).Order("id").Find(&f).Error
}

func (s *CRMStore) People(org, company int64) ([]store.Contact, error) {
	var c []store.Contact
	return c, s.InOrg(org).Where("company_id = ? and key not like 'org:%'", company).Order("status desc, id").Find(&c).Error
}

// History is one page of what happened at a company, newest first.
func (s *CRMStore) History(org, company int64, page int) ([]HistoryItem, Page, error) {
	total, err := s.HistoryCount(org, company)
	if err != nil {
		return nil, Page{}, err
	}
	pg := PageOf(page, HistoryPageSize, total)
	out, err := s.HistoryAt(org, company, pg.Offset(), pg.Size)
	return out, pg, err
}

func (s *CRMStore) HistoryCount(org, company int64) (int, error) {
	var total int64
	err := s.InOrg(org).Model(&store.Event{}).Where("company_id = ?", company).Count(&total).Error
	return int(total), err
}

func (s *CRMStore) HistoryAt(org, company int64, offset, limit int) ([]HistoryItem, error) {
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
func (s *CRMStore) Touches(org, company int64) (int, error) {
	var n int64
	err := s.InOrg(org).Model(&store.Event{}).Where("company_id = ? and action in ?", company,
		[]string{"mark:linkedin", "mark:email", "mark:followup", "mark:call"}).Count(&n).Error
	return int(n), err
}

// PickContact moves a suggested person into outreach, with a note from the org's template.
func (s *CRMStore) PickContact(org int64, o *store.Org, id int64, actor string) error {
	return s.PickContactFor(org, o, id, nil, actor)
}

// PickContactFor picks someone for an offering, whatever they were suggested for, or for
// what they were suggested for when it is nil.
func (s *CRMStore) PickContactFor(org int64, o *store.Org, id int64, offeringID *int64, actor string) error {
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

func (s *CRMStore) Conversations(org int64) (map[string][]store.Contact, error) {
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

func (s *CRMStore) Companies(org int64, q, segment, stage string, page int) ([]CompanyListRow, Page, error) {
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
func (s *CRMStore) CompaniesList(org int64, q, segment, stage string, in paging.Page) ([]CompanyListRow, int, string, error) {
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
func (s *CRMStore) companiesQuery(org int64, q, segment, stage string) (db, people *gorm.DB) {
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

func (s *CRMStore) companyRows(db, people *gorm.DB, offset, limit int) ([]CompanyListRow, error) {
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
		out[i] = CompanyListRow{Company: c, Pipeline: PipelineAt(c, 0)}
		if j, ok := at[c.ID]; ok {
			out[i].People, out[i].Pipeline = standing[j].People, PipelineAt(c, standing[j].Furthest)
		}
	}
	return out, nil
}

func (s *CRMStore) Segments(org int64) ([]string, error) {
	var out []string
	return out, store.Logged(s.InOrg(org).Model(&store.Company{}).Where("segment <> ''").Distinct().Order("segment").Pluck("segment", &out))
}

func (s *CRMStore) Week(org int64, now time.Time) Stats {
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

func (s *CRMStore) QuickAddPerson(o *store.Org, in QuickAdd, actor string) (*store.Contact, error) {
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
			out.LinkedinAdded = new(store.Today())
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
func (s *CRMStore) Allowed(o *store.Org, email, link string) error {
	return o.Settings.Listed(email, link)
}

// NeverContactCovers names the people the list is keeping out of the outreach queues:
// those who would otherwise be open for a next step.
func (s *CRMStore) NeverContactCovers(o *store.Org) []string {
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

// IndexSearch never stops the server starting: without the index, search scans as it always did.
func (s *CRMStore) IndexSearch() {
	for _, stmt := range postgresSearch {
		if err := s.DB.Exec(stmt).Error; err != nil {
			log.Printf("search index unavailable, searching by scan: %v", err)
			return
		}
	}
}

// term is already lower case.
func (s *CRMStore) CompanyMatch(term string) (string, []any) {
	return pgCompanyText + " like ?", []any{"%" + term + "%"}
}

func (s *CRMStore) ContactMatch(term string, maker bool) (string, []any) {
	text := pgPersonText
	if maker {
		text = pgContactText
	}
	return text + " like ?", []any{"%" + term + "%"}
}
