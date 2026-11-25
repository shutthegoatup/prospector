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
	Field     string `json:"field" jsonschema:"what the fact is about, e.g. team_page or press, or one of the things the guidance says this organisation looks for"`
	Value     string `json:"value" jsonschema:"the fact itself, one sentence"`
	SourceURL string `json:"source_url" jsonschema:"the page that shows it (required)"`
}

type AddCompanyIn struct {
	Name            string   `json:"name" jsonschema:"company name"`
	Domain          string   `json:"domain,omitempty" jsonschema:"the company's own website domain, e.g. example.com; leave empty rather than give a shared site"`
	Country         string   `json:"country,omitempty" jsonschema:"ISO country code"`
	Segment         string   `json:"segment,omitempty"`
	Fit             string   `json:"fit,omitempty" jsonschema:"suggested fit grade A, B or C"`
	ConfiguratorURL string   `json:"configurator_url,omitempty" jsonschema:"older clients only: add a fact instead"`
	Platform        string   `json:"platform,omitempty" jsonschema:"older clients only: add a fact instead"`
	BuiltBy         string   `json:"built_by,omitempty" jsonschema:"older clients only: add a fact instead"`
	Why             string   `json:"why,omitempty" jsonschema:"why it fits, one sentence"`
	Facts           []FactIn `json:"facts,omitempty" jsonschema:"facts with their sources"`
}

