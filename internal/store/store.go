package store

import (
	"log"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/say"
)

type User struct {
	ID           int64  `gorm:"primaryKey"`
	Email        string `gorm:"uniqueIndex;not null"`
	Name         string
	PasswordHash string
	// MustChangePassword is set while the password is one somebody else has seen.
	MustChangePassword bool `gorm:"not null;default:false"`
	// Language is the one they chose, which outranks their browser's; empty is none.
	Language  string `gorm:"not null;default:''"`
	CreatedAt time.Time
}

// OrgSettings is what differs between organisations: timings, wording and rules.
type OrgSettings struct {
	// CustomerLanguage is what the organisation speaks to someone outside it, and to a
	// member who hasn't chosen and whose browser asks for nothing Prospector has;
	// empty is the source language.
	CustomerLanguage  string `json:"customer_language,omitempty"`
	EmailAfterDays    int    `json:"email_after_days"`
	FollowupAfterDays int    `json:"followup_after_days"`
	Pitch             string `json:"pitch"`
	NoteTemplate      string `json:"note_template"`
	NeverContact      string `json:"never_contact"`
	IncorporatedOnly  bool   `json:"incorporated_only"`
	AgentGuidance     string `json:"agent_guidance"`
	EnquirySites      string `json:"enquiry_sites"`
	// LookFor is what this organisation looks for at a company, one "field: hint"
	// per line; see Assets.
	LookFor string `json:"look_for"`
	// OnboardingSteps is what a new customer goes through, one step a line; see Onboarding.
	OnboardingSteps string `json:"onboarding_steps"`
	// Currency is the three-letter code deals are valued in.
	Currency string `json:"currency"`
	// PostalAddress is where the organisation can be written to. Marketing mail has to say it.
	PostalAddress string `json:"postal_address"`
}

func (s OrgSettings) WithDefaults() OrgSettings {
	if s.EmailAfterDays == 0 {
		s.EmailAfterDays = 2
	}
	if s.FollowupAfterDays == 0 {
		s.FollowupAfterDays = 6
	}
	if s.Pitch == "" {
		s.Pitch = "We help companies like {company} with [WHAT YOU DO]."
	}
	if s.NoteTemplate == "" {
		s.NoteTemplate = "Hi {first},\n\nI work on {org}. [ONE LINE ABOUT {company}].\n\nWould be glad to connect."
	}
	if s.Currency == "" {
		s.Currency = DefaultCurrency
	}
	if s.CustomerLanguage == "" {
		s.CustomerLanguage = say.Source
	}
	return s
}

type Org struct {
	ID        int64       `gorm:"primaryKey"`
	Slug      string      `gorm:"uniqueIndex;not null"`
	Name      string      `gorm:"not null"`
	Color     string      `gorm:"not null;default:'#56c5f4'"`
	Settings  OrgSettings `gorm:"serializer:json"`
	CreatedAt time.Time
}

type Membership struct {
	ID         int64 `gorm:"primaryKey"`
	OrgID      int64 `gorm:"uniqueIndex:idx_member;not null"`
	UserID     int64 `gorm:"uniqueIndex:idx_member;not null"`
	Role       string
	CallerName string
	Signature  string
	Areas      string `gorm:"not null;default:'sales support'"`
}

type Session struct {
	ID        int64  `gorm:"primaryKey"`
	TokenHash string `gorm:"uniqueIndex;not null"`
	UserID    int64  `gorm:"index"`
	ExpiresAt time.Time
	// SSOOrgs are the organisations whose own sign-in this session has been through
	// (see proofs). A Limited session began at one and opens nothing else.
	SSOOrgs string `gorm:"not null;default:''"`
	Limited bool   `gorm:"not null;default:false"`
}

