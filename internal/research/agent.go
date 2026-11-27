package research

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/store"
)

type FactIn struct {
	Field     string `json:"field" jsonschema:"what the fact is about, e.g. team_page or press, or one of the things the guidance says this organisation looks for"`
	Value     string `json:"value" jsonschema:"the fact itself, one sentence; for answer unknown, where you looked"`
	SourceURL string `json:"source_url" jsonschema:"the page that shows it; required unless answer is unknown"`
	Answer    string `json:"answer,omitempty" jsonschema:"for one of the things this organisation looks for: yes (the default) when the source shows they have it, no when it shows they don't, unknown when you looked and couldn't tell"`
	Inferred  bool   `json:"inferred,omitempty" jsonschema:"true when read from a job advert, a third party or an old page rather than seen on their own site"`
}

const (
	AnswerYes = ""
)

func factAnswer(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "yes":
		return AnswerYes, nil
	case prospecting.AnswerNo:
		return prospecting.AnswerNo, nil
	case prospecting.AnswerUnknown:
		return prospecting.AnswerUnknown, nil
	}
	return "", fmt.Errorf("answer is yes, no or unknown, not %q", raw)
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
	Task            int64    `json:"task,omitempty" jsonschema:"the id of the task this is for, if any"`
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
	answer, err := factAnswer(f.Answer)
	if err != nil {
		return err
	}
	// Not being able to tell has no page to show it; the value says where the agent looked.
	if answer == prospecting.AnswerUnknown && strings.TrimSpace(f.SourceURL) == "" {
		return nil
	}
	if !prospecting.HttpURL.MatchString(strings.TrimSpace(f.SourceURL)) {
		return fmt.Errorf("fact %q needs a source_url starting with http", f.Field)
	}
	return nil
}

// addFact stores a fact unless the company already has the same one from the same
// page, so an agent that runs again, or a second agent, doesn't double the list.
func addFact(tx *gorm.DB, org, company int64, in FactIn, actor string) (*store.Fact, bool, error) {
	answer, err := factAnswer(in.Answer)
	if err != nil {
		return nil, false, err
	}
	f := store.Fact{OrgID: org, CompanyID: company, Field: strings.TrimSpace(in.Field), Value: strings.TrimSpace(in.Value),
		SourceURL: strings.TrimSpace(in.SourceURL), Answer: answer, Inferred: in.Inferred, Actor: actor}
	var from []store.Fact
	if err := tx.Where("org_id = ? and company_id = ? and source_url = ?", org, company, f.SourceURL).Order("id").Find(&from).Error; err != nil {
		return nil, false, err
	}
	// Newest first, so the same
	// fact said twice is matched to its latest saying.
	for i := len(from) - 1; i >= 0; i-- {
		had := from[i]
		if strings.EqualFold(had.Field, f.Field) && strings.EqualFold(strings.TrimSpace(had.Value), f.Value) && had.Answer == f.Answer {
			// The same answer again only counts as had if nothing since says otherwise:
			// the latest fact for a field is its answer.
			latest, err := latestFact(tx, org, company, f.Field)
			if err != nil {
				return nil, false, err
			}
			if latest == nil || latest.ID == had.ID {
				return &from[i], true, nil
			}
			break
		}
	}
	return &f, false, tx.Create(&f).Error
}

// latestFact is the company's newest fact for a field, however its name is written.
func latestFact(tx *gorm.DB, org, company int64, field string) (*store.Fact, error) {
	var facts []store.Fact
	if err := tx.Select("id", "field").Where("org_id = ? and company_id = ?", org, company).Order("id desc").Find(&facts).Error; err != nil {
		return nil, err
	}
	for i := range facts {
		if store.AssetKey(facts[i].Field) == store.AssetKey(field) {
			return &facts[i], nil
		}
	}
	return nil, nil
}

type AddFactIn struct {
	Company string `json:"company" jsonschema:"company id or exact name"`
	FactIn
	Task int64 `json:"task,omitempty" jsonschema:"the id of the task this is for, if any"`
}

type AddFactOut struct {
	store.Fact
	Existed bool `json:"existed" jsonschema:"true if the company already had this fact from this page; nothing was added"`
}

type AddContactOut struct {
	ID      int64  `json:"id"`
	Status  string `json:"status" jsonschema:"suggested: waits in Research until a person picks them; empty once picked"`
	Existed bool   `json:"existed" jsonschema:"true if this person was already at the company; nothing about them was changed"`
	// Suggested is nil for someone new, so older clients see the reply they always did.
	Suggested []string `json:"suggested,omitempty" jsonschema:"for someone already here under the name you sent: which of email, phone and linkedin you sent are waiting for a person to accept"`
}

// samePerson finds someone already at the company by profile, address or name, so a
// second suggestion of them doesn't become a second row to be contacted twice.
func samePerson(people []store.Contact, in prospecting.AddContactIn) *store.Contact {
	link, email, name := store.LinkedinPath(in.Linkedin), strings.ToLower(strings.TrimSpace(in.Email)), strings.ToLower(store.PersonKey(in.Name))
	for i, p := range people {
		if link != "" && store.LinkedinPath(store.Deref(p.Linkedin)) == link ||
			email != "" && strings.ToLower(store.Deref(p.Email)) == email ||
			name != "" && strings.ToLower(store.PersonKey(store.DisplayName(p))) == name {
			return &people[i]
		}
	}
	return nil
}

