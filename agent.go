package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Agent operations: the REST API and the MCP server both call these, with the
// organisation and actor taken from the API key, never from the request body.

type FactIn struct {
	Field     string `json:"field" jsonschema:"what the fact is about, e.g. configurator, built_by, team_page, press"`
	Value     string `json:"value" jsonschema:"the fact itself, one sentence"`
	SourceURL string `json:"source_url" jsonschema:"the page that shows it (required)"`
}

type AddCompanyIn struct {
	Name            string   `json:"name" jsonschema:"company name"`
	Domain          string   `json:"domain,omitempty" jsonschema:"website domain, e.g. example.com"`
	Country         string   `json:"country,omitempty" jsonschema:"ISO country code"`
	Segment         string   `json:"segment,omitempty"`
	Fit             string   `json:"fit,omitempty" jsonschema:"suggested fit grade A, B or C"`
	ConfiguratorURL string   `json:"configurator_url,omitempty"`
	Platform        string   `json:"platform,omitempty" jsonschema:"configurator platform if detected"`
	BuiltBy         string   `json:"built_by,omitempty" jsonschema:"agency or vendor that built it, if a source says so"`
	Why             string   `json:"why,omitempty" jsonschema:"why it fits, one sentence"`
	Facts           []FactIn `json:"facts,omitempty" jsonschema:"facts with their sources"`
}

type AddCompanyOut struct {
	ID      int64  `json:"id"`
	Existed bool   `json:"existed" jsonschema:"true if the company was already known; new facts were still added"`
	Stage   string `json:"stage"`
}

func checkFact(f FactIn) error {
	if strings.TrimSpace(f.Field) == "" || strings.TrimSpace(f.Value) == "" {
		return errors.New("a fact needs a field and a value")
	}
	if !httpURL.MatchString(strings.TrimSpace(f.SourceURL)) {
		return fmt.Errorf("fact %q needs a source_url starting with http", f.Field)
	}
	return nil
}

func (s *Store) AgentAddCompany(org int64, actor string, in AddCompanyIn) (*AddCompanyOut, error) {
	for _, f := range in.Facts {
		if err := checkFact(f); err != nil {
			return nil, err
		}
	}
	if in.ConfiguratorURL != "" && !httpURL.MatchString(strings.TrimSpace(in.ConfiguratorURL)) {
		return nil, errors.New("configurator_url must be an http(s) link")
	}
	var out AddCompanyOut
	err := s.db.Transaction(func(tx *gorm.DB) error {
		c, created, err := ensureCompany(tx, org, Company{Name: in.Name, Domain: strings.ToLower(in.Domain), Country: in.Country,
			Segment: in.Segment, Fit: in.Fit, ConfiguratorURL: in.ConfiguratorURL, Platform: in.Platform, BuiltBy: in.BuiltBy,
			Why: in.Why, FoundBy: actor, Stage: CompanyReview})
		if err != nil {
			return err
		}
		out = AddCompanyOut{ID: c.ID, Existed: !created, Stage: c.Stage}
		for _, f := range in.Facts {
			if err := tx.Create(&Fact{OrgID: org, CompanyID: c.ID, Field: f.Field, Value: f.Value, SourceURL: strings.TrimSpace(f.SourceURL), Actor: actor}).Error; err != nil {
				return err
			}
		}
		action := "add:company"
		if !created {
			action = "add:facts"
		}
		return s.log(tx, org, 0, &c.ID, actor, action, str(fmt.Sprintf("%s, %d facts", c.Name, len(in.Facts))))
	})
	return &out, err
}

type AddFactIn struct {
	Company string `json:"company" jsonschema:"company id or exact name"`
	FactIn
}