type APIKey struct {
	ID         int64  `gorm:"primaryKey"`
	OrgID      int64  `gorm:"index;not null"`
	Name       string `gorm:"not null"`
	Prefix     string
	Hash       string `gorm:"uniqueIndex;not null"`
	Scopes     string
	CreatedBy  string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

type Company struct {
	ID           int64   `gorm:"primaryKey" json:"id"`
	OrgID        int64   `gorm:"uniqueIndex:idx_org_company;index:idx_companies_org_stage,priority:1;not null" json:"-"`
	Name         string  `gorm:"uniqueIndex:idx_org_company;not null" json:"name"`
	Domain       string  `json:"domain"`
	Country      string  `json:"country"`
	Segment      string  `json:"segment"`
	Fit          string  `json:"fit"`
	Stage        string  `gorm:"index;index:idx_companies_org_stage,priority:2" json:"stage"`
	Why          string  `json:"why"`
	FoundBy      string  `json:"found_by"`
	Incorporated *bool   `json:"incorporated"`
	Hold         *string `json:"hold"`
	// FoundFor is the offering a company was first found for: until the organisation's
	// own offering looks at it, it stays out of the organisation's own review.
	FoundFor  int64     `gorm:"not null;default:0" json:"found_for"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Fact struct {
	ID        int64  `gorm:"primaryKey" json:"id"`
	OrgID     int64  `gorm:"index;not null" json:"-"`
	CompanyID int64  `gorm:"index" json:"company_id"`
	ContactID *int64 `json:"contact_id,omitempty"`
	Field     string `json:"field"`
	Value     string `json:"value"`
	SourceURL string `json:"source_url"`
	// Answer is, for something the organisation looks for, whether the company has it:
	// empty for yes, "no", or "unknown" when the agent looked and couldn't tell.
	Answer string `gorm:"not null;default:''" json:"answer,omitempty"`
	// Inferred is read from a job advert, a third party or an old page, not seen on
	// the company's own site.
	Inferred  bool      `gorm:"not null;default:false" json:"inferred,omitempty"`
	Actor     string    `json:"actor"`
	CreatedAt time.Time `json:"created_at"`
}

// Contact mirrors the Python tracker's contacts table column for column (see Columns),
// so leads/outreach-db.sql moves between the two unchanged; the fields after it are new.
type Contact struct {
	ID            int64   `gorm:"primaryKey" json:"id"`
	Key           string  `gorm:"uniqueIndex:idx_org_key;not null" json:"key"`
	File          string  `gorm:"not null" json:"file"`
	Section       *string `json:"section"`
	Fit           *string `json:"fit"`
	Maker         *string `json:"maker"`
	Person        *string `json:"person"`
	First         *string `json:"first"`
	Linkedin      *string `json:"linkedin"`
	Email         *string `json:"email"`
	Note          *string `json:"note"`
	Subject       *string `json:"subject"`
	Body          *string `json:"body"`
	Followup      *string `json:"followup"`
	Flags         *string `json:"flags"`
	LinkedinAdded *string `json:"linkedin_added"`
	EmailSent     *string `json:"email_sent"`
	FollowupSent  *string `json:"followup_sent"`
	Replied       *string `json:"replied"`
	Declined      *string `json:"declined"`
	Hold          *string `json:"hold"`
	Sources       *string `json:"sources"`
	Phone         *string `json:"phone"`
	Called        *string `json:"called"`
	CallNotes     *string `json:"call_notes"`
	Tz            *string `json:"tz"`
	SendTo        *string `json:"send_to"`
	Accepted      *string `json:"accepted"`
	Message       *string `json:"message"`

	OrgID       int64   `gorm:"uniqueIndex:idx_org_key;index:idx_contacts_org_company,priority:1;not null" json:"-"`
	CompanyID   *int64  `gorm:"index;index:idx_contacts_org_company,priority:2" json:"company_id"`
	Status      string  `gorm:"not null;default:''" json:"status"`
	Title       string  `json:"title"`
	EmailStatus string  `json:"email_status"`
	Stage       string  `json:"stage"`
	NextStep    string  `json:"next_step"`
	NextDate    *string `json:"next_date"`
	LostReason  string  `json:"lost_reason"`
	DraftBy     string  `json:"draft_by"`
	// OfferingID is what they are being reached for; 0 is the organisation's own. Their
	// company being rejected for that offering takes them out of the queues.
	OfferingID int64 `gorm:"not null;default:0" json:"offering_id"`
}

// ContactSuggested is a person an agent found; a human picks them before outreach.
const ContactSuggested = "suggested"

// Event records who did what. User is a person's full name (their first name in
// older rows), "agent:<key name>" or "cli".
type Event struct {
	ID        int64  `gorm:"primaryKey"`
	OrgID     int64  `gorm:"index;index:idx_events_company_at,priority:1;index:idx_events_org_at,priority:1"`
	ContactID int64  `gorm:"index"`
	CompanyID *int64 `gorm:"index;index:idx_events_company_at,priority:2"`
	User      string
	// KeyID is the API key behind an agent's action, since key names can repeat; 0 for people.
	KeyID int64 `gorm:"not null;default:0"`
	// TaskID is the task an action was done for, 0 when for none.
	TaskID int64 `gorm:"index;not null;default:0"`
	Action string
	Value  *string
	At     time.Time `gorm:"index;index:idx_events_company_at,priority:3;index:idx_events_org_at,priority:2"`
}

func Deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Actor is who an operation is recorded against: a name ("agent:<key name>" for an
// agent), the key behind it, and the task it is working on, if any.
type Actor struct {
	Task  int64
	Name  string
	KeyID int64
	// ReadOnly is a key that can add nothing, so its reading a task isn't work on it.
	ReadOnly bool
	// Session is the MCP session calling, if any: two clients on one key are two agents.
	Session string
	// Offering is what Task is finding companies for; 0 is the organisation's own.
	Offering int64
}

// Core is what every area's queries need: the database and the means to keep secrets.
type Core struct {
	DB *gorm.DB
	// keys seal what is stored but mustn't be readable from the database alone; nil
	// when the server was given none.
	Keyring *Keyring
	// dns says whether a domain takes mail; nil asks nothing, so tests never reach the network.
	DNS MailDNS
}

// InOrg scopes a query to one organisation. Not every query uses it, so what holds the
// store's exported methods to their organisation is TestNoMethodReachesIntoAnotherOrganisation.
func (s *Core) InOrg(org int64) *gorm.DB { return s.DB.Where("org_id = ?", org) }

func (s *Core) Log(tx *gorm.DB, org, contact int64, company *int64, actor string, action Action, value *string) error {
	return s.LogAs(tx, org, contact, company, Actor{Name: actor}, action, value)
}

func (s *Core) LogAs(tx *gorm.DB, org, contact int64, company *int64, who Actor, action Action, value *string) error {
	e := Event{OrgID: org, ContactID: contact, CompanyID: company, User: who.Name, KeyID: who.KeyID, TaskID: who.Task, Action: action.String(), Value: value, At: time.Now()}
	if err := tx.Create(&e).Error; err != nil {
		return err
	}
	return queueWebhooks(tx, &e)
}

// HoldOrg and hold make a transaction wait its turn on one row, so a rule that is
// checked by reading and then kept by writing ("add it unless it is already here")
// holds when two callers arrive together. Taken before the first read; an organisation
// before anything in it.
func HoldOrg(tx *gorm.DB, org int64) error {
	return tx.Exec("update orgs set id = id where id = ?", org).Error
}

func Hold(tx *gorm.DB, model any, org, id int64) error {
	// UpdateColumn, so holding a company doesn't count as changing it.
	return tx.Model(model).Where("org_id = ? and id = ?", org, id).UpdateColumn("id", gorm.Expr("id")).Error
}

// Logged is for a query whose caller has nowhere to send an error: a count or a list
// on its way to a page. The page then shows nothing, and the log says why.
func Logged(res *gorm.DB) error {
	if res.Error != nil {
		log.Printf("database: %v", res.Error)
	}
	return res.Error
}

func Today() string { return time.Now().Format(time.DateOnly) }

// NonEmpty is Python's nullif(v, ”).
func NonEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// IDOf is an optional id as the API gives one: 0 for none.
func IDOf(id *int64) int64 {
	if id == nil {
		return 0
	}
	return *id
}

// FirstField is the first word of s, or "" when it has none.
func FirstField(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return ""
}

// CustomerActor is the actor recorded for what a customer does on their onboarding page.
const CustomerActor = "customer"

// AgentPrefix starts the actor recorded for what an agent does: "agent:" and its key's name.
const AgentPrefix = "agent:"

// AgentsLike matches an agent's actor in a SQL like.
const AgentsLike = AgentPrefix + "%"

// AgentActor is the actor recorded for what the agent with that key's name does.
func AgentActor(name string) string { return AgentPrefix + name }

// AgentName is the key's name in an agent's actor.
func AgentName(actor string) string { return strings.TrimPrefix(actor, AgentPrefix) }

// IsAgent is whether an actor is an agent's.
func IsAgent(actor string) bool { return strings.HasPrefix(actor, AgentPrefix) }

// NotSent is a failure of the mail server, not of what was asked.
type NotSent struct{ Err error }

func (e NotSent) Error() string { return e.Err.Error() }

func (e NotSent) Unwrap() error { return e.Err }