type AddCompanyOut struct {
	ID      int64  `json:"id"`
	Name    string `json:"name" jsonschema:"the name it is filed under, which can differ from yours if the domain was already known"`
	Existed bool   `json:"existed" jsonschema:"true if the company was already known; facts it didn't have were still added"`
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

// addFact stores a fact unless the company already has the same one from the same
// page, so an agent that runs again, or a second agent, doesn't double the list.
func addFact(tx *gorm.DB, org, company int64, in FactIn, actor string) (*Fact, bool, error) {
	f := Fact{OrgID: org, CompanyID: company, Field: strings.TrimSpace(in.Field), Value: strings.TrimSpace(in.Value),
		SourceURL: strings.TrimSpace(in.SourceURL), Actor: actor}
	var from []Fact
	if err := tx.Where("org_id = ? and company_id = ? and source_url = ?", org, company, f.SourceURL).Order("id").Find(&from).Error; err != nil {
		return nil, false, err
	}
	// Compared here because SQLite's lower() only folds ASCII.
	for i, had := range from {
		if strings.EqualFold(had.Field, f.Field) && strings.EqualFold(strings.TrimSpace(had.Value), f.Value) {
			return &from[i], true, nil
		}
	}
	return &f, false, tx.Create(&f).Error
}

func (s *Store) AgentAddCompany(org int64, who Actor, in AddCompanyIn) (*AddCompanyOut, error) {
	if in.ConfiguratorURL != "" && !httpURL.MatchString(strings.TrimSpace(in.ConfiguratorURL)) {
		return nil, errors.New("configurator_url must be an http(s) link")
	}
	for _, f := range in.Facts {
		if err := checkFact(f); err != nil {
			return nil, err
		}
	}
	var out AddCompanyOut
	err := s.db.Transaction(func(tx *gorm.DB) error {
		domain := bareDomain(in.Domain)
		var c *Company
		var named int64
		err := tx.Model(&Company{}).Where("org_id = ? and lower(name) = ?", org, strings.ToLower(strings.TrimSpace(in.Name))).Count(&named).Error
		if err == nil && named == 0 {
			c, err = companyAt(tx, org, domain)
		}
		if err != nil {
			return err
		}
		created := false
		if c == nil {
			c, created, err = ensureCompany(tx, org, Company{Name: in.Name, Domain: domain, Country: in.Country,
				Segment: in.Segment, Fit: in.Fit, Why: in.Why, FoundBy: who.Name, Stage: CompanyReview})
			if err != nil {
				return err
			}
		}
		out = AddCompanyOut{ID: c.ID, Name: c.Name, Existed: !created, Stage: c.Stage}
		added := 0
		for _, f := range in.Facts {
			_, had, err := addFact(tx, org, c.ID, f, who.Name)
			if err != nil {
				return err
			}
			if !had {
				added++
			}
		}
		// Older clients' fields become facts; with no configurator link they cite nothing, as they did as columns.
		older, err := addConfiguratorFacts(tx, org, c.ID, in.ConfiguratorURL, in.Platform, in.BuiltBy, who.Name)
		if err != nil {
			return err
		}
		added += older
		if !created && added == 0 {
			return nil
		}
		action := "add:company"
		if !created {
			action = "add:facts"
		}
		return s.logAs(tx, org, 0, &c.ID, who, action, str(fmt.Sprintf("%s, %d facts", c.Name, added)))
	})
	return &out, err
}

type AddFactIn struct {
	Company string `json:"company" jsonschema:"company id or exact name"`
	FactIn
}

type AddFactOut struct {
	Fact
	Existed bool `json:"existed" jsonschema:"true if the company already had this fact from this page; nothing was added"`
}

func (s *Store) AgentAddFact(org int64, who Actor, in AddFactIn) (*AddFactOut, error) {
	if err := checkFact(in.FactIn); err != nil {
		return nil, err
	}
	c, err := s.companyRef(org, in.Company)
	if err != nil {
		return nil, err
	}
	var out AddFactOut
	err = s.db.Transaction(func(tx *gorm.DB) error {
		f, had, err := addFact(tx, org, c.ID, in.FactIn, who.Name)
		if err != nil {
			return err
		}
		out = AddFactOut{Fact: *f, Existed: had}
		if had {
			return nil
		}
		return s.logAs(tx, org, 0, &c.ID, who, "add:fact", str(f.Field))
	})
	return &out, err
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
	ID      int64  `json:"id"`
	Status  string `json:"status" jsonschema:"suggested: waits in Research until a person picks them; empty once picked"`
	Existed bool   `json:"existed" jsonschema:"true if this person was already at the company; nothing was added or changed"`
}

// samePerson finds someone already at the company by profile, address or name, so a
// second suggestion of them doesn't become a second row to be contacted twice.
func samePerson(people []Contact, in AddContactIn) *Contact {
	link, email, name := linkedinPath(in.Linkedin), strings.ToLower(strings.TrimSpace(in.Email)), strings.ToLower(personKey(in.Name))
	for i, p := range people {
		if link != "" && linkedinPath(deref(p.Linkedin)) == link ||
			email != "" && strings.ToLower(deref(p.Email)) == email ||
			name != "" && strings.ToLower(personKey(displayName(p))) == name {
			return &people[i]
		}
	}
	return nil
}

func (s *Store) AgentAddContact(o *Org, who Actor, in AddContactIn) (*AddContactOut, error) {
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
	if err := erased(s.db, o.ID, in.Name, c.Name, in.Email, in.Linkedin); err != nil {
		return nil, err
	}
	person := strings.TrimSpace(in.Name)
	if in.Title != "" {
		person += ", " + strings.TrimSpace(in.Title)
	}
	first := firstField(in.Name)
	var out AddContactOut
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var people []Contact
		if err := tx.Where("org_id = ? and company_id = ? and key not like 'org:%'", o.ID, c.ID).Order("id").Find(&people).Error; err != nil {
			return err
		}
		if p := samePerson(people, in); p != nil {
			out = AddContactOut{ID: p.ID, Status: p.Status, Existed: true}
			return nil
		}
		contact := Contact{OrgID: o.ID, Key: uniqueKey(tx, o.ID, personKey(in.Name)), File: "added by " + who.Name,
			Person: &person, First: &first, Maker: &c.Name, CompanyID: &c.ID, Title: strings.TrimSpace(in.Title),
			Email: nonEmpty(strings.TrimSpace(in.Email)), EmailStatus: in.EmailStatus, Linkedin: nonEmpty(strings.TrimSpace(in.Linkedin)),
			Phone: nonEmpty(strings.TrimSpace(in.Phone)), Sources: str(strings.TrimSpace(in.SourceURL)), Status: ContactSuggested}
		contact.Tz = GuessTZ(in.Phone, "("+c.Country+")")
		if err := tx.Create(&contact).Error; err != nil {
			return err
		}
		out = AddContactOut{ID: contact.ID, Status: contact.Status}
		return s.logAs(tx, o.ID, contact.ID, &c.ID, who, "add:contact", &person)
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

// draftSent is the mark that says a draft's text has gone out. A LinkedIn message
// has no mark of its own.
func draftSent(c Contact, kind string) string {
	switch kind {
	case "note":
		return deref(c.LinkedinAdded)
	case "email":
		return deref(c.EmailSent)
	case "followup":
		return deref(c.FollowupSent)
	}
	return ""
}

// editedBy names the person whose edit is the last word on a draft field, or "" if
// an agent or nobody wrote what is there. The template note that picking fills in
// and imported text leave no edit, so an agent may still improve on them.
func editedBy(tx *gorm.DB, org, contact int64, field, kind string) (string, error) {
	var last Event
	err := tx.Where("org_id = ? and contact_id = ? and action in ?", org, contact, []string{"set:" + field, "draft:" + kind}).
		Order("at desc, id desc").First(&last).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && last.Action != "set:"+field {
		return "", nil
	}
	return last.User, err
}

func (s *Store) AgentDraft(org int64, who Actor, in DraftIn) error {
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
		if day := draftSent(c, in.Kind); day != "" {
			return fmt.Errorf("that went out on %s, so its text stays as sent", day)
		}
		current := map[string]*string{"note": c.Note, "message": c.Message, "body": c.Body, "followup": c.Followup}[field]
		if deref(current) != "" {
			person, err := editedBy(tx, org, c.ID, field, in.Kind)
			if err != nil {
				return err
			}
			if person != "" {
				return fmt.Errorf("%s edited this text, so it is theirs to change", person)
			}
		}
		updates := map[string]any{field: strings.TrimSpace(in.Text), "draft_by": who.Name}
		if in.Kind == "email" && in.Subject != "" {
			updates["subject"] = in.Subject
		}
		if err := tx.Model(&c).Updates(updates).Error; err != nil {
			return err
		}
		return s.logAs(tx, org, c.ID, c.CompanyID, who, "draft:"+in.Kind, nil)
	})
}

