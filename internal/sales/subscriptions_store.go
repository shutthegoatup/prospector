package sales

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// SubscriptionStore is the subscriptions area's queries: what each customer is on, and its renewals.
type SubscriptionStore struct{ *store.Core }

func (s *SubscriptionStore) AddSubscription(org int64, in SubscriptionIn, actor string) (*store.Subscription, error) {
	in.Name = store.Clip(strings.TrimSpace(in.Name), 200)
	if in.Name == "" {
		return nil, say.New("say what they are on")
	}
	if in.Per == "" {
		in.Per = store.PerYear
	}
	if in.Then == "" {
		in.Then = ThenRenews
	}
	if in.Per != store.PerMonth && in.Per != store.PerYear {
		return nil, say.Errorf("a price is for a month or a year, not %q", in.Per)
	}
	if !validThen(in.Then) {
		return nil, say.Errorf("unknown end of term %q", in.Then)
	}
	if err := checkTerm(in.TermMonths, in.NoticeDays); err != nil {
		return nil, err
	}
	if in.TermMonths == 0 {
		// With nothing committed there is no term to renew or end: it rolls on.
		in.Then = store.ThenRolls
	}
	if _, err := store.ParseDay(in.Start); err != nil {
		return nil, err
	}
	amount, err := store.ParseMoney(in.Amount)
	if err != nil {
		return nil, err
	}
	var sub store.Subscription
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		var c store.Company
		if err := tx.Where("org_id = ? and id = ?", org, in.Company).First(&c).Error; err != nil {
			return store.Missing(err, "no company %d", in.Company)
		}
		var o store.Org
		if err := tx.First(&o, org).Error; err != nil {
			return err
		}
		sub = store.Subscription{OrgID: org, CompanyID: c.ID, Name: in.Name, Amount: amount, Currency: o.Settings.WithDefaults().Currency,
			Per: in.Per, Start: in.Start, TermMonths: in.TermMonths, Then: in.Then, NoticeDays: in.NoticeDays}
		if err := tx.Create(&sub).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, &c.ID, actor, "sub:new", &in.Start)
	})
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// ChangeSubscription sets one field. The history keeps dates and terms, which say what
// was agreed when; a name is free text and a price is the customer's business, so
// those are noted as changed and no more.
func (s *SubscriptionStore) ChangeSubscription(org, id int64, field, value, actor string) error {
	return s.ChangeSubscriptionFields(org, id, []SubscriptionChange{{field, value}}, actor)
}

// ChangeSubscriptionFields makes several changes or none: one that is refused takes
// back those before it.
func (s *SubscriptionStore) ChangeSubscriptionFields(org, id int64, changes []SubscriptionChange, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		for _, c := range changes {
			if err := s.changeSubscription(tx, org, id, c.Field, strings.TrimSpace(c.Value), actor); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SubscriptionStore) changeSubscription(tx *gorm.DB, org, id int64, field, value, actor string) error {
	{
		var sub store.Subscription
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&sub).Error; err != nil {
			return store.Missing(err, "no subscription %d", id)
		}
		updates := map[string]any{}
		logged := value
		switch field {
		case "name":
			if value == "" {
				return say.New("say what they are on")
			}
			updates["name"], logged = store.Clip(value, 200), ""
		case "amount":
			amount, err := store.ParseMoney(value)
			if err != nil {
				return err
			}
			updates["amount"], logged = amount, ""
			if sub.Amount == 0 || sub.Currency == "" {
				var o store.Org
				if err := tx.First(&o, org).Error; err != nil {
					return err
				}
				updates["currency"] = o.Settings.WithDefaults().Currency
			}
		case "per":
			if value != store.PerMonth && value != store.PerYear {
				return say.Errorf("a price is for a month or a year, not %q", value)
			}
			updates["per"] = value
		case "then":
			if !validThen(value) {
				return say.Errorf("unknown end of term %q", value)
			}
			if sub.TermMonths == 0 && value != store.ThenRolls {
				return say.New("with no months committed it rolls on: say how many months first")
			}
			updates["then"] = value
		case "start":
			if _, err := store.ParseDay(value); err != nil {
				return err
			}
			for _, later := range []*string{sub.NoticeOn, sub.EndedOn} {
				if later != nil && value > *later {
					return say.New("that is after notice was given or it ended: clear that date first")
				}
			}
			updates["start"] = value
		case "notice_on", "ended_on":
			if value != "" {
				if _, err := store.ParseDay(value); err != nil {
					return err
				}
				if value < sub.Start {
					return say.New("that is before it started")
				}
			}
			updates[field] = store.NonEmpty(value)
		case "term_months", "notice_days":
			n, err := strconv.Atoi(value)
			if err != nil {
				return say.Errorf("%q isn't a number", value)
			}
			months, notice := sub.TermMonths, sub.NoticeDays
			if field == "term_months" {
				months = n
			} else {
				notice = n
			}
			if err := checkTerm(months, notice); err != nil {
				return err
			}
			updates[field] = n
			if months == 0 {
				updates["then"] = store.ThenRolls
			}
		default:
			return say.Errorf("a subscription has no %q", field)
		}
		if err := tx.Model(&sub).Updates(updates).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, &sub.CompanyID, actor, "sub:"+field, store.NonEmpty(logged))
	}
}

