package main

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
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
		// Someone added it between the look-up and the insert.
		err := tx.Where("org_id = ? and name = ?", org, c.Name).First(&found).Error
		return &found, false, err
	}
	return &c, true, nil
}

// companyAt finds the company an agent means by its website, so one company found
// under two spellings of its name stays one row. Imports don't use it: a group's
// brands can share a site.
func companyAt(tx *gorm.DB, org int64, domain string) (*Company, error) {
	if domain == "" {
		return nil, nil
	}
	var found Company
	err := tx.Where("org_id = ? and lower(domain) = ?", org, domain).Order("id").First(&found).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &found, err
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
		return nil, fmt.Errorf("no company %q in this organisation", ref)
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

func (s *Store) History(org, company int64, limit int) ([]HistoryItem, error) {
	var events []Event
	err := s.in(org).Where("company_id = ?", company).Order("at desc").Limit(limit).Find(&events).Error
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	var people []Contact
	s.in(org).Where("company_id = ?", company).Find(&people)
	for _, p := range people {
		names[p.ID] = displayName(p)
	}
	out := make([]HistoryItem, len(events))
	for i, e := range events {
		out[i] = HistoryItem{At: e.At, Actor: e.User, Action: e.Action, Value: deref(e.Value), Person: names[e.ContactID]}
	}
	return out, nil
}

// ResearchRow is one company in the Research tab with what's known about it.
type ResearchRow struct {
	Company
	Facts      []Fact
	People     []Contact
	Incomplete bool
}

var ResearchFilters = []struct{ Key, Label string }{
	{"review", "To review"}, {"contact", "Needs a contact"}, {"verify", "Needs verifying"}, {"rejected", "Rejected"},
}

func (s *Store) Research(org int64, filter string) ([]ResearchRow, error) {
	var companies []Company
	q := s.in(org).Model(&Company{})
	withPeople := s.db.Model(&Contact{}).Select("company_id").
		Where("org_id = ? and company_id is not null and key not like 'org:%' and status <> 'suggested'", org)
	switch filter {
	case "review":
		q = q.Where("stage = ?", CompanyReview).Order("created_at desc")
	case "contact":
		q = q.Where("stage = ? and hold is null and id not in (?)", CompanyQualified, withPeople).Order("fit, name")
	case "verify":
		guess := s.db.Model(&Contact{}).Select("company_id").
			Where("org_id = ? and company_id is not null and (email_status = 'guess' or flags like '%INFERRED%') and email_sent is null and hold is null", org)
		q = q.Where("id in (?)", guess).Order("name")
	case "rejected":
		q = q.Where("stage = ?", CompanyRejected).Order("updated_at desc")
	default:
		return nil, fmt.Errorf("unknown filter %q", filter)
	}
	if err := q.Limit(200).Find(&companies).Error; err != nil {
		return nil, err
	}
	rows := make([]ResearchRow, len(companies))
	for i, c := range companies {
		rows[i].Company = c
		rows[i].Facts, _ = s.Facts(org, c.ID)
		rows[i].People, _ = s.People(org, c.ID)
	}
	return rows, nil
}

func (s *Store) ResearchCounts(org int64) map[string]int {
	out := map[string]int{}
	for _, f := range ResearchFilters {
		rows, _ := s.Research(org, f.Key)
		out[f.Key] = len(rows)
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
			return fmt.Errorf("no company %d", id)
		}
		return s.log(tx, org, 0, &id, actor, "company:"+stage, nonEmpty(fit))
	})
}