type SearchIn struct {
	Query string `json:"query" jsonschema:"words in a company, person, email or notes"`
}

type SearchHit struct {
	Kind      string     `json:"kind"`
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Company   string     `json:"company,omitempty"`
	Stage     string     `json:"stage"`
	LastAgent *LastAgent `json:"last_agent,omitempty" jsonschema:"the agent that last changed this company, if any"`
}

// LastAgent is the latest change an agent made to a company. People's actions
// don't count: someone qualifying a company is the cue to work on it.
type LastAgent struct {
	Actor string    `json:"actor"`
	At    time.Time `json:"at"`
	ByYou bool      `json:"by_you" jsonschema:"true if it was this key, perhaps in an earlier run"`
}

func (who Actor) did(e Event) bool {
	if e.KeyID != 0 && who.KeyID != 0 {
		return e.KeyID == who.KeyID
	}
	return e.User == who.Name
}

func (s *Store) lastAgents(org int64, who Actor, companies []int64) (map[int64]*LastAgent, error) {
	out := map[int64]*LastAgent{}
	if len(companies) == 0 {
		return out, nil
	}
	latest := s.db.Model(&Event{}).Select("max(id)").
		Where(`org_id = ? and company_id in ? and "user" like 'agent:%'`, org, companies).Group("company_id")
	var events []Event
	if err := s.in(org).Where("id in (?)", latest).Find(&events).Error; err != nil {
		return nil, err
	}
	for _, e := range events {
		out[*e.CompanyID] = &LastAgent{Actor: e.User, At: e.At, ByYou: who.did(e)}
	}
	return out, nil
}