// RemoveSubscription takes back one recorded by mistake. One that ran its course is
// given an end date instead, so the record of it stays.
func (s *SubscriptionStore) RemoveSubscription(org, id int64, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var sub store.Subscription
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&sub).Error; err != nil {
			return store.Missing(err, "no subscription %d", id)
		}
		if err := tx.Delete(&sub).Error; err != nil {
			return err
		}
		// A renewal deal for it carries on as a deal, pointing at nothing.
		if err := tx.Model(&store.Deal{}).Where("org_id = ? and subscription_id = ?", org, id).Update("subscription_id", 0).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, &sub.CompanyID, actor, "sub:remove", nil)
	})
}

// Subscriptions lists an organisation's, the one needing attention soonest first;
// company narrows it to one company's when it isn't 0.
func (s *SubscriptionStore) Subscriptions(org, company int64, day string) ([]SubscriptionRow, error) {
	q := s.InOrg(org)
	if company != 0 {
		q = q.Where("company_id = ?", company)
	}
	var subs []store.Subscription
	if err := q.Order("id").Find(&subs).Error; err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, nil
	}
	var companies []store.Company
	if err := s.InOrg(org).Where("id in (?)", s.InOrg(org).Model(&store.Subscription{}).Select("company_id")).Find(&companies).Error; err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(companies))
	for _, c := range companies {
		names[c.ID] = c.Name
	}
	rows := make([]SubscriptionRow, len(subs))
	for i, sub := range subs {
		rows[i] = SubscriptionRow{Subscription: sub, Company: names[sub.CompanyID], Standing: sub.On(day)}
	}
	var renewing []int64
	if err := s.InOrg(org).Model(&store.Deal{}).Where("subscription_id <> 0 and stage not in ?", []string{"won", "lost"}).
		Pluck("subscription_id", &renewing).Error; err != nil {
		return nil, err
	}
	var people []store.Contact
	if err := s.InOrg(org).Where("company_id in (?)", s.InOrg(org).Model(&store.Subscription{}).Select("company_id")).
		Where(store.InConversation).Order("id").Find(&people).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].Renewing = slices.Contains(renewing, rows[i].ID)
		for _, p := range people {
			if p.CompanyID != nil && *p.CompanyID == rows[i].CompanyID {
				rows[i].People = append(rows[i].People, p)
			}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Attention() < rows[j].Attention() })
	return rows, nil
}

// StartRenewal opens a renewal deal for a subscription with someone at its company who
// is already in conversation. The deal starts with what the subscription costs for a
// year and closes when the term in hand ends, both of which can then be changed.
func (s *SubscriptionStore) StartRenewal(org, subscription, contact int64, actor string) (*store.Deal, error) {
	var d store.Deal
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		// The subscription is held so two people can't each start its renewal with a
		// different person at once, and the contact so their deals are numbered in turn.
		if err := store.Hold(tx, &store.Subscription{}, org, subscription); err != nil {
			return err
		}
		if err := store.Hold(tx, &store.Contact{}, org, contact); err != nil {
			return err
		}
		var sub store.Subscription
		if err := tx.Where("org_id = ? and id = ?", org, subscription).First(&sub).Error; err != nil {
			return store.Missing(err, "no subscription %d", subscription)
		}
		var c store.Contact
		if err := tx.Where("org_id = ? and id = ?", org, contact).First(&c).Error; err != nil {
			return store.Missing(err, "no contact %d", contact)
		}
		if c.CompanyID == nil || *c.CompanyID != sub.CompanyID {
			return say.New("that person is not at the company the subscription is with")
		}
		if c.Replied == nil || c.Declined != nil || c.Status == store.ContactSuggested {
			return say.New("a renewal is with someone you are in conversation with")
		}
		st := sub.On(store.Today())
		if st.Status == store.SubEnded {
			return say.New("that subscription has ended: record a new one when they come back")
		}
		var open int64
		if err := tx.Model(&store.Deal{}).Where("org_id = ? and subscription_id = ? and stage not in ?", org, sub.ID, []string{"won", "lost"}).
			Count(&open).Error; err != nil {
			return err
		}
		if open > 0 {
			return store.Conflict{Text: say.New("a renewal for it is already under way on the Conversations board")}
		}
		var last int
		if err := tx.Model(&store.Deal{}).Where("org_id = ? and contact_id = ?", org, contact).
			Select("coalesce(max(seq), 0)").Scan(&last).Error; err != nil {
			return err
		}
		currency := sub.Currency
		if currency == "" {
			var o store.Org
			if err := tx.First(&o, org).Error; err != nil {
				return err
			}
			currency = o.Settings.WithDefaults().Currency
		}
		closes := st.TermEnd
		if st.Ends != "" {
			closes = st.Ends
		}
		d = store.Deal{OrgID: org, ContactID: contact, Seq: last + 1, Kind: "renewal", Stage: store.Stages[0].Key, SubscriptionID: sub.ID,
			Amount: sub.Yearly(), Currency: currency, CloseDate: store.NonEmpty(closes)}
		if err := tx.Create(&d).Error; err != nil {
			return err
		}
		return s.Log(tx, org, contact, c.CompanyID, actor, "deal:start", store.Str("renewal"))
	})
	if err != nil {
		return nil, err
	}
	return &d, nil
}
