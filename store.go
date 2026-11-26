package main

import (
	"fmt"
	"log"
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
	if s.Currency == "" {
		s.Currency = defaultCurrency
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
	ID           int64     `gorm:"primaryKey" json:"id"`
	OrgID        int64     `gorm:"uniqueIndex:idx_org_company;not null" json:"-"`
	Name         string    `gorm:"uniqueIndex:idx_org_company;not null" json:"name"`
	Domain       string    `json:"domain"`
	Country      string    `json:"country"`
	Segment      string    `json:"segment"`
	Fit          string    `json:"fit"`
	Stage        string    `gorm:"index" json:"stage"`
	Why          string    `json:"why"`
	FoundBy      string    `json:"found_by"`
	Incorporated *bool     `json:"incorporated"`
	Hold         *string   `json:"hold"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

const (
	CompanyReview    = "review"
	CompanyQualified = "qualified"
	CompanyRejected  = "rejected"
)

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
	// KeyID is the API key behind an agent's action, since key names can repeat; 0 for people.
	KeyID int64 `gorm:"not null;default:0"`
	// TaskID is the task an action was done for, 0 when for none.
	TaskID int64 `gorm:"index;not null;default:0"`
	Action string
	Value  *string
	At     time.Time `gorm:"index"`
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

var Editable = map[string]bool{"phone": true, "email": true, "linkedin": true, "sources": true,
	"call_notes": true, "tz": true, "person": true, "send_to": true, "message": true, "note": true,
	"body": true, "followup": true, "subject": true,
	"stage": true, "next_step": true, "next_date": true, "lost_reason": true, "title": true}

// loggedValue are the fields whose new value goes into the history: where a conversation
// stands and when it is due. The rest are a person's details or free text about them,
// which their own row already holds, so the history keeps only that they changed.
var loggedValue = map[string]bool{"stage": true, "next_date": true}

// stringFields are the newer non-pointer columns, which store "" rather than NULL.
var stringFields = map[string]bool{"stage": true, "next_step": true, "lost_reason": true, "title": true}

var Marks = map[string]string{"linkedin": "linkedin_added", "email": "email_sent", "followup": "followup_sent",
	"call": "called", "replied": "replied", "declined": "declined", "hold": "hold", "accepted": "accepted"}

type Store struct {
	db *gorm.DB
	// keys seal what is stored but mustn't be readable from the database alone; nil
	// when the server was given none.
	keys *keyring
	// fts is whether SQLite has its search tables; Postgres searches through an index of its own.
	fts bool
	// dns says whether a domain takes mail; nil asks nothing, so tests never reach the network.
	dns mailDNS
}

var models = []any{&User{}, &Org{}, &Membership{}, &Session{}, &APIKey{},
	&Company{}, &Fact{}, &Contact{}, &Event{}, &Erasure{}, &Identity{}, &SSOConnection{}, &SSODomain{}, &Enquiry{}, &EnquiryMessage{}, &Suppression{}, &UnsubscribeLink{}, &Campaign{}, &CampaignRecipient{}, &Agreement{}, &AgreementFile{}, &AgreementRecipient{}, &AgreementField{}, &Suggestion{},
	&Task{}, &TaskRequest{}, &TaskNote{}, &RequestClaim{}, &PasswordToken{}, &OrgMail{}, &Agent{}, &AgentRun{}, &Deal{}, &Subscription{}, &Onboarding{}, &OnboardingStep{}, &CustomerGrant{}, &CustomerLink{}, &CustomerSession{}, &MailDomain{}, &Offering{}, &Verdict{}}

// Open takes a postgres:// URL or a SQLite file path.
func Open(dsn string) (*Store, error) {
	var dialector gorm.Dialector
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		dialector = postgres.Open(dsn)
	} else {
		// A transaction takes the write lock as it begins and waits for it. One that began
		// by reading would be refused outright, however long it was willing to wait, if
		// someone else had written by the time it wrote.
		dialector = sqlite.Open(dsn + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(models...); err != nil {
		return nil, err
	}
	if err := moveAssetColumns(db); err != nil {
		return nil, err
	}
	if err := allowSeveralDeals(db); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	s.oneNameWhateverItsCapitals()
	s.indexSearch()
	if err := migrateAgentConfigs(db); err != nil {
		return nil, err
	}
	if err := oneGoingRunPerAgent(db); err != nil {
		return nil, err
	}
	return s, nil
}

// oneGoingRunPerAgent has the database hold that an agent works a task once at a time,
// however two starts interleave; several agents can share a task. It keeps the name of
// the earlier rule, one run per task, because an earlier release creates that name only
// if nothing has it: so going back to one still starts, whatever runs are going.
func oneGoingRunPerAgent(db *gorm.DB) error {
	const name = "idx_agent_runs_one_going"
	var def string
	lookup := "select coalesce(sql, '') from sqlite_master where type = 'index' and name = ?"
	if db.Dialector.Name() == "postgres" {
		lookup = "select indexdef from pg_indexes where indexname = ? and tablename = 'agent_runs' and schemaname = current_schema()"
	}
	if err := db.Raw(lookup, name).Scan(&def).Error; err != nil {
		return err
	}
	if strings.Contains(def, "agent_id") {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, stmt := range []string{"drop index if exists " + name,
			"create unique index " + name + " on agent_runs (task_id, agent_id) where status in ('running', 'waiting')"} {
			if err := tx.Exec(stmt).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) postgres() bool { return s.db.Dialector.Name() == "postgres" }

// in scopes a query to one organisation. Not every query uses it, so what holds the
// store's exported methods to their organisation is TestNoMethodReachesIntoAnotherOrganisation.
func (s *Store) in(org int64) *gorm.DB { return s.db.Where("org_id = ?", org) }

// holdOrg and hold make a transaction wait its turn on one row, so a rule that is
// checked by reading and then kept by writing ("add it unless it is already here")
// holds when two callers arrive together. Taken before the first read; an organisation
// before anything in it. On SQLite a write that opens the transaction also keeps it
// from being refused for writing after someone else has.
func holdOrg(tx *gorm.DB, org int64) error {
	return tx.Exec("update orgs set id = id where id = ?", org).Error
}

func hold(tx *gorm.DB, model any, org, id int64) error {
	// UpdateColumn, so holding a company doesn't count as changing it.
	return tx.Model(model).Where("org_id = ? and id = ?", org, id).UpdateColumn("id", gorm.Expr("id")).Error
}

// oneNameWhateverItsCapitals backs ensureCompany's look-up with an index. Companies
// that already differ only in capitals keep it from being built, which costs the
// check its backing and nothing else, so the server still starts.
func (s *Store) oneNameWhateverItsCapitals() {
	err := s.db.Exec("create unique index if not exists idx_companies_org_name_folded on companies (org_id, lower(name))").Error
	if err != nil {
		log.Printf("companies in one organisation share a name but for its capitals, so nothing but the look-up keeps another out: %v", err)
	}
}

// logged is for a query whose caller has nowhere to send an error: a count or a list
// on its way to a page. The page then shows nothing, and the log says why.
func logged(res *gorm.DB) error {
	if res.Error != nil {
		log.Printf("database: %v", res.Error)
	}
	return res.Error
}

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
	return s.logAs(tx, org, contact, company, Actor{Name: actor}, action, value)
}

func (s *Store) logAs(tx *gorm.DB, org, contact int64, company *int64, who Actor, action string, value *string) error {
	return tx.Create(&Event{OrgID: org, ContactID: contact, CompanyID: company, User: who.Name, KeyID: who.KeyID, TaskID: who.Task, Action: action, Value: value, At: time.Now()}).Error
}

func (s *Store) Mark(org, id int64, step, date, actor string) error {
	return s.mark(org, id, step, date, Actor{Name: actor}, "")
}

func (s *Store) mark(org, id int64, step, date string, who Actor, note string) error {
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
			return missing(err, "no contact %d", id)
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
		return s.logAs(tx, org, id, c.CompanyID, who, "mark:"+step, &value)
	})
}

func (s *Store) Update(org, id int64, field, value, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if of, ok := strings.CutPrefix(field, "deal_"); ok {
			return s.setDeal(tx, org, id, of, value, actor)
		}
		return s.set(tx, org, id, field, value, actor)
	})
}

func (s *Store) set(tx *gorm.DB, org, id int64, field, value, actor string) error {
	if !Editable[field] {
		return fmt.Errorf("%q can't be edited", field)
	}
	value = strings.TrimSpace(value)
	var v any = nonEmpty(value)
	if stringFields[field] {
		v = value
	}
	var c Contact
	if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
		return missing(err, "no contact %d", id)
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
	var logged *string
	if loggedValue[field] {
		logged = nonEmpty(value)
	}
	return s.log(tx, org, id, c.CompanyID, actor, "set:"+field, logged)
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
