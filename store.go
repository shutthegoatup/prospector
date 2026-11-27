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

// Store is every area's queries, each area a type of its own embedded here so its
// methods are the store's. They share one core, so a key or resolver set on the store
// reaches every area.
type Store struct {
	*core
	researchStore
	privacyStore
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

func newStore(c *core) *Store {
	s := &Store{core: c, identityStore: identityStore{Core: c}, agentStore: agentStore{Core: c}, ssoStore: ssoStore{Core: c},
		suppressionStore: suppressionStore{Core: c}, customerStore: customerStore{Core: c}, dealStore: dealStore{Core: c},
		subscriptionStore: subscriptionStore{Core: c}, onboardingStore: onboardingStore{Core: c}, privacyStore: privacyStore{Core: c}}
	// An area that needs another's queries holds that area, not the whole store.
	s.prospectingStore = prospectingStore{Core: c, Identity: &s.identityStore, Sales: &s.dealStore}
	s.enquiryStore = enquiryStore{Core: c, Prospecting: &s.prospectingStore}
	s.agreementStore = agreementStore{Core: c, Prospecting: &s.prospectingStore, Identity: &s.identityStore}
	s.outreachStore = outreachStore{Core: c, Identity: &s.identityStore, Suppression: &s.suppressionStore, Billing: &s.subscriptionStore}
	s.researchStore = researchStore{Core: c, Prospecting: &s.prospectingStore, Identity: &s.identityStore, Outreach: &s.outreachStore}
	return s
}

// through is the same store working through db, such as a transaction.
func (s *Store) through(db *gorm.DB) *Store {
	return newStore(&core{DB: db, Keyring: s.Keyring, DNS: s.DNS})
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
	return newStore(&core{DB: db}), nil
}

// oneNameWhateverItsCapitals backs ensureCompany's look-up with an index. Companies
// that already differ only in capitals keep it from being built, which costs the
// check its backing and nothing else, so the server still starts.
func (s *Store) oneNameWhateverItsCapitals() {
	err := s.DB.Exec("create unique index if not exists idx_companies_org_name_folded on companies (org_id, lower(name))").Error
	if err != nil {
		log.Printf("companies in one organisation share a name but for its capitals, so nothing but the look-up keeps another out: %v", err)
	}
}
