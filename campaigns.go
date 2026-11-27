package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

// Campaign is a marketing mail written once for many: everyone in conversation, or
// every customer. It goes only to people the organisation already has something with:
// cold outreach stays one person writing to one person.
type Campaign struct {
	ID       int64  `gorm:"primaryKey" json:"id"`
	OrgID    int64  `gorm:"index;not null" json:"-"`
	Name     string `gorm:"not null;default:''" json:"name"`
	Audience string `gorm:"not null;default:'conversations'" json:"audience"`
	// Kind is "marketing", or "service": a notice to current customers about what they have
	// (a price or terms change), which reaches them even if they unsubscribed from marketing.
	Kind    string `gorm:"not null;default:'marketing'" json:"kind"`
	Subject string `gorm:"not null;default:''" json:"subject"`
	Body    string `gorm:"not null;default:''" json:"body"`
	// Status is "draft" until it is sent; only a draft can be changed.
	Status    string    `gorm:"not null;default:'draft'" json:"status"`
	CreatedBy string    `gorm:"not null;default:''" json:"created_by"`
	UpdatedBy string    `gorm:"not null;default:''" json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// QueuedBy and QueuedAt say who sent it and when; SentAt is when the last mail went.
	QueuedBy string     `gorm:"not null;default:''" json:"queued_by"`
	QueuedAt *time.Time `json:"queued_at"`
	SentAt   *time.Time `json:"sent_at"`
	// Paused says why sending stopped by itself; Failures counts sends in a row that
	// didn't reach the mail server.
	Paused   string `gorm:"not null;default:''" json:"paused"`
	Failures int    `gorm:"not null;default:0" json:"-"`
}

const (
	AudienceConversations = "conversations"
	AudienceCustomers     = "customers"
	KindMarketing         = "marketing"
	KindService           = "service"
	CampaignDraft         = "draft"
	CampaignSending       = "sending"
	CampaignPaused        = "paused"
	CampaignSent          = "sent"
	CampaignCancelled     = "cancelled"

	campaignSubjectLimit = 200
	campaignBodyLimit    = 20000
)

var CampaignAudiences = []struct{ Key, Label string }{
	{AudienceConversations, "Everyone in conversation"},
	{AudienceCustomers, "Everyone at a customer"},
}

func audienceLabel(key string) string {
	for _, a := range CampaignAudiences {
		if a.Key == key {
			return a.Label
		}
	}
	return ""
}

type CampaignIn struct {
	Name     string `json:"name"`
	Audience string `json:"audience"`
	Kind     string `json:"kind"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
}