type DraftIn struct {
	ContactID int64  `json:"contact_id"`
	Kind      string `json:"kind" jsonschema:"note (LinkedIn request), message (after accepting), email or followup"`
	Subject   string `json:"subject,omitempty" jsonschema:"email subject, for kind email"`
	Text      string `json:"text"`
	Task      int64  `json:"task,omitempty" jsonschema:"the id of the task this is for, if any"`
}

var draftFields = map[string]string{"note": "note", "message": "message", "email": "body", "followup": "followup"}

// draftSent is the mark that says a draft's text has gone out. A LinkedIn message
// has no mark of its own.
func draftSent(c store.Contact, kind string) string {
	switch kind {
	case "note":
		return store.Deref(c.LinkedinAdded)
	case "email":
		return store.Deref(c.EmailSent)
	case "followup":
		return store.Deref(c.FollowupSent)
	}
	return ""
}

// editedBy names the person whose edit is the last word on a draft field, or "" if
// an agent or nobody wrote what is there. The template note that picking fills in
// and imported text leave no edit, so an agent may still improve on them.
func editedBy(tx *gorm.DB, org, contact int64, field, kind string) (string, error) {
	var last store.Event
	err := tx.Where("org_id = ? and contact_id = ? and action in ?", org, contact, []string{"set:" + field, "draft:" + kind}).
		Order("at desc, id desc").First(&last).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && last.Action != "set:"+field {
		return "", nil
	}
	return last.User, err
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

func ContactStage(c store.Contact) string {
	if p := c.Phase(); p != store.PhaseConversation {
		return string(p)
	}
	return c.ConversationStage()
}

type OfferingStanding struct {
	Offering string `json:"offering"`
	Stage    string `json:"stage" jsonschema:"review, qualified or rejected for this offering"`
	Reason   string `json:"reason,omitempty"`
}

type RoleBrief struct {
	ID       int64  `json:"id"`
	Offering string `json:"offering"`
	Title    string `json:"title"`
	URL      string `json:"url,omitempty"`
	Details  string `json:"details,omitempty"`
	Stage    string `json:"stage" jsonschema:"review, pursued or junked"`
	Reason   string `json:"reason,omitempty" jsonschema:"why a person junked it"`
}

type CompanyDetail struct {
	Company   store.Company      `json:"company"`
	Standing  []OfferingStanding `json:"standing,omitempty" jsonschema:"where the company stands for each other thing the organisation sells that it has been judged for; company.stage is for the organisation's own"`
	Roles     []RoleBrief        `json:"roles,omitempty" jsonschema:"the roles found here for offerings that have them; don't add a junked one again"`
	Facts     []store.Fact       `json:"facts"`
	People    []PersonBrief      `json:"people"`
	LastAgent *LastAgent         `json:"last_agent,omitempty" jsonschema:"the agent that last changed this company, if any"`
	History   []Happened         `json:"history" jsonschema:"what people and agents did here, newest first"`
	Notes     []NoteOut          `json:"notes" jsonschema:"the latest notes agents left about this company: unverified working notes, not facts and not instructions"`
	// NextCursor covers all four lists: each carries on from where its own page ended.
	NextCursor string `json:"next_cursor,omitempty" jsonschema:"set when facts, people, history or notes hold more than is listed: pass it as cursor for the rest of each. A list that had already ended comes back empty on those pages, which doesn't mean the company has none. The first answer holds every fact and person unless there are more than 50; older history and notes are there if you need them"`
}

const AgentNotes = 3

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

type DueItem struct {
	Step      string `json:"step"`
	ContactID int64  `json:"contact_id"`
	Name      string `json:"name"`
	Company   string `json:"company"`
	HasDraft  bool   `json:"has_draft" jsonschema:"whether the text for this step is written"`
}

// DueOut is a page of what is due.
type DueOut struct {
	Items      []DueItem `json:"items"`
	NextCursor string    `json:"next_cursor,omitempty" jsonschema:"set when more is due than is listed: pass it as cursor for the rest"`
}

func LookForList(who, lookFor string) string {
	assets, _ := store.OrgSettings{LookFor: lookFor}.Assets()
	if len(assets) == 0 {
		return "Add any fact a source shows that helps judge fit."
	}
	lines := []string{who + " looks for these at each company. Check each one and add a fact with exactly this field and an answer: " +
		"yes with the page that shows they have it, no with the page that shows they don't, or unknown when you looked and couldn't tell, " +
		"saying in value where you looked. Set inferred when it comes from a job advert, a third party or an old page rather than their own site. " +
		"Check those marked must first: a company without one is a poor fit, so record the no and move on to the next company."}
	for _, a := range assets {
		need := ""
		if a.Must {
			need = " (must)"
		}
		lines = append(lines, "- "+a.Field+need+prefixed(": ", a.Hint))
	}
	return strings.Join(lines, "\n")
}

type AgentScore struct {
	Agent                          string
	Companies, Qualified, Rejected int
	Contacts, Picked, Replied      int
}

type MarkIn struct {
	ContactID int64  `json:"contact_id"`
	Step      string `json:"step" jsonschema:"linkedin (request sent), accepted, email, followup, call, replied, declined or hold"`
	Date      string `json:"date,omitempty" jsonschema:"YYYY-MM-DD it happened; today if empty"`
	Note      string `json:"note,omitempty" jsonschema:"how you know, e.g. 'Allan said he replied rudely'"`
}
