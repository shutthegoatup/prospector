package privacy

import (
	"errors"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
	"github.com/shutthegoatup/prospector/internal/tickets"
)

// PersonExport is everything held about one contact row, for answering a request
// from that person to see it.
type PersonExport struct {
	ExportedAt    time.Time                  `json:"exported_at"`
	Note          string                     `json:"note,omitempty"`
	Company       string                     `json:"company,omitempty"`
	Person        store.Contact              `json:"person"`
	Facts         []store.Fact               `json:"facts"`
	Enquiries     []store.Enquiry            `json:"enquiries"`
	EnquiryThread []store.EnquiryMessage     `json:"enquiry_messages"`
	Suppressed    []store.Suppression        `json:"not_to_be_mailed"`
	CampaignMail  []store.CampaignRecipient  `json:"campaign_mail"`
	Suggested     []store.Suggestion         `json:"suggested_details"`
	Deals         []store.Deal               `json:"deals"`
	Access        []store.CustomerGrant      `json:"onboarding_access"`
	Agreements    []store.AgreementRecipient `json:"agreements_to_sign"`
	History       []ExportEvent              `json:"history"`
}

type ExportEvent struct {
	At     time.Time `json:"at"`
	By     string    `json:"by"`
	Action string    `json:"action"`
	Value  string    `json:"value,omitempty"`
}

