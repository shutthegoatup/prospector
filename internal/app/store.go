package app

import (
	"errors"
	"log"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/shutthegoatup/prospector/internal/agents"
	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/marketing"
	"github.com/shutthegoatup/prospector/internal/onboarding"
	"github.com/shutthegoatup/prospector/internal/privacy"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/sales"
	"github.com/shutthegoatup/prospector/internal/signing"
	"github.com/shutthegoatup/prospector/internal/store"
	"github.com/shutthegoatup/prospector/internal/tickets"
	"github.com/shutthegoatup/prospector/internal/webhooks"
)

// Store is every area's queries, each area a type of its own embedded here so its
// methods are the store's. They share one core, so a key or resolver set on the store
// reaches every area.
type Store struct {
	*store.Core
	research.ResearchStore
	privacy.PrivacyStore
	webhooks.WebhookStore
	marketing.OutreachStore
	signing.AgreementStore
	tickets.EnquiryStore
	prospecting.ProspectingStore
	crm.CRMStore
	identity.IdentityStore
	agents.AgentStore
	identity.SSOStore
	marketing.SuppressionStore
	onboarding.CustomerStore
	sales.DealStore
	sales.SubscriptionStore
	onboarding.OnboardingStore
}

func newStore(c *store.Core) *Store {
	s := &Store{Core: c, IdentityStore: identity.IdentityStore{Core: c}, AgentStore: agents.AgentStore{Core: c}, SSOStore: identity.SSOStore{Core: c},
		SuppressionStore: marketing.SuppressionStore{Core: c}, CustomerStore: onboarding.CustomerStore{Core: c}, DealStore: sales.DealStore{Core: c},
		SubscriptionStore: sales.SubscriptionStore{Core: c}, OnboardingStore: onboarding.OnboardingStore{Core: c}}
	// An area that needs another's queries holds that area, not the whole store.
	s.CRMStore = crm.CRMStore{Core: c, Sales: &s.DealStore}
	s.ProspectingStore = prospecting.ProspectingStore{Core: c, Identity: &s.IdentityStore, CRM: &s.CRMStore}
	s.EnquiryStore = tickets.EnquiryStore{Core: c, CRM: &s.CRMStore}
	s.AgreementStore = signing.AgreementStore{Core: c, CRM: &s.CRMStore, Identity: &s.IdentityStore}
	s.OutreachStore = marketing.OutreachStore{Core: c, Identity: &s.IdentityStore, Suppression: &s.SuppressionStore, Billing: &s.SubscriptionStore}
	s.WebhookStore = webhooks.WebhookStore{Core: c}
	s.PrivacyStore = privacy.PrivacyStore{Core: c, Identity: &s.IdentityStore, CRM: &s.CRMStore}
	s.ResearchStore = research.ResearchStore{Core: c, Prospecting: &s.ProspectingStore, CRM: &s.CRMStore, Identity: &s.IdentityStore, Outreach: &s.OutreachStore}
	return s
}

var models = []any{&store.User{}, &store.Org{}, &store.Membership{}, &store.Session{}, &store.APIKey{},
	&store.Company{}, &store.Fact{}, &store.Contact{}, &store.Event{}, &store.Erasure{}, &store.Identity{}, &store.SSOConnection{}, &store.SSODomain{}, &store.Enquiry{}, &store.EnquiryMessage{}, &store.Suppression{}, &store.UnsubscribeLink{}, &store.Campaign{}, &store.CampaignRecipient{}, &store.Agreement{}, &store.AgreementFile{}, &store.AgreementRecipient{}, &store.AgreementField{}, &store.AgreementEvent{}, &store.SealKey{}, &store.Suggestion{},
	&store.Task{}, &store.TaskRequest{}, &store.TaskNote{}, &store.RequestClaim{}, &store.PasswordToken{}, &store.OrgMail{}, &store.Agent{}, &store.AgentRun{}, &store.Deal{}, &store.Subscription{}, &store.Onboarding{}, &store.OnboardingStep{}, &store.CustomerGrant{}, &store.CustomerLink{}, &store.CustomerSession{}, &store.MailDomain{}, &store.Enricher{}, &store.EnrichUsage{}, &store.Offering{}, &store.Verdict{}, &store.Role{}, &store.BounceHook{}, &store.WebhookEndpoint{}, &store.WebhookDelivery{}}

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
	return newStore(&store.Core{DB: db}), nil
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
