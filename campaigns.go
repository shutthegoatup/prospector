package main

import (
	"errors"
	"fmt"
	"net/http"
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
	Subject  string `gorm:"not null;default:''" json:"subject"`
	Body     string `gorm:"not null;default:''" json:"body"`
	// Status is "draft" until it is sent; only a draft can be changed.
	Status    string    `gorm:"not null;default:'draft'" json:"status"`
	CreatedBy string    `gorm:"not null;default:''" json:"created_by"`
	UpdatedBy string    `gorm:"not null;default:''" json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const (
	AudienceConversations = "conversations"
	AudienceCustomers     = "customers"
	CampaignDraft         = "draft"

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
	case "subject":
		// One line: a subject is a header.
		value = strings.Join(strings.Fields(value), " ")
		if utf8.RuneCountInString(value) > campaignSubjectLimit {
			return "", fmt.Errorf("a subject is %d characters at most", campaignSubjectLimit)
		}
	case "body":
		value = strings.TrimRight(strings.ReplaceAll(value, "\r\n", "\n"), " \n\t")
		if utf8.RuneCountInString(value) > campaignBodyLimit {
			return "", fmt.Errorf("a campaign is %d characters at most", campaignBodyLimit)
		}
	default:
		return "", fmt.Errorf("a campaign has no %q to set", field)
	}
	return value, nil
}

func (s *Store) AddCampaign(org int64, in CampaignIn, actor string) (*Campaign, error) {
	if in.Audience == "" {
		in.Audience = AudienceConversations
	}
	c := Campaign{OrgID: org, Status: CampaignDraft, CreatedBy: actor, UpdatedBy: actor}
	for _, f := range []struct {
		field string
		value string
		into  *string
	}{{"name", in.Name, &c.Name}, {"audience", in.Audience, &c.Audience}, {"subject", in.Subject, &c.Subject}, {"body", in.Body, &c.Body}} {
		v, err := campaignField(f.field, f.value)
		if err != nil {
			return nil, err
		}
		*f.into = v
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
		for _, ch := range changes {
			v, err := campaignField(ch.Field, ch.Value)
			if err != nil {
				return err
			}
			updates[ch.Field] = v
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
	if c.OrgID != o.ID {
		return nil, noSuch("no campaign %d", c.ID)
	}
	q := s.in(o.ID).Where("key not like 'org:%'").Where("status <> ?", ContactSuggested).Where("declined is null")
	switch c.Audience {
	case AudienceConversations:
		q = q.Where(inConversation)
	case AudienceCustomers:
		companies, err := s.customerCompanies(o.ID)
		if err != nil {
			return nil, err
		}
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
			} else if why := s.marketable(s.db, o, addr); why != nil {
				r.LeftOut = leftOutBecause(why)
			} else {
				seen[addr] = r.Name
			}
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LeftOut == "" && out[j].LeftOut != "" })
	return out, nil
}

func leftOutBecause(err error) string {
	switch {
	case errors.Is(err, errSuppressed):
		return "on the list not to be mailed"
	case errors.Is(err, errErased):
		return "erased on request"
	}
	return "on the never-contact list"
}

// customerCompanies are those with a subscription that hasn't ended.
func (s *Store) customerCompanies(org int64) ([]int64, error) {
	subs, err := s.Subscriptions(org, 0, today())
	if err != nil {
		return nil, err
	}
	var out []int64
	seen := map[int64]bool{}
	for _, sub := range subs {
		if sub.Status != SubEnded && !seen[sub.CompanyID] {
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

// CampaignCard is a campaign as its page shows it, with who it would go to now.
type CampaignCard struct {
	Campaign
	Going, LeftOut int
	// Shown is the start of the audience, those it goes to first.
	Shown []Recipient
}

const campaignShown = 30

func (a *App) campaignsPage(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	list, err := a.s.Campaigns(q.Org.ID)
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	cards := make([]CampaignCard, 0, len(list))
	for _, c := range list {
		people, err := a.s.Audience(q.Org, c)
		if err != nil {
			fail(w, err, http.StatusInternalServerError)
			return
		}
		card := CampaignCard{Campaign: c, Shown: people[:min(len(people), campaignShown)]}
		for _, p := range people {
			if p.LeftOut == "" {
				card.Going++
			} else {
				card.LeftOut++
			}
		}
		cards = append(cards, card)
	}
	a.page(w, r, "campaigns", CampaignsView(q, cards, q.Org.Settings.PostalAddress))
}
