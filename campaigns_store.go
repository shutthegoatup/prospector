package main

import (
	"sort"
	"strings"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/say"
)

// outreachStore is the outreach area's queries: campaigns, sending them, bounces and whether a domain takes mail.
type outreachStore struct {
	*core
	// Mail goes only where suppression allows, a campaign can be for those subscribed,
	// and a bounce names its organisation by slug.
	identity      *identityStore
	suppression   *suppressionStore
	subscriptions *subscriptionStore
}

func (s *outreachStore) AddCampaign(org int64, in CampaignIn, actor string) (*Campaign, error) {
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
	if err := c.Fits(); err != nil {
		return nil, err
	}
	return &c, s.DB.Create(&c).Error
}

// ChangeCampaign makes several changes to a draft, or none.
func (s *outreachStore) ChangeCampaign(org, id int64, changes []Change, actor string) (*Campaign, error) {
	var c Campaign
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
			return missing(err, "no campaign %d", id)
		}
		if c.Status != CampaignDraft {
			return say.New("only a draft can be changed")
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
		if err := after.Fits(); err != nil {
			return err
		}
		// Held to the draft it was read as: one sent in the meantime is left as it went.
		res := tx.Model(&Campaign{}).Where("org_id = ? and id = ? and status = ?", org, id, CampaignDraft).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return say.New("only a draft can be changed")
		}
		return tx.Where("org_id = ? and id = ?", org, id).First(&c).Error
	})
	return &c, err
}

func (s *outreachStore) RemoveCampaign(org, id int64) error {
	res := s.InOrg(org).Where("id = ? and status = ?", id, CampaignDraft).Delete(&Campaign{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var n int64
		if err := s.InOrg(org).Model(&Campaign{}).Where("id = ?", id).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return noSuch("no campaign %d", id)
		}
		return say.New("only a draft can be removed; one that went is kept as a record of what was sent")
	}
	return nil
}

// Campaigns are an organisation's, newest first.
func (s *outreachStore) Campaigns(org int64) ([]Campaign, error) {
	out := []Campaign{}
	return out, s.InOrg(org).Order("id desc").Find(&out).Error
}

func (s *outreachStore) Campaign(org, id int64) (*Campaign, error) {
	var c Campaign
	if err := s.InOrg(org).Where("id = ?", id).First(&c).Error; err != nil {
		return nil, missing(err, "no campaign %d", id)
	}
	return &c, nil
}

// Audience is everyone a campaign is for, those it goes to first, each address once.
func (s *outreachStore) Audience(o *Org, c Campaign) ([]Recipient, error) {
	w, err := s.audienceWork(o)
	if err != nil {
		return nil, err
	}
	return s.audience(o, c, w)
}

func (s *outreachStore) audienceWork(o *Org) (*audienceWork, error) {
	g, err := s.suppression.Gate(s.DB, o)
	if err != nil {
		return nil, err
	}
	customers, err := s.CustomerCompanies(o.ID)
	if err != nil {
		return nil, err
	}
	return &audienceWork{gate: g, customers: customers, noMail: s.NoMailDomains()}, nil
}

func (s *outreachStore) audience(o *Org, c Campaign, w *audienceWork) ([]Recipient, error) {
	if c.OrgID != o.ID {
		return nil, noSuch("no campaign %d", c.ID)
	}
	q := s.InOrg(o.ID).Where("key not like 'org:%'").Where("status <> ?", ContactSuggested).Where("declined is null")
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
		return nil, say.Errorf("a campaign goes to %q or %q", AudienceConversations, AudienceCustomers)
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
			} else if why := w.gate.WhyFor(c.Kind, addr, deref(p.Linkedin)); why != nil {
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

// CustomerCompanies are those with a subscription running today: started, not ended.
func (s *outreachStore) CustomerCompanies(org int64) ([]int64, error) {
	subs, err := s.subscriptions.Subscriptions(org, 0, today())
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

func (s *outreachStore) companyNames(org int64, people []Contact) (map[int64]string, error) {
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
	if err := s.InOrg(org).Where("id in ?", ids).Find(&companies).Error; err != nil {
		return nil, err
	}
	for _, c := range companies {
		names[c.ID] = c.Name
	}
	return names, nil
}