// campaignField checks one field as a draft holds it.
func campaignField(field, value string) (string, error) {
	switch field {
	case "name":
		value = strings.TrimSpace(value)
		if value == "" {
			return "", errors.New("a campaign needs a name")
		}
		if utf8.RuneCountInString(value) > 120 {
			return "", errors.New("a campaign's name is 120 characters at most")
		}
	case "audience":
		if audienceLabel(value) == "" {
			return "", fmt.Errorf("a campaign goes to %q or %q", AudienceConversations, AudienceCustomers)
		}
	case "kind":
		if value != KindMarketing && value != KindService {
			return "", fmt.Errorf("a campaign is %q or %q", KindMarketing, KindService)
		}
	case "subject":
		// One line: a subject is a header.
		value = strings.Join(strings.Fields(value), " ")
		if utf8.RuneCountInString(value) > campaignSubjectLimit {
			return "", fmt.Errorf("a subject is %d characters at most", campaignSubjectLimit)
		}
	case "body":
		// Only trailing blank lines go: a line that ends in a space, as "-- " does, keeps it.
		value = strings.TrimRight(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
		if utf8.RuneCountInString(value) > campaignBodyLimit {
			return "", fmt.Errorf("a campaign is %d characters at most", campaignBodyLimit)
		}
	default:
		return "", fmt.Errorf("a campaign has no %q to set", field)
	}
	return value, nil
}

func (s *Store) AddCampaign(org int64, in CampaignIn, actor string) (*Campaign, error) {
	if in.Kind == "" {
		in.Kind = KindMarketing
	}
	if in.Audience == "" {
		in.Audience = AudienceConversations
		if in.Kind == KindService {
			in.Audience = AudienceCustomers
		}
	}
	c := Campaign{OrgID: org, Status: CampaignDraft, CreatedBy: actor, UpdatedBy: actor}
	for _, f := range []struct {
		field string
		value string
		into  *string
	}{{"name", in.Name, &c.Name}, {"kind", in.Kind, &c.Kind}, {"audience", in.Audience, &c.Audience}, {"subject", in.Subject, &c.Subject}, {"body", in.Body, &c.Body}} {
		v, err := campaignField(f.field, f.value)
		if err != nil {
			return nil, err
		}
		*f.into = v
	}
	if err := c.fits(); err != nil {
		return nil, err
	}
	return &c, s.db.Create(&c).Error
}

// ChangeCampaign makes several changes to a draft, or none.
func (s *Store) ChangeCampaign(org, id int64, changes []Change, actor string) (*Campaign, error) {
	var c Campaign
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
			return missing(err, "no campaign %d", id)
		}
		if c.Status != CampaignDraft {
			return errors.New("only a draft can be changed")
		}
		updates := map[string]any{"updated_by": actor}
		after := c
		for _, ch := range changes {
			v, err := campaignField(ch.Field, ch.Value)
			if err != nil {
				return err
			}
			updates[ch.Field] = v
			switch ch.Field {
			case "kind":
				after.Kind = v
			case "audience":
				after.Audience = v
			}
		}
		// Turning a draft into a service notice takes it to customers, unless asked otherwise.
		if after.Kind == KindService && c.Kind != KindService && updates["audience"] == nil {
			after.Audience, updates["audience"] = AudienceCustomers, AudienceCustomers
		}
		if err := after.fits(); err != nil {
			return err
		}
		// Held to the draft it was read as: one sent in the meantime is left as it went.
		res := tx.Model(&Campaign{}).Where("org_id = ? and id = ? and status = ?", org, id, CampaignDraft).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errors.New("only a draft can be changed")
		}
		return tx.Where("org_id = ? and id = ?", org, id).First(&c).Error
	})
	return &c, err
}

func (s *Store) RemoveCampaign(org, id int64) error {
	res := s.in(org).Where("id = ? and status = ?", id, CampaignDraft).Delete(&Campaign{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var n int64
		if err := s.in(org).Model(&Campaign{}).Where("id = ?", id).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return noSuch("no campaign %d", id)
		}
		return errors.New("only a draft can be removed; one that went is kept as a record of what was sent")
	}
	return nil
}

// Campaigns are an organisation's, newest first.
func (s *Store) Campaigns(org int64) ([]Campaign, error) {
	out := []Campaign{}
	return out, s.in(org).Order("id desc").Find(&out).Error
}

func (s *Store) Campaign(org, id int64) (*Campaign, error) {
	var c Campaign
	if err := s.in(org).Where("id = ?", id).First(&c).Error; err != nil {
		return nil, missing(err, "no campaign %d", id)
	}
	return &c, nil
}

// Recipient is one person a campaign is for, and why they are left out when they are.
type Recipient struct {
	ContactID int64  `json:"contact_id"`
	Name      string `json:"name"`
	Company   string `json:"company"`
	Address   string `json:"address"`
	// LeftOut is empty for someone it goes to, and otherwise says why it doesn't.
	LeftOut string `json:"left_out"`
}

// Audience is everyone a campaign is for, those it goes to first, each address once.
func (s *Store) Audience(o *Org, c Campaign) ([]Recipient, error) {
	w, err := s.audienceWork(o)
	if err != nil {
		return nil, err
	}
	return s.audience(o, c, w)
}

// audienceWork is what every campaign's audience is checked against, read once for a
// page of them.
type audienceWork struct {
	gate      *mailGate
	customers []int64
	noMail    map[string]bool
}

func (s *Store) audienceWork(o *Org) (*audienceWork, error) {
	g, err := s.gate(s.db, o)
	if err != nil {
		return nil, err
	}
	customers, err := s.customerCompanies(o.ID)
	if err != nil {
		return nil, err
	}
	return &audienceWork{gate: g, customers: customers, noMail: s.noMailDomains()}, nil
}