func (s *Store) AgentAddFact(org int64, actor string, in AddFactIn) (*Fact, error) {
	if err := checkFact(in.FactIn); err != nil {
		return nil, err
	}
	c, err := s.companyRef(org, in.Company)
	if err != nil {
		return nil, err
	}
	f := Fact{OrgID: org, CompanyID: c.ID, Field: in.Field, Value: in.Value, SourceURL: strings.TrimSpace(in.SourceURL), Actor: actor}
	return &f, s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&f).Error; err != nil {
			return err
		}
		return s.log(tx, org, 0, &c.ID, actor, "add:fact", str(in.Field))
	})
}

type AddContactIn struct {
	Company     string `json:"company" jsonschema:"company id or exact name"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Email       string `json:"email,omitempty"`
	EmailStatus string `json:"email_status,omitempty" jsonschema:"verified, guess or empty"`
	Linkedin    string `json:"linkedin,omitempty" jsonschema:"profile URL, found by search, never scraped"`
	Phone       string `json:"phone,omitempty"`
	SourceURL   string `json:"source_url" jsonschema:"page that shows this person has this role (required)"`
}

type AddContactOut struct {
	ID     int64  `json:"id"`
	Status string `json:"status" jsonschema:"suggested: waits in Research until a person picks them"`
}

func (s *Store) AgentAddContact(o *Org, actor string, in AddContactIn) (*AddContactOut, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, errors.New("name is required")
	}
	if !httpURL.MatchString(strings.TrimSpace(in.SourceURL)) {
		return nil, errors.New("source_url is required and must start with http")
	}
	if in.EmailStatus != "" && in.EmailStatus != "verified" && in.EmailStatus != "guess" {
		return nil, errors.New("email_status is verified, guess or empty")
	}
	if in.Linkedin != "" && !httpURL.MatchString(strings.TrimSpace(in.Linkedin)) {
		return nil, errors.New("linkedin must be an https link")
	}
	if err := s.allowed(o, in.Email, in.Linkedin); err != nil {
		return nil, err
	}
	c, err := s.companyRef(o.ID, in.Company)
	if err != nil {
		return nil, err
	}
	person := strings.TrimSpace(in.Name)
	if in.Title != "" {
		person += ", " + strings.TrimSpace(in.Title)
	}
	first := firstField(in.Name)
	var out AddContactOut
	err = s.db.Transaction(func(tx *gorm.DB) error {
		contact := Contact{OrgID: o.ID, Key: uniqueKey(tx, o.ID, personKey(in.Name)), File: "added by " + actor,
			Person: &person, First: &first, Maker: &c.Name, CompanyID: &c.ID, Title: strings.TrimSpace(in.Title),
			Email: nonEmpty(strings.TrimSpace(in.Email)), EmailStatus: in.EmailStatus, Linkedin: nonEmpty(strings.TrimSpace(in.Linkedin)),
			Phone: nonEmpty(strings.TrimSpace(in.Phone)), Sources: str(strings.TrimSpace(in.SourceURL)), Status: ContactSuggested}
		contact.Tz = GuessTZ(in.Phone, "("+c.Country+")")
		if err := tx.Create(&contact).Error; err != nil {
			return err
		}
		out = AddContactOut{ID: contact.ID, Status: contact.Status}
		return s.log(tx, o.ID, contact.ID, &c.ID, actor, "add:contact", &person)
	})
	return &out, err
}

type DraftIn struct {
	ContactID int64  `json:"contact_id"`
	Kind      string `json:"kind" jsonschema:"note (LinkedIn request), message (after accepting), email or followup"`
	Subject   string `json:"subject,omitempty" jsonschema:"email subject, for kind email"`
	Text      string `json:"text"`
}

var draftFields = map[string]string{"note": "note", "message": "message", "email": "body", "followup": "followup"}

func (s *Store) AgentDraft(org int64, actor string, in DraftIn) error {
	field, ok := draftFields[in.Kind]
	if !ok {
		return fmt.Errorf("kind is note, message, email or followup, not %q", in.Kind)
	}
	if strings.TrimSpace(in.Text) == "" {
		return errors.New("text is empty")
	}
	if in.Kind == "note" && len([]rune(in.Text)) > 300 {
		return errors.New("LinkedIn notes are 300 characters at most")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var c Contact
		if err := tx.Where("org_id = ? and id = ?", org, in.ContactID).First(&c).Error; err != nil {
			return fmt.Errorf("no contact %d", in.ContactID)
		}
		updates := map[string]any{field: strings.TrimSpace(in.Text), "draft_by": actor}
		if in.Kind == "email" && in.Subject != "" {
			updates["subject"] = in.Subject
		}
		if err := tx.Model(&c).Updates(updates).Error; err != nil {
			return err
		}
		return s.log(tx, org, c.ID, c.CompanyID, actor, "draft:"+in.Kind, nil)
	})
}

type SearchIn struct {
	Query string `json:"query" jsonschema:"words in a company, person, email or notes"`
}

type SearchHit struct {
	Kind    string `json:"kind"`
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Company string `json:"company,omitempty"`
	Stage   string `json:"stage"`
}

func (s *Store) AgentSearch(org int64, q string) ([]SearchHit, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	if len(q) < 2 {
		return nil, errors.New("search for at least two characters")
	}
	like := "%" + q + "%"
	var companies []Company
	if err := s.in(org).Where("lower(name) like ? or lower(domain) like ? or lower(why) like ?", like, like, like).Limit(25).Find(&companies).Error; err != nil {
		return nil, err
	}
	hits := []SearchHit{}
	for _, c := range companies {
		hits = append(hits, SearchHit{Kind: "company", ID: c.ID, Name: c.Name, Stage: c.Stage})
	}
	var people []Contact
	if err := s.in(org).Where("key not like 'org:%' and (lower(person) like ? or lower(email) like ? or lower(maker) like ?)", like, like, like).Limit(25).Find(&people).Error; err != nil {
		return nil, err
	}
	for _, p := range people {
		hits = append(hits, SearchHit{Kind: "contact", ID: p.ID, Name: displayName(p), Company: deref(p.Maker), Stage: contactStage(p)})
	}
	return hits, nil
}

func contactStage(c Contact) string {
	switch {
	case c.Status == ContactSuggested:
		return "suggested"
	case c.Declined != nil:
		return "declined"
	case c.Replied != nil:
		if c.Stage != "" {
			return c.Stage
		}
		return "replied"
	case c.Hold != nil:
		return "hold"
	case c.EmailSent != nil, c.LinkedinAdded != nil, c.Called != nil:
		return "in outreach"
	}
	return "not contacted"
}

type CompanyDetail struct {
	Company Company       `json:"company"`
	Facts   []Fact        `json:"facts"`
	People  []PersonBrief `json:"people"`
}

type PersonBrief struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Email       string `json:"email,omitempty"`
	EmailStatus string `json:"email_status,omitempty"`
	Linkedin    string `json:"linkedin,omitempty"`
	Stage       string `json:"stage"`
}

func (s *Store) AgentCompany(org int64, ref string) (*CompanyDetail, error) {
	c, err := s.companyRef(org, ref)
	if err != nil {
		return nil, err
	}
	facts, err := s.Facts(org, c.ID)
	if err != nil {
		return nil, err
	}
	people, err := s.People(org, c.ID)
	if err != nil {
		return nil, err
	}
	out := &CompanyDetail{Company: *c, Facts: facts, People: []PersonBrief{}}
	for _, p := range people {
		out.People = append(out.People, PersonBrief{ID: p.ID, Name: displayName(p), Title: p.Title, Email: deref(p.Email),
			EmailStatus: p.EmailStatus, Linkedin: deref(p.Linkedin), Stage: contactStage(p)})
	}
	return out, nil
}

type DueItem struct {
	Step      string `json:"step"`
	ContactID int64  `json:"contact_id"`
	Name      string `json:"name"`
	Company   string `json:"company"`
	HasDraft  bool   `json:"has_draft" jsonschema:"whether the text for this step is written"`
}

func (s *Store) AgentDue(o *Org) ([]DueItem, error) {
	items := []DueItem{}
	for _, step := range []string{"linkedin", "accepted", "email", "followup", "call"} {
		rows, err := s.Due(o.ID, step, time.Now(), o.Settings)
		if err != nil {
			return nil, err
		}
		for _, c := range rows {
			text := map[string]string{"linkedin": deref(c.Note), "accepted": deref(c.Message), "email": deref(c.Body), "followup": deref(c.Followup), "call": "x"}[step]
			items = append(items, DueItem{Step: step, ContactID: c.ID, Name: displayName(c), Company: deref(c.Maker), HasDraft: text != ""})
		}
	}
	return items, nil
}

// agentInstructions describes each task rather than naming a model: the client
// picks the model, and the agents page shows how each key's suggestions fare.
func agentInstructions(org string, k *APIKey) string {
	parts := []string{"Research and outreach data for " + org + ". Companies and contacts you add wait in Research until a person qualifies them. Every fact and contact needs a source URL. Search before adding, to avoid duplicates. You can draft messages; only people send them."}
	if k.Has("research:write") {
		parts = append(parts,
			"Companies and facts are high volume with simple judgement, so a small, fast model does them well. One claim per fact, from the page that shows it. Leave built_by and platform empty unless a source says so.",
			"Contacts need care more than speed, so use a capable model. Pick the person who owns the decision, not a general inbox. Confirm they hold the role now on a current page: an old bio, press release or speaker listing isn't enough. Set email_status to guess for any address inferred from a pattern. If you can't confirm someone, don't add them; add a team_page fact so a person can look.")
	}
	if k.Has("drafts:write") {
		parts = append(parts,
			"Drafts need a capable model. Write to the person: say plainly what you offer and why it fits them, in a few sentences. Claim nothing a source doesn't show. LinkedIn notes are 300 characters at most.")
	}
	parts = append(parts, "People approve or reject what you add, and the agents page shows each key's record, so one key per agent keeps the record useful.")
	return strings.Join(parts, "\n\n")
}

type AgentScore struct {
	Agent                          string
	Companies, Qualified, Rejected int
	Contacts, Picked, Replied      int
}

// AgentScores is each agent's record: how many of its companies people qualified
// or rejected, and how many of its contacts were picked and later replied.
func (s *Store) AgentScores(org int64) []AgentScore {
	by := map[string]*AgentScore{}
	get := func(actor string) *AgentScore {
		name := strings.TrimPrefix(actor, "agent:")
		if by[name] == nil {
			by[name] = &AgentScore{Agent: name}
		}
		return by[name]
	}
	var companies []struct {
		FoundBy, Stage string
		N              int
	}
	s.db.Raw(`select found_by, stage, count(*) as n from companies
		where org_id = ? and found_by like 'agent:%' group by found_by, stage`, org).Scan(&companies)
	for _, r := range companies {
		a := get(r.FoundBy)
		a.Companies += r.N
		switch r.Stage {
		case CompanyQualified:
			a.Qualified += r.N
		case CompanyRejected:
			a.Rejected += r.N
		}
	}
	var contacts []struct {
		Actor                     string
		Contacts, Picked, Replied int
	}
	s.db.Raw(`select e."user" as actor, count(*) as contacts,
		sum(case when exists (select 1 from events p where p.org_id = e.org_id and p.contact_id = e.contact_id and p.action = 'pick') then 1 else 0 end) as picked,
		sum(case when c.replied is not null then 1 else 0 end) as replied
		from events e join contacts c on c.id = e.contact_id
		where e.org_id = ? and e.action = 'add:contact' and e."user" like 'agent:%' group by e."user"`, org).Scan(&contacts)
	for _, r := range contacts {
		a := get(r.Actor)
		a.Contacts, a.Picked, a.Replied = r.Contacts, r.Picked, r.Replied
	}
	out := make([]AgentScore, 0, len(by))
	for _, a := range by {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Agent < out[j].Agent })
	return out
}
