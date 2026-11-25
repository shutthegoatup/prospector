package main

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// PersonExport is everything held about one contact row, for answering a request
// from that person to see it.
type PersonExport struct {
	ExportedAt    time.Time        `json:"exported_at"`
	Note          string           `json:"note,omitempty"`
	Company       string           `json:"company,omitempty"`
	Person        Contact          `json:"person"`
	Facts         []Fact           `json:"facts"`
	Enquiries     []Enquiry        `json:"enquiries"`
	EnquiryThread []EnquiryMessage `json:"enquiry_messages"`
	Suggested     []Suggestion     `json:"suggested_details"`
	Deals         []Deal           `json:"deals"`
	Access        []CustomerGrant  `json:"onboarding_access"`
	History       []ExportEvent    `json:"history"`
}

type ExportEvent struct {
	At     time.Time `json:"at"`
	By     string    `json:"by"`
	Action string    `json:"action"`
	Value  string    `json:"value,omitempty"`
}

func (s *Store) ExportPerson(org, id int64, actor string) (*PersonExport, error) {
	out := &PersonExport{ExportedAt: time.Now().UTC(), Facts: []Fact{}, Enquiries: []Enquiry{}, Suggested: []Suggestion{}, Deals: []Deal{}, Access: []CustomerGrant{}, History: []ExportEvent{}}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		c := &out.Person
		if err := tx.Where("org_id = ? and id = ?", org, id).First(c).Error; err != nil {
			return missing(err, "no contact %d", id)
		}
		if strings.HasPrefix(c.Key, "org:") {
			return errors.New("that row stands for a company, not a person")
		}
		if len(alternates(deref(c.Person))) > 1 {
			out.Note = "This row names several people; everything here is held against all of them."
		}
		out.Company = deref(c.Maker)
		if c.CompanyID != nil {
			var co Company
			if err := tx.Where("org_id = ? and id = ?", org, *c.CompanyID).First(&co).Error; err == nil {
				out.Company = co.Name
			}
		}
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Order("id").Find(&out.Facts).Error; err != nil {
			return err
		}
		// As erase does: what they wrote to us counts whether or not it was matched to them.
		wrote := tx.Where("org_id = ? and contact_id = ?", org, id)
		if emails := emailRe.FindAllString(strings.ToLower(deref(c.Email)), -1); len(emails) > 0 {
			wrote = tx.Where("org_id = ? and (contact_id = ? or email in ?)", org, id, emails)
		}
		if err := wrote.Order("id").Find(&out.Enquiries).Error; err != nil {
			return err
		}
		out.EnquiryThread = []EnquiryMessage{}
		if ids := enquiryIDs(out.Enquiries); len(ids) > 0 {
			if err := tx.Where("org_id = ? and enquiry_id in ?", org, ids).Where(inThread).Order("id").Find(&out.EnquiryThread).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Order("id").Find(&out.Suggested).Error; err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Order("id").Find(&out.Deals).Error; err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Order("id").Find(&out.Access).Error; err != nil {
			return err
		}
		var events []Event
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Order("at, id").Find(&events).Error; err != nil {
			return err
		}
		for _, e := range events {
			out.History = append(out.History, ExportEvent{At: e.At, By: e.User, Action: e.Action, Value: deref(e.Value)})
		}
		return s.log(tx, org, id, c.CompanyID, actor, "export", nil)
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
	ExportedAt    time.Time        `json:"exported_at"`
	Organisation  ExportedOrg      `json:"organisation"`
	Members       []ExportMember   `json:"members"`
	Keys          []ExportKey      `json:"api_keys"`
	SignOn        *ExportSignOn    `json:"single_sign_on,omitempty"`
	Mail          *ExportMail      `json:"mail_server,omitempty"`
	Agents        []ExportAgent    `json:"agents"`
	Companies     []Company        `json:"companies"`
	Facts         []Fact           `json:"facts"`
	Contacts      []Contact        `json:"contacts"`
	Deals         []Deal           `json:"deals"`
	Subs          []Subscription   `json:"subscriptions"`
	Onboardings   []Onboarding     `json:"onboardings"`
	OnboardSteps  []OnboardingStep `json:"onboarding_steps"`
	Access        []CustomerGrant  `json:"onboarding_access"`
	Enquiries     []Enquiry        `json:"enquiries"`
	EnquiryThread []EnquiryMessage `json:"enquiry_messages"`
	Suggested     []Suggestion     `json:"suggested_details"`
	Tasks         []Task           `json:"tasks"`
	TaskRequests  []TaskRequest    `json:"task_requests"`
	TaskNotes     []TaskNote       `json:"task_notes"`
	History       []ExportRecord   `json:"history"`
	Erased        int64            `json:"people_erased"`
}

type ExportedOrg struct {
	Slug      string      `json:"slug"`
	Name      string      `json:"name"`
	Color     string      `json:"color"`
	Settings  OrgSettings `json:"settings"`
	CreatedAt time.Time   `json:"created_at"`
}

type ExportMember struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	Role       string `json:"role"`
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
type ExportAgent struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Endpoint string   `json:"endpoint"`
	Model    string   `json:"model"`
	ToolsURL string   `json:"tools_url,omitempty"`
	Tools    []string `json:"allowed_tools"`
	// FallsBackTo is the agent that takes over after FallbackMinutes of waiting.
	FallsBackTo     string `json:"falls_back_to,omitempty"`
	FallbackMinutes int    `json:"fallback_minutes,omitempty"`
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

func (s *Store) ExportOrg(org int64, actor string) (*OrgExport, error) {
	out := &OrgExport{ExportedAt: time.Now().UTC(), Members: []ExportMember{}, Keys: []ExportKey{}, History: []ExportRecord{}}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// Written before anything is read: SQLite refuses a write that follows reads in
		// a transaction if anyone else wrote meanwhile, and this one reads for a while.
		if err := s.log(tx, org, 0, nil, actor, "export:org", nil); err != nil {
			return err
		}
		var o Org
		if err := tx.First(&o, org).Error; err != nil {
			return err
		}
		out.Organisation = ExportedOrg{Slug: o.Slug, Name: o.Name, Color: o.Color, Settings: o.Settings, CreatedAt: o.CreatedAt}
		if err := tx.Table("memberships").Select("users.name, users.email, memberships.role, memberships.caller_name, memberships.signature").
			Joins("join users on users.id = memberships.user_id").
			Where("memberships.org_id = ?", org).Order("users.email").Scan(&out.Members).Error; err != nil {
			return err
		}
		if err := tx.Model(&APIKey{}).Where("org_id = ?", org).Order("id").Find(&out.Keys).Error; err != nil {
			return err
		}
		var conn SSOConnection
		if err := tx.Where("org_id = ?", org).First(&conn).Error; err == nil {
			out.SignOn = &ExportSignOn{Issuer: conn.Issuer, ClientID: conn.ClientID, Required: conn.Required, AutoJoin: conn.AutoJoin, Domains: []ExportDomain{}}
			if err := tx.Model(&SSODomain{}).Where("org_id = ?", org).Order("domain").Find(&out.SignOn.Domains).Error; err != nil {
				return err
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var mail OrgMail
		if err := tx.Where("org_id = ?", org).First(&mail).Error; err == nil {
			out.Mail = &ExportMail{Host: mail.Host, Port: mail.Port, Username: mail.Username, Sender: mail.Sender}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var agents []Agent
		if err := tx.Where("org_id = ?", org).Order("name, id").Find(&agents).Error; err != nil {
			return err
		}
		out.Agents = []ExportAgent{}
		names := map[int64]string{}
		for _, a := range agents {
			names[a.ID] = a.Name
		}
		for _, a := range agents {
			e := ExportAgent{Name: a.Name, Kind: a.Kind, Endpoint: a.BaseURL, Model: a.Model, ToolsURL: a.ToolsURL, Tools: a.AllowedTools()}
			if next, ok := names[a.FallbackID]; ok {
				e.FallsBackTo, e.FallbackMinutes = next, a.FallbackMinutes
			}
			out.Agents = append(out.Agents, e)
		}
		mine := func() *gorm.DB { return tx.Where("org_id = ?", org).Order("id") }
		for _, rows := range []any{&out.Companies, &out.Facts, &out.Contacts, &out.Deals, &out.Subs, &out.Onboardings, &out.OnboardSteps, &out.Access, &out.Enquiries, &out.EnquiryThread, &out.Suggested, &out.Tasks, &out.TaskRequests, &out.TaskNotes} {
			if err := mine().Find(rows).Error; err != nil {
				return err
			}
		}
		var events []Event
		if err := mine().Find(&events).Error; err != nil {
			return err
		}
		for _, e := range events {
			if e.Action == "erase" {
				out.Erased++
			}
			out.History = append(out.History, ExportRecord{ExportEvent{At: e.At, By: e.User, Action: e.Action, Value: deref(e.Value)}, e.ContactID, e.CompanyID, e.TaskID})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