func (s *Store) audience(o *Org, c Campaign, w *audienceWork) ([]Recipient, error) {
	if c.OrgID != o.ID {
		return nil, noSuch("no campaign %d", c.ID)
	}
	q := s.in(o.ID).Where("key not like 'org:%'").Where("status <> ?", ContactSuggested).Where("declined is null")
	switch c.Audience {
	case AudienceConversations:
		q = q.Where(inConversation)
	case AudienceCustomers:
		companies := w.customers
		if len(companies) == 0 {
			return []Recipient{}, nil
		}
		q = q.Where("company_id in ?", companies)
	default:
		return nil, fmt.Errorf("a campaign goes to %q or %q", AudienceConversations, AudienceCustomers)
	}
	var people []Contact
	if err := q.Order("id").Find(&people).Error; err != nil {
		return nil, err
	}
	names, err := s.companyNames(o.ID, people)
	if err != nil {
		return nil, err
	}
	out := make([]Recipient, 0, len(people))
	seen := map[string]string{}
	for _, p := range people {
		r := Recipient{ContactID: p.ID, Name: displayName(p)}
		if p.CompanyID != nil {
			r.Company = names[*p.CompanyID]
		}
		addr, err := bareAddress(deref(p.Email))
		switch {
		case strings.TrimSpace(deref(p.Email)) == "":
			r.LeftOut = "no email address"
		case err != nil:
			r.Address, r.LeftOut = deref(p.Email), "not one email address"
		default:
			r.Address = addr
			if first, twice := seen[addr]; twice {
				r.LeftOut = "same address as " + first
			} else if why := w.gate.whyFor(c.Kind, addr, deref(p.Linkedin)); why != nil {
				because, known := leftOutBecause(why)
				if !known {
					return nil, why
				}
				r.LeftOut = because
			} else if w.noMail[mailDomainOf(addr)] {
				r.LeftOut = "its domain takes no mail"
			} else {
				seen[addr] = r.Name
			}
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LeftOut == "" && out[j].LeftOut != "" })
	return out, nil
}

// leftOutBecause says why someone is left out, for the reasons there are. Any other
// error is a failure to check, and is not passed off as a reason.
func leftOutBecause(err error) (string, bool) {
	switch {
	case errors.Is(err, errSuppressed):
		return "on the list not to be mailed", true
	case errors.Is(err, errBounced):
		return "mail to them bounced", true
	case errors.Is(err, errComplained):
		return "marked a mail as spam", true
	case errors.Is(err, errErased):
		return "erased on request", true
	case errors.Is(err, errNeverContact):
		return "on the never-contact list", true
	}
	return "", false
}

// customerCompanies are those with a subscription running today: started, not ended.
func (s *Store) customerCompanies(org int64) ([]int64, error) {
	subs, err := s.Subscriptions(org, 0, today())
	if err != nil {
		return nil, err
	}
	var out []int64
	seen := map[int64]bool{}
	for _, sub := range subs {
		if sub.Status != SubEnded && sub.Status != SubScheduled && !seen[sub.CompanyID] {
			seen[sub.CompanyID] = true
			out = append(out, sub.CompanyID)
		}
	}
	return out, nil
}

func (s *Store) companyNames(org int64, people []Contact) (map[int64]string, error) {
	var ids []int64
	for _, p := range people {
		if p.CompanyID != nil {
			ids = append(ids, *p.CompanyID)
		}
	}
	names := map[int64]string{}
	if len(ids) == 0 {
		return names, nil
	}
	var companies []Company
	if err := s.in(org).Where("id in ?", ids).Find(&companies).Error; err != nil {
		return nil, err
	}
	for _, c := range companies {
		names[c.ID] = c.Name
	}
	return names, nil
}

// fits is whether a campaign's kind and audience go together: a service notice is for
// current customers only, or it is marketing by another name.
func (c Campaign) fits() error {
	if c.Kind == KindService && c.Audience != AudienceCustomers {
		return errors.New("a service notice goes to current customers only: one to people in conversation is marketing")
	}
	return nil
}