func (s *Store) AgentSearch(org int64, who Actor, q string) ([]SearchHit, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	if len(q) < 2 {
		return nil, errors.New("search for at least two characters")
	}
	like := "%" + q + "%"
	var companies []Company
	if err := s.in(org).Where("lower(name) like ? or lower(domain) like ? or lower(why) like ?", like, like, like).Limit(25).Find(&companies).Error; err != nil {
		return nil, err
	}
	ids := make([]int64, len(companies))
	for i, c := range companies {
		ids[i] = c.ID
	}
	last, err := s.lastAgents(org, who, ids)
	if err != nil {
		return nil, err
	}
	hits := []SearchHit{}
	for _, c := range companies {
		hits = append(hits, SearchHit{Kind: "company", ID: c.ID, Name: c.Name, Stage: c.Stage, LastAgent: last[c.ID]})
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
	if p := c.Phase(); p != PhaseConversation {
		return string(p)
	}
	return c.ConversationStage()
}

type CompanyDetail struct {
	Company   Company       `json:"company"`
	Facts     []Fact        `json:"facts"`
	People    []PersonBrief `json:"people"`
	LastAgent *LastAgent    `json:"last_agent,omitempty" jsonschema:"the agent that last changed this company, if any"`
	History   []Happened    `json:"history" jsonschema:"what people and agents did here, newest first"`
}

type PersonBrief struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Email       string `json:"email,omitempty"`
	EmailStatus string `json:"email_status,omitempty"`
	Linkedin    string `json:"linkedin,omitempty"`
	Stage       string `json:"stage"`
	NextStep    string `json:"next_step,omitempty" jsonschema:"what the person working this conversation plans to do"`
	NextDate    string `json:"next_date,omitempty"`
	LostReason  string `json:"lost_reason,omitempty"`
}

// Happened is one line of a company's history as an agent sees it.
type Happened struct {
	At     time.Time `json:"at"`
	Actor  string    `json:"actor" jsonschema:"a person's name, or agent:<key name>"`
	Action string    `json:"action" jsonschema:"e.g. add:fact, add:contact, pick, company:rejected, draft:email, mark:replied"`
	Value  string    `json:"value,omitempty" jsonschema:"the day of a mark, or a conversation stage"`
	Person string    `json:"person,omitempty"`
	ByYou  bool      `json:"by_you,omitempty"`
}

const agentHistory = 20