func (s *PrivacyStore) ExportPerson(org, id int64, actor string) (*PersonExport, error) {
	out := &PersonExport{ExportedAt: time.Now().UTC(), Facts: []store.Fact{}, Enquiries: []store.Enquiry{}, Suggested: []store.Suggestion{}, Deals: []store.Deal{}, Access: []store.CustomerGrant{}, Agreements: []store.AgreementRecipient{}, History: []ExportEvent{}}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		c := &out.Person
		if err := tx.Where("org_id = ? and id = ?", org, id).First(c).Error; err != nil {
			return store.Missing(err, "no contact %d", id)
		}
		if strings.HasPrefix(c.Key, "org:") {
			return say.New("that row stands for a company, not a person")
		}
		if len(Alternates(store.Deref(c.Person))) > 1 {
			out.Note = "This row names several people; everything here is held against all of them."
		}
		out.Company = store.Deref(c.Maker)
		if c.CompanyID != nil {
			var co store.Company
			if err := tx.Where("org_id = ? and id = ?", org, *c.CompanyID).First(&co).Error; err == nil {
				out.Company = co.Name
			}
		}
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Order("id").Find(&out.Facts).Error; err != nil {
			return err
		}
		// As erase does: what they wrote to us counts whether or not it was matched to them.
		wrote := tx.Where("org_id = ? and contact_id = ?", org, id)
		if emails := store.EmailRe.FindAllString(strings.ToLower(store.Deref(c.Email)), -1); len(emails) > 0 {
			wrote = tx.Where("org_id = ? and (contact_id = ? or email in ?)", org, id, emails)
		}
		if err := wrote.Order("id").Find(&out.Enquiries).Error; err != nil {
			return err
		}
		out.EnquiryThread = []store.EnquiryMessage{}
		if ids := tickets.EnquiryIDs(out.Enquiries); len(ids) > 0 {
			if err := tx.Where("org_id = ? and enquiry_id in ?", org, ids).Where(tickets.InThread).Order("id").Find(&out.EnquiryThread).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Order("id").Find(&out.Suggested).Error; err != nil {
			return err
		}
		// Every address that is theirs, as erasure counts them: their own and any suggested
		// for them that nobody dismissed.
		var addresses strings.Builder
		addresses.WriteString(store.Deref(c.Email))
		for _, sg := range out.Suggested {
			if sg.Field == "email" && sg.Decision != prospecting.SuggestionDismissed {
				addresses.WriteString(" " + sg.Value)
			}
		}
		out.CampaignMail = []store.CampaignRecipient{}
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Order("id").Find(&out.CampaignMail).Error; err != nil {
			return err
		}
		out.Suppressed = []store.Suppression{}
		if emails := store.EmailRe.FindAllString(strings.ToLower(addresses.String()), -1); len(emails) > 0 {
			for _, e := range emails {
				emails = append(emails, strings.Trim(e, "."))
			}
			if err := tx.Where("org_id = ? and address in ?", org, emails).Order("id").Find(&out.Suppressed).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Order("id").Find(&out.Deals).Error; err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Order("id").Find(&out.Access).Error; err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Order("id").Find(&out.Agreements).Error; err != nil {
			return err
		}
		var events []store.Event
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Order("at, id").Find(&events).Error; err != nil {
			return err
		}
		for _, e := range events {
			out.History = append(out.History, ExportEvent{At: e.At, By: e.User, Action: e.Action, Value: store.Deref(e.Value)})
		}
		return s.Log(tx, org, id, c.CompanyID, actor, store.ActExport, nil)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// OrgExport is everything an organisation has put here, for taking it elsewhere. It
// holds no secret: not a password or key hash, nor the sign-on client secret, sealed
// or otherwise. What identifies erased people is left out too; only how many there are.
type OrgExport struct {
	ExportedAt    time.Time                  `json:"exported_at"`
	Organisation  ExportedOrg                `json:"organisation"`
	Members       []ExportMember             `json:"members"`
	Keys          []ExportKey                `json:"api_keys"`
	SignOn        *ExportSignOn              `json:"single_sign_on,omitempty"`
	Mail          *ExportMail                `json:"mail_server,omitempty"`
	Agents        []ExportAgent              `json:"agents"`
	Enrichers     []ExportEnricher           `json:"enrichment_vendors"`
	Companies     []store.Company            `json:"companies"`
	Facts         []store.Fact               `json:"facts"`
	Contacts      []store.Contact            `json:"contacts"`
	Deals         []store.Deal               `json:"deals"`
	Subs          []store.Subscription       `json:"subscriptions"`
	Onboardings   []store.Onboarding         `json:"onboardings"`
	OnboardSteps  []store.OnboardingStep     `json:"onboarding_steps"`
	Access        []store.CustomerGrant      `json:"onboarding_access"`
	Enquiries     []store.Enquiry            `json:"enquiries"`
	EnquiryThread []store.EnquiryMessage     `json:"enquiry_messages"`
	Suppressed    []store.Suppression        `json:"not_to_be_mailed"`
	Campaigns     []store.Campaign           `json:"campaigns"`
	CampaignMail  []store.CampaignRecipient  `json:"campaign_mail"`
	Agreements    []store.Agreement          `json:"agreements"`
	Recipients    []store.AgreementRecipient `json:"agreement_recipients"`
	Fields        []store.AgreementField     `json:"agreement_fields"`
	Events        []store.AgreementEvent     `json:"agreement_events"`
	Suggested     []store.Suggestion         `json:"suggested_details"`
	Tasks         []store.Task               `json:"tasks"`
	TaskRequests  []store.TaskRequest        `json:"task_requests"`
	TaskNotes     []store.TaskNote           `json:"task_notes"`
	Offerings     []store.Offering           `json:"offerings"`
	Verdicts      []store.Verdict            `json:"verdicts"`
	Roles         []store.Role               `json:"roles"`
	History       []ExportRecord             `json:"history"`
	Erased        int64                      `json:"people_erased"`
}

type ExportedOrg struct {
	Slug      string            `json:"slug"`
	Name      string            `json:"name"`
	Color     string            `json:"color"`
	Settings  store.OrgSettings `json:"settings"`
	CreatedAt time.Time         `json:"created_at"`
}

type ExportMember struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	Areas      string `json:"areas"`
	CallerName string `json:"caller_name,omitempty"`
	Signature  string `json:"signature,omitempty"`
}

type ExportKey struct {
	Name       string     `json:"name"`
	Scopes     string     `json:"scopes"`
	CreatedBy  string     `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

type ExportSignOn struct {
	Issuer   string         `json:"issuer"`
	ClientID string         `json:"client_id"`
	Required bool           `json:"required"`
	AutoJoin bool           `json:"auto_join"`
	Domains  []ExportDomain `json:"domains"`
}

// ExportAgent is one of the organisation's own agents, without its key.
// ExportEnricher is a vendor without its key, which is never exported.
type ExportEnricher struct {
	Kind         string `json:"kind"`
	Position     int    `json:"position"`
	MonthlyLimit int    `json:"monthly_limit"`
}

type ExportAgent struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Endpoint  string   `json:"endpoint"`
	Model     string   `json:"model"`
	ToolsURL  string   `json:"tools_url,omitempty"`
	SearchURL string   `json:"search_url,omitempty"`
	Tools     []string `json:"allowed_tools"`
	// FallsBackTo is the agent that takes over after FallbackMinutes of waiting.
	FallsBackTo     string `json:"falls_back_to,omitempty"`
	FallbackMinutes int    `json:"fallback_minutes,omitempty"`
	ThinkingOff     bool   `json:"thinking_off,omitempty"`
}

type ExportMail struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Sender   string `json:"sender"`
}

type ExportDomain struct {
	Domain     string     `json:"domain"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
}

type ExportRecord struct {
	ExportEvent
	ContactID int64  `json:"contact_id,omitempty"`
	CompanyID *int64 `json:"company_id,omitempty"`
	TaskID    int64  `json:"task_id,omitempty"`
}

func (s *PrivacyStore) ExportOrg(org int64, actor string) (*OrgExport, error) {
	out := &OrgExport{ExportedAt: time.Now().UTC(), Members: []ExportMember{}, Keys: []ExportKey{}, History: []ExportRecord{}}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := s.Log(tx, org, 0, nil, actor, store.ActExportOrg, nil); err != nil {
			return err
		}
		var o store.Org
		if err := tx.First(&o, org).Error; err != nil {
			return err
		}
		out.Organisation = ExportedOrg{Slug: o.Slug, Name: o.Name, Color: o.Color, Settings: o.Settings, CreatedAt: o.CreatedAt}
		if err := tx.Table("memberships").Select("users.name, users.email, memberships.role, memberships.areas, memberships.caller_name, memberships.signature").
			Joins("join users on users.id = memberships.user_id").
			Where("memberships.org_id = ?", org).Order("users.email").Scan(&out.Members).Error; err != nil {
			return err
		}
		if err := tx.Model(&store.APIKey{}).Where("org_id = ?", org).Order("id").Find(&out.Keys).Error; err != nil {
			return err
		}
		var conn store.SSOConnection
		if err := tx.Where("org_id = ?", org).First(&conn).Error; err == nil {
			out.SignOn = &ExportSignOn{Issuer: conn.Issuer, ClientID: conn.ClientID, Required: conn.Required, AutoJoin: conn.AutoJoin, Domains: []ExportDomain{}}
			if err := tx.Model(&store.SSODomain{}).Where("org_id = ?", org).Order("domain").Find(&out.SignOn.Domains).Error; err != nil {
				return err
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var mail store.OrgMail
		if err := tx.Where("org_id = ?", org).First(&mail).Error; err == nil {
			out.Mail = &ExportMail{Host: mail.Host, Port: mail.Port, Username: mail.Username, Sender: mail.Sender}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var agents []store.Agent
		if err := tx.Where("org_id = ?", org).Order("name, id").Find(&agents).Error; err != nil {
			return err
		}
		out.Agents = []ExportAgent{}
		names := map[int64]string{}
		for _, a := range agents {
			names[a.ID] = a.Name
		}
		for _, a := range agents {
			e := ExportAgent{Name: a.Name, Kind: a.Kind, Endpoint: a.BaseURL, Model: a.Model, ToolsURL: a.ToolsURL, SearchURL: a.SearchURL, Tools: a.AllowedTools(),
				ThinkingOff: a.ThinkingOff}
			if next, ok := names[a.FallbackID]; ok {
				e.FallsBackTo, e.FallbackMinutes = next, a.FallbackMinutes
			}
			out.Agents = append(out.Agents, e)
		}
		var enrichers []store.Enricher
		if err := tx.Where("org_id = ?", org).Order("position, id").Find(&enrichers).Error; err != nil {
			return err
		}
		out.Enrichers = []ExportEnricher{}
		for _, e := range enrichers {
			out.Enrichers = append(out.Enrichers, ExportEnricher{Kind: e.Kind, Position: e.Position, MonthlyLimit: e.MonthlyLimit})
		}
		mine := func() *gorm.DB { return tx.Where("org_id = ?", org).Order("id") }
		for _, rows := range []any{&out.Companies, &out.Facts, &out.Contacts, &out.Deals, &out.Subs, &out.Onboardings, &out.OnboardSteps, &out.Access, &out.Enquiries, &out.EnquiryThread, &out.Suppressed, &out.Campaigns, &out.Agreements, &out.Recipients, &out.Fields, &out.Events, &out.CampaignMail, &out.Suggested, &out.Tasks, &out.TaskRequests, &out.TaskNotes, &out.Offerings, &out.Verdicts, &out.Roles} {
			if err := mine().Find(rows).Error; err != nil {
				return err
			}
		}
		// A reply that never left isn't part of what was said.
		out.EnquiryThread = slices.DeleteFunc(out.EnquiryThread, func(m store.EnquiryMessage) bool { return m.Ours && m.SentAt == nil })
		var events []store.Event
		if err := mine().Find(&events).Error; err != nil {
			return err
		}
		for _, e := range events {
			if e.Action == store.ActErase.String() {
				out.Erased++
			}
			out.History = append(out.History, ExportRecord{ExportEvent{At: e.At, By: e.User, Action: e.Action, Value: store.Deref(e.Value)}, e.ContactID, e.CompanyID, e.TaskID})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PrivacyStore is the work across every area: exporting and erasing a person or an
// organisation's data, and the snapshots it is restored from.
// PrivacyStore holds the identity and crm areas for what it reads of them, such as the
// organisation an import is for and who may be added.
type PrivacyStore struct {
	*store.Core
	Identity *identity.IdentityStore
	CRM      *crm.CRMStore
}
