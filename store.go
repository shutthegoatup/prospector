package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type User struct {
	ID           int64  `gorm:"primaryKey"`
	Email        string `gorm:"uniqueIndex;not null"`
	Name         string
	PasswordHash string
	// MustChangePassword is set while the password is one somebody else has seen.
	MustChangePassword bool `gorm:"not null;default:false"`
	CreatedAt          time.Time
}

// OrgSettings is what differs between organisations: timings, wording and rules.
type OrgSettings struct {
	EmailAfterDays    int    `json:"email_after_days"`
	FollowupAfterDays int    `json:"followup_after_days"`
	Pitch             string `json:"pitch"`
	NoteTemplate      string `json:"note_template"`
	NeverContact      string `json:"never_contact"`
	IncorporatedOnly  bool   `json:"incorporated_only"`
	AgentGuidance     string `json:"agent_guidance"`
	EnquirySites      string `json:"enquiry_sites"`
}

func (s OrgSettings) withDefaults() OrgSettings {
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
	ID              int64     `gorm:"primaryKey" json:"id"`
	OrgID           int64     `gorm:"uniqueIndex:idx_org_company;not null" json:"-"`
	Name            string    `gorm:"uniqueIndex:idx_org_company;not null" json:"name"`
	Domain          string    `json:"domain"`
	Country         string    `json:"country"`
	Segment         string    `json:"segment"`
	Fit             string    `json:"fit"`
	Stage           string    `gorm:"index" json:"stage"`
	ConfiguratorURL string    `json:"configurator_url"`
	Platform        string    `json:"platform"`
	BuiltBy         string    `json:"built_by"`
	Why             string    `json:"why"`
	FoundBy         string    `json:"found_by"`
	Incorporated    *bool     `json:"incorporated"`
	Hold            *string   `json:"hold"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

const (
	CompanyReview    = "review"
	CompanyQualified = "qualified"
	CompanyRejected  = "rejected"
)

type Fact struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	OrgID     int64     `gorm:"index;not null" json:"-"`
	CompanyID int64     `gorm:"index" json:"company_id"`
	ContactID *int64    `json:"contact_id,omitempty"`
	Field     string    `json:"field"`
	Value     string    `json:"value"`
	SourceURL string    `json:"source_url"`
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

	OrgID       int64   `gorm:"uniqueIndex:idx_org_key;not null" json:"-"`
	CompanyID   *int64  `gorm:"index" json:"company_id"`
	Status      string  `gorm:"not null;default:''" json:"status"`
	Title       string  `json:"title"`
	EmailStatus string  `json:"email_status"`
	Stage       string  `json:"stage"`
	NextStep    string  `json:"next_step"`
	NextDate    *string `json:"next_date"`
	LostReason  string  `json:"lost_reason"`
	DraftBy     string  `json:"draft_by"`
}

// ContactSuggested is a person an agent found; a human picks them before outreach.
const ContactSuggested = "suggested"

var Columns = []string{"id", "key", "file", "section", "fit", "maker", "person", "first",
	"linkedin", "email", "note", "subject", "body", "followup", "flags",
	"linkedin_added", "email_sent", "followup_sent", "replied", "declined", "hold",
	"sources", "phone", "called", "call_notes", "tz", "send_to", "accepted", "message"}

// Event records who did what. User is a person's full name (their first name in
// older rows), "agent:<key name>" or "cli".
type Event struct {
	ID        int64  `gorm:"primaryKey"`
	OrgID     int64  `gorm:"index"`
	ContactID int64  `gorm:"index"`
	CompanyID *int64 `gorm:"index"`
	User      string
	Action    string
	Value     *string
	At        time.Time `gorm:"index"`
}

var Editable = map[string]bool{"phone": true, "email": true, "linkedin": true, "sources": true,
	"call_notes": true, "tz": true, "person": true, "send_to": true, "message": true, "note": true,
	"body": true, "followup": true, "subject": true,
	"stage": true, "next_step": true, "next_date": true, "lost_reason": true, "title": true}

// stringFields are the newer non-pointer columns, which store "" rather than NULL.
var stringFields = map[string]bool{"stage": true, "next_step": true, "lost_reason": true, "title": true}

var Marks = map[string]string{"linkedin": "linkedin_added", "email": "email_sent", "followup": "followup_sent",
	"call": "called", "replied": "replied", "declined": "declined", "hold": "hold", "accepted": "accepted"}

type Store struct {
	db *gorm.DB
	// keys seal what is stored but mustn't be readable from the database alone; nil
	// when the server was given none.
	keys *keyring
}

var models = []any{&User{}, &Org{}, &Membership{}, &Session{}, &APIKey{},
	&Company{}, &Fact{}, &Contact{}, &Event{}, &Erasure{}, &Identity{}, &SSOConnection{}, &SSODomain{}, &Enquiry{}}

// Open takes a postgres:// URL or a SQLite file path.
func Open(dsn string) (*Store, error) {
	var dialector gorm.Dialector
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		dialector = postgres.Open(dsn)
	} else {
		dialector = sqlite.Open(dsn + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(models...); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) postgres() bool { return s.db.Dialector.Name() == "postgres" }

// in scopes a query to one organisation. Every read and write of org data goes through it.
func (s *Store) in(org int64) *gorm.DB { return s.db.Where("org_id = ?", org) }

func today() string { return time.Now().Format("2006-01-02") }

func str(v string) *string { return &v }

// nonEmpty is Python's nullif(v, ”).
func nonEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Due returns one outreach step's queue for an organisation. NULLS FIRST keeps SQLite's ordering on Postgres.
// People on the never-contact list are left out here rather than marked, so the list
// and the queue can't drift apart: listing someone hides them, unlisting brings them back.
func (s *Store) Due(org int64, step string, now time.Time, set OrgSettings) ([]Contact, error) {
	set = set.withDefaults()
	var rows []Contact
	q := s.in(org).Model(&Contact{}).Where(companyNotRejected)
	switch step {
	case "linkedin":
		q = q.Where(openContacts + " and linkedin_added is null and note is not null and note <> ''").Order("fit nulls first, file")
	case "accepted":
		q = q.Where(openContacts + " and accepted is not null and email_sent is null").Order("accepted")
	case "call":
		q = q.Where(openContacts + " and phone is not null and phone != '' and called is null").
			Order("linkedin_added is null, linkedin_added nulls first, fit nulls first")
	case "email":
		cutoff := now.AddDate(0, 0, -set.EmailAfterDays).Format("2006-01-02")
		q = q.Where(openContacts+" and email_sent is null and accepted is null and linkedin_added <= ?", cutoff).Order("linkedin_added")
	case "followup":
		cutoff := now.AddDate(0, 0, -set.FollowupAfterDays).Format("2006-01-02")
		q = q.Where(openContacts+" and followup_sent is null and email_sent <= ?", cutoff).Order("email_sent")
	default:
		return nil, fmt.Errorf("unknown step %q", step)
	}
	if err := q.Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	if strings.TrimSpace(set.NeverContact) == "" {
		return rows, nil
	}
	due := rows[:0]
	for _, c := range rows {
		if set.listed(deref(c.Email), deref(c.Linkedin)) == nil {
			due = append(due, c)
		}
	}
	return due, nil
}

func (s *Store) log(tx *gorm.DB, org, contact int64, company *int64, actor, action string, value *string) error {
	return tx.Create(&Event{OrgID: org, ContactID: contact, CompanyID: company, User: actor, Action: action, Value: value, At: time.Now()}).Error
}

func (s *Store) Mark(org, id int64, step, date, actor string) error {
	return s.mark(org, id, step, date, actor, "")
}

func (s *Store) mark(org, id int64, step, date, actor, note string) error {
	column, ok := Marks[step]
	if !ok {
		return fmt.Errorf("unknown step %q", step)
	}
	if err := markDate(date, time.Now()); err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var c Contact
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
			return fmt.Errorf("no contact %d", id)
		}
		if err := c.canMark(); err != nil {
			return err
		}
		updates := map[string]any{column: date}
		if step == "replied" && c.Stage == "" {
			updates["stage"] = Stages[0].Key
		}
		if err := tx.Model(&c).Updates(updates).Error; err != nil {
			return err
		}
		value := date
		if note != "" {
			value += " · " + note
		}
		return s.log(tx, org, id, c.CompanyID, actor, "mark:"+step, &value)
	})
}

func (s *Store) Update(org, id int64, field, value, actor string) error {
	if !Editable[field] {
		return fmt.Errorf("%q can't be edited", field)
	}
	value = strings.TrimSpace(value)
	var v any = nonEmpty(value)
	if stringFields[field] {
		v = value
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var c Contact
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
			return fmt.Errorf("no contact %d", id)
		}
		if field == "stage" {
			if err := c.canEnterStage(value); err != nil {
				return err
			}
		}
		if field == "email" || field == "linkedin" {
			var o Org
			if err := tx.First(&o, org).Error; err != nil {
				return err
			}
			email, link := value, ""
			if field == "linkedin" {
				email, link = "", value
			}
			if err := s.allowed(&o, email, link); err != nil {
				return err
			}
			if err := erased(tx, org, "", "", email, link); err != nil {
				return err
			}
		}
		if err := tx.Model(&c).Update(field, v).Error; err != nil {
			return err
		}
		if field == "phone" {
			if err := tx.Model(&c).Update("tz", GuessTZ(value, deref(c.Maker))).Error; err != nil {
				return err
			}
		}
		return s.log(tx, org, id, c.CompanyID, actor, "set:"+field, nonEmpty(value))
	})
}

// Find matches the CLI's name argument against person, maker or first name.
func (s *Store) Find(org int64, name string) ([]Contact, error) {
	var rows []Contact
	like := "%" + strings.ToLower(name) + "%"
	err := s.in(org).Where("lower(person) like ? or lower(maker) like ? or first = ?", like, like, name).Find(&rows).Error
	return rows, err
}

func (s *Store) FindOne(org int64, name string) (*Contact, error) {
	rows, err := s.Find(org, name)
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		var who []string
		for _, r := range rows {
			who = append(who, deref(r.First)+" @ "+deref(r.Maker))
		}
		return nil, fmt.Errorf("%d matches for %q: %s", len(rows), name, strings.Join(who, "; "))
	}
	return &rows[0], nil
}
