package main

import (
	"errors"
	"log"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	CompanyReview    = "review"
	CompanyQualified = "qualified"
	CompanyRejected  = "rejected"
)

var Columns = []string{"id", "key", "file", "section", "fit", "maker", "person", "first",
	"linkedin", "email", "note", "subject", "body", "followup", "flags",
	"linkedin_added", "email_sent", "followup_sent", "replied", "declined", "hold",
	"sources", "phone", "called", "call_notes", "tz", "send_to", "accepted", "message"}

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

// Store is every area's queries, each area a type of its own embedded here so its
// methods are the store's. They share one core, so a key or resolver set on the store
// reaches every area.
type Store struct {
	*core
	researchStore
	outreachStore
	agreementStore
	enquiryStore
	prospectingStore
	identityStore
	agentStore
	ssoStore
	suppressionStore
	customerStore
	dealStore
	subscriptionStore
	onboardingStore
}

// core is what every area's queries need: the database and the means to keep secrets.
type core struct {
	db *gorm.DB
	// keys seal what is stored but mustn't be readable from the database alone; nil
	// when the server was given none.
	keys *keyring
	// dns says whether a domain takes mail; nil asks nothing, so tests never reach the network.
	dns mailDNS
}

func newStore(c *core) *Store {
	s := &Store{core: c, identityStore: identityStore{c}, agentStore: agentStore{c}, ssoStore: ssoStore{c},
		suppressionStore: suppressionStore{c}, customerStore: customerStore{c}, dealStore: dealStore{c},
		subscriptionStore: subscriptionStore{c}, onboardingStore: onboardingStore{c}}
	// An area that needs another's queries holds that area, not the whole store.
	s.prospectingStore = prospectingStore{core: c, identity: &s.identityStore, deals: &s.dealStore}
	s.enquiryStore = enquiryStore{core: c, prospecting: &s.prospectingStore}
	s.agreementStore = agreementStore{core: c, prospecting: &s.prospectingStore, identity: &s.identityStore}
	s.outreachStore = outreachStore{core: c, identity: &s.identityStore, suppression: &s.suppressionStore, subscriptions: &s.subscriptionStore}
	s.researchStore = researchStore{core: c, prospecting: &s.prospectingStore, identity: &s.identityStore, outreach: &s.outreachStore}
	return s
}

// through is the same store working through db, such as a transaction.
func (s *Store) through(db *gorm.DB) *Store {
	return newStore(&core{db: db, keys: s.keys, dns: s.dns})
}

var models = []any{&User{}, &Org{}, &Membership{}, &Session{}, &APIKey{},
	&Company{}, &Fact{}, &Contact{}, &Event{}, &Erasure{}, &Identity{}, &SSOConnection{}, &SSODomain{}, &Enquiry{}, &EnquiryMessage{}, &Suppression{}, &UnsubscribeLink{}, &Campaign{}, &CampaignRecipient{}, &Agreement{}, &AgreementFile{}, &AgreementRecipient{}, &AgreementField{}, &AgreementEvent{}, &SealKey{}, &Suggestion{},
	&Task{}, &TaskRequest{}, &TaskNote{}, &RequestClaim{}, &PasswordToken{}, &OrgMail{}, &Agent{}, &AgentRun{}, &Deal{}, &Subscription{}, &Onboarding{}, &OnboardingStep{}, &CustomerGrant{}, &CustomerLink{}, &CustomerSession{}, &MailDomain{}, &Enricher{}, &EnrichUsage{}, &Offering{}, &Verdict{}, &Role{}, &BounceHook{}}

// Open takes a postgres:// URL and brings its schema up to date.
func Open(dsn string) (*Store, error) {
	s, err := connect(dsn)
	if err != nil {
		return nil, err
	}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// connect opens a database without touching its schema.
func connect(dsn string) (*Store, error) {
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		return nil, errors.New("set DATABASE_URL to a postgres:// URL")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	// Left alone, the pool keeps two idle connections and opens without limit, so a busy
	// moment opens Postgres connections over and over, and can open more than it allows.
	pool, err := db.DB()
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(20)
	pool.SetMaxIdleConns(20)
	pool.SetConnMaxIdleTime(5 * time.Minute)
	return newStore(&core{db: db}), nil
}

// in scopes a query to one organisation. Not every query uses it, so what holds the
// store's exported methods to their organisation is TestNoMethodReachesIntoAnotherOrganisation.
func (s *core) in(org int64) *gorm.DB { return s.db.Where("org_id = ?", org) }

// holdOrg and hold make a transaction wait its turn on one row, so a rule that is
// checked by reading and then kept by writing ("add it unless it is already here")
// holds when two callers arrive together. Taken before the first read; an organisation
// before anything in it.
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

func (s *core) log(tx *gorm.DB, org, contact int64, company *int64, actor, action string, value *string) error {
	return s.logAs(tx, org, contact, company, Actor{Name: actor}, action, value)
}

func (s *core) logAs(tx *gorm.DB, org, contact int64, company *int64, who Actor, action string, value *string) error {
	return tx.Create(&Event{OrgID: org, ContactID: contact, CompanyID: company, User: who.Name, KeyID: who.KeyID, TaskID: who.Task, Action: action, Value: value, At: time.Now()}).Error
}