func (s *Store) AgentCompany(org int64, who Actor, ref string) (*CompanyDetail, error) {
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
	out := &CompanyDetail{Company: *c, Facts: facts, People: []PersonBrief{}, History: []Happened{}}
	names := map[int64]string{}
	for _, p := range people {
		names[p.ID] = displayName(p)
		out.People = append(out.People, PersonBrief{ID: p.ID, Name: displayName(p), Title: p.Title, Email: deref(p.Email),
			EmailStatus: p.EmailStatus, Linkedin: deref(p.Linkedin), Stage: contactStage(p),
			NextStep: p.NextStep, NextDate: deref(p.NextDate), LostReason: p.LostReason})
	}
	last, err := s.lastAgents(org, who, []int64{c.ID})
	if err != nil {
		return nil, err
	}
	out.LastAgent = last[c.ID]
	var events []Event
	if err := s.in(org).Where("company_id = ?", c.ID).Order("at desc, id desc").Limit(agentHistory).Find(&events).Error; err != nil {
		return nil, err
	}
	for _, e := range events {
		// Only what erasure would keep: an event's value can hold a note or someone's details.
		out.History = append(out.History, Happened{At: e.At, Actor: e.User, Action: e.Action, Value: deref(keptValue(e)),
			Person: names[e.ContactID], ByYou: who.did(e)})
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
func agentInstructions(o *Org, k *APIKey) string {
	parts := []string{"Research and outreach data for " + o.Name + ". Companies and contacts you add wait in Research until a person qualifies them. Every fact and contact needs a source URL. Search before adding, to avoid duplicates. You can draft messages; only people send them.",
		"Other agents and people work the same companies. get_company returns the company's history: read it before researching, don't suggest again what a person rejected, and if last_agent shows another agent was there within the hour, take a different company. by_you marks what this key did, perhaps in an earlier run."}
	if k.Has("research:write") {
		parts = append(parts,
			"Companies and facts are high volume with simple judgement, so a small, fast model does them well. One claim per fact, from the page that shows it.",
			lookForGuidance(o),
			"Contacts need care more than speed, so use a capable model. Pick the person who owns the decision, not a general inbox. Confirm they hold the role now on a current page: an old bio, press release or speaker listing isn't enough. Set email_status to guess for any address inferred from a pattern. If you can't confirm someone, don't add them; add a team_page fact so a person can look.")
	}
	if k.Has("outcomes:write") {
		parts = append(parts,
			"Record an outcome with mark only when a person tells you it happened or you can see it yourself, and say how you know in note. Each mark shows as yours.")
	}
	if k.Has("drafts:write") {
		parts = append(parts,
			"Drafts need a capable model. Write to the person: say plainly what you offer and why it fits them, in a few sentences. Claim nothing a source doesn't show. LinkedIn notes are 300 characters at most.")
	}
	parts = append(parts, "People approve or reject what you add, and the agents page shows each key's record, so one key per agent keeps the record useful.")
	if g := strings.TrimSpace(o.Settings.AgentGuidance); g != "" {
		parts = append(parts, "From "+o.Name+":\n"+g)
	}
	return strings.Join(parts, "\n\n")
}

// lookForGuidance names the assets the organisation looks for, so agents record
// each under the field the Research page checks.
func lookForGuidance(o *Org) string {
	assets, _ := o.Settings.Assets()
	if len(assets) == 0 {
		return "Add any fact a source shows that helps judge fit."
	}
	lines := []string{o.Name + " looks for these at each company. When a source shows one, add a fact with exactly this field. Add nothing for one you didn't find:"}
	for _, a := range assets {
		lines = append(lines, "- "+a.Field+prefixed(": ", a.Hint))
	}
	return strings.Join(lines, "\n")
}

type GuidanceOut struct {
	Guidance string `json:"guidance"`
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

type MarkIn struct {
	ContactID int64  `json:"contact_id"`
	Step      string `json:"step" jsonschema:"linkedin (request sent), accepted, email, followup, call, replied, declined or hold"`
	Date      string `json:"date,omitempty" jsonschema:"YYYY-MM-DD it happened; today if empty"`
	Note      string `json:"note,omitempty" jsonschema:"how you know, e.g. 'Allan said he replied rudely'"`
}

// AgentMark records an outcome for a contact already in outreach. Suggested
// contacts haven't been picked by a person yet, so nothing has happened to them.
func (s *Store) AgentMark(org int64, who Actor, in MarkIn) error {
	date := strings.TrimSpace(in.Date)
	if date == "" {
		date = today()
	}
	return s.mark(org, in.ContactID, in.Step, date, who, strings.TrimSpace(in.Note))
}

// AgentMarks is, per contact and step, the agent behind the latest mark, so a
// person re-marking a step takes it back.
func (s *Store) AgentMarks(org int64, ids []int64) map[int64]map[string]string {
	out := map[int64]map[string]string{}
	if len(ids) == 0 {
		return out
	}
	var events []Event
	s.in(org).Where("contact_id in ? and action like 'mark:%'", ids).Order("at, id").Find(&events)
	for _, e := range events {
		step := strings.TrimPrefix(e.Action, "mark:")
		if out[e.ContactID] == nil {
			out[e.ContactID] = map[string]string{}
		}
		if strings.HasPrefix(e.User, "agent:") {
			out[e.ContactID][step] = e.User
		} else {
			delete(out[e.ContactID], step)
		}
	}
	return out
}