// PickContact moves a suggested person into outreach, with a note from the org's template.
func (s *Store) PickContact(org int64, o *Org, id int64, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var c Contact
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
			return fmt.Errorf("no contact %d", id)
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

func (s *Store) Companies(org int64, q, segment, stage string) ([]CompanyListRow, error) {
	var companies []Company
	db := s.in(org).Model(&Company{})
	if q = strings.TrimSpace(strings.ToLower(q)); q != "" {
		like := "%" + q + "%"
		people := s.db.Model(&Contact{}).Select("company_id").Where("org_id = ? and (lower(person) like ? or lower(email) like ?)", org, like, like)
		db = db.Where("lower(name) like ? or lower(domain) like ? or lower(why) like ? or id in (?)", like, like, like, people)
	}
	if segment != "" {
		db = db.Where("segment = ?", segment)
	}
	if err := db.Order("name").Limit(500).Find(&companies).Error; err != nil {
		return nil, err
	}
	var contacts []Contact
	s.in(org).Where("company_id is not null and key not like 'org:%'").Find(&contacts)
	byCompany := map[int64][]Contact{}
	for _, c := range contacts {
		byCompany[*c.CompanyID] = append(byCompany[*c.CompanyID], c)
	}
	var out []CompanyListRow
	for _, c := range companies {
		row := CompanyListRow{Company: c, People: len(byCompany[c.ID]), Pipeline: pipeline(c, byCompany[c.ID])}
		if stage == "" || row.Pipeline == stage {
			out = append(out, row)
		}
	}
	return out, nil
}

var stageRankOrder = map[string]int{"research": 0, "rejected": 0, "outreach": 1, "replied": 2, "meeting": 3, "proposal": 4, "won": 5, "lost": 1}

// pipeline is where a company stands, from its own stage and its furthest contact.
func pipeline(c Company, people []Contact) string {
	best := "research"
	if c.Stage == CompanyRejected {
		best = "rejected"
	}
	for _, p := range people {
		st := ""
		switch {
		case p.Declined != nil:
			if p.Touched() || p.Replied != nil {
				st = "outreach"
			}
		case p.Replied != nil:
			st = p.ConversationStage()
		case p.Touched():
			st = "outreach"
		}
		if st != "" && stageRankOrder[st] >= stageRankOrder[best] {
			best = st
		}
	}
	return best
}

func (s *Store) Segments(org int64) []string {
	var out []string
	s.in(org).Model(&Company{}).Where("segment <> ''").Distinct().Order("segment").Pluck("segment", &out)
	return out
}

type Stats struct{ Touches, Replies, Meetings, Researched int64 }

func (s *Store) Week(org int64, now time.Time) Stats {
	since := now.AddDate(0, 0, -7)
	day := since.Format("2006-01-02")
	var st Stats
	// From the contacts' own dates, so history brought over from an import counts.
	for _, col := range []string{"linkedin_added", "email_sent", "followup_sent", "called"} {
		var n int64
		s.in(org).Model(&Contact{}).Where(col+" >= ?", day).Count(&n)
		st.Touches += n
	}
	s.in(org).Model(&Contact{}).Where("replied >= ?", day).Count(&st.Replies)
	s.in(org).Model(&Event{}).Where("at >= ? and action = ? and value = ?", since, "set:stage", "meeting").Count(&st.Meetings)
	s.in(org).Model(&Company{}).Where("created_at >= ? and found_by <> 'import'", since).Count(&st.Researched)
	return st
}

type AgentEvent struct {
	At     time.Time
	Actor  string
	Action string
	Value  string
}

func (s *Store) AgentActivity(org int64, limit int) []AgentEvent {
	var events []Event
	s.in(org).Where(`"user" like 'agent:%'`).Order("at desc").Limit(limit).Find(&events)
	out := make([]AgentEvent, len(events))
	for i, e := range events {
		out[i] = AgentEvent{e.At, strings.TrimPrefix(e.User, "agent:"), e.Action, deref(e.Value)}
	}
	return out
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
		return s.log(tx, o.ID, out.ID, companyID, actor, "add:person", nonEmpty(in.Linkedin))
	})
	return &out, err
}

func uniqueKey(tx *gorm.DB, org int64, key string) string {
	try := key
	for i := 2; ; i++ {
		var n int64
		tx.Model(&Contact{}).Where("org_id = ? and key = ?", org, try).Count(&n)
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
	s.in(o.ID).Where(openContacts).Where(companyNotRejected).Order("id").Find(&people)
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
