package sales

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/shutthegoatup/prospector/internal/store"
)

// DealStore is the deals area's queries: the conversation's own deal and any others with someone.
type DealStore struct{ *store.Core }

// Deals are an organisation's deals by the contact each belongs to.
func (s *DealStore) Deals(org int64) (map[int64]store.Deal, error) {
	var rows []store.Deal
	if err := s.InOrg(org).Where("seq = 0").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]store.Deal, len(rows))
	for _, d := range rows {
		out[d.ContactID] = d
	}
	return out, nil
}

func (s *DealStore) SetDeal(tx *gorm.DB, org, contact int64, field, value, actor string) error {
	value = strings.TrimSpace(value)
	var c store.Contact
	if err := tx.Where("org_id = ? and id = ?", org, contact).First(&c).Error; err != nil {
		return store.Missing(err, "no contact %d", contact)
	}
	if c.Replied == nil {
		return errors.New("a deal is for someone who has replied; mark the reply first")
	}
	var o store.Org
	if err := tx.First(&o, org).Error; err != nil {
		return err
	}
	var d store.Deal
	if err := tx.Where("org_id = ? and contact_id = ? and seq = 0", org, contact).Limit(1).Find(&d).Error; err != nil {
		return err
	}
	// A deal already valued keeps its currency: the setting changing mustn't turn
	// pounds into euros the next time someone corrects the figure.
	currency := o.Settings.WithDefaults().Currency
	if d.Amount != 0 && d.Currency != "" {
		currency = d.Currency
	}
	updates, logged, err := dealChange(tx, org, d, currency, field, value)
	if err != nil {
		return err
	}
	if d.ID == 0 {
		// Two fields changed at once are two requests; whichever comes second finds the row.
		first := store.Deal{OrgID: org, ContactID: contact, Kind: DealNew, Currency: currency}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&first).Error; err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and contact_id = ? and seq = 0", org, contact).First(&d).Error; err != nil {
			return err
		}
	}
	if err := tx.Model(&d).Updates(updates).Error; err != nil {
		return err
	}
	return s.Log(tx, org, contact, c.CompanyID, actor, "deal:"+field, store.NonEmpty(logged))
}

// OtherDeals are the renewals, expansions and further deals, by the stage each stands
// at. One stays on the board when its person later declines, unlike their conversation:
// it has a value and an outcome of its own, and somebody has to close it.
func (s *DealStore) OtherDeals(org int64) (map[string][]OtherDeal, error) {
	var deals []store.Deal
	if err := s.InOrg(org).Where("seq > 0").Order("next_date nulls first, id").Find(&deals).Error; err != nil {
		return nil, err
	}
	if len(deals) == 0 {
		return map[string][]OtherDeal{}, nil
	}
	var people []store.Contact
	if err := s.InOrg(org).Where("id in (?)", s.InOrg(org).Model(&store.Deal{}).Select("contact_id").Where("seq > 0")).Find(&people).Error; err != nil {
		return nil, err
	}
	with := make(map[int64]store.Contact, len(people))
	for _, c := range people {
		with[c.ID] = c
	}
	out := map[string][]OtherDeal{}
	for _, d := range deals {
		if c, ok := with[d.ContactID]; ok {
			out[d.Stage] = append(out[d.Stage], OtherDeal{d, c})
		}
	}
	return out, nil
}

// OwnDeal is the deal a conversation is, empty if it has none yet.
func (s *DealStore) OwnDeal(org, contact int64) (*store.Deal, error) {
	var d store.Deal
	return &d, s.InOrg(org).Where("contact_id = ? and seq = 0", contact).Limit(1).Find(&d).Error
}

// StartDeal opens another deal with someone already in conversation: a renewal, an
// expansion or a second piece of new business. It starts at the first stage.
func (s *DealStore) StartDeal(org, contact int64, kind, actor string) (*store.Deal, error) {
	if !kindOfDeal(kind) {
		return nil, fmt.Errorf("unknown kind of deal %q", kind)
	}
	var d store.Deal
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		// Held, so two started together are numbered one after the other.
		if err := store.Hold(tx, &store.Contact{}, org, contact); err != nil {
			return err
		}
		var c store.Contact
		if err := tx.Where("org_id = ? and id = ?", org, contact).First(&c).Error; err != nil {
			return store.Missing(err, "no contact %d", contact)
		}
		if c.Replied == nil || c.Declined != nil || c.Status == store.ContactSuggested {
			return errors.New("another deal is for someone you are in conversation with")
		}
		var o store.Org
		if err := tx.First(&o, org).Error; err != nil {
			return err
		}
		var last int
		if err := tx.Model(&store.Deal{}).Where("org_id = ? and contact_id = ?", org, contact).
			Select("coalesce(max(seq), 0)").Scan(&last).Error; err != nil {
			return err
		}
		d = store.Deal{OrgID: org, ContactID: contact, Seq: last + 1, Kind: kind, Stage: store.Stages[0].Key, Currency: o.Settings.WithDefaults().Currency}
		if err := tx.Create(&d).Error; err != nil {
			return err
		}
		return s.Log(tx, org, contact, c.CompanyID, actor, "deal:start", &kind)
	})
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *DealStore) otherDeal(tx *gorm.DB, org, id int64) (store.Deal, store.Contact, error) {
	var d store.Deal
	if err := tx.Where("org_id = ? and id = ? and seq > 0", org, id).First(&d).Error; err != nil {
		return d, store.Contact{}, store.Missing(err, "no deal %d", id)
	}
	var c store.Contact
	if err := tx.Where("org_id = ? and id = ?", org, d.ContactID).First(&c).Error; err != nil {
		return d, c, store.Missing(err, "no contact %d", d.ContactID)
	}
	return d, c, nil
}

// ChangeDeal sets one field of a deal after the conversation's own. Those hold their
// stage and next step themselves; the conversation's own are still its contact's.
func (s *DealStore) ChangeDeal(org, id int64, field, value, actor string) error {
	return s.ChangeDealFields(org, id, []store.Change{{Field: field, Value: value}}, actor)
}

// ChangeDealFields makes several changes to a deal after the conversation's own, or
// none: one that is refused takes back those before it.
func (s *DealStore) ChangeDealFields(org, id int64, changes []store.Change, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		for _, c := range changes {
			if err := s.changeDeal(tx, org, id, c.Field, strings.TrimSpace(c.Value), actor); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetDealFields does the same for a conversation's own deal, which is its contact's.
func (s *DealStore) SetDealFields(org, contact int64, changes []store.Change, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		for _, c := range changes {
			if err := s.SetDeal(tx, org, contact, c.Field, c.Value, actor); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *DealStore) changeDeal(tx *gorm.DB, org, id int64, field, value, actor string) error {
	{
		d, c, err := s.otherDeal(tx, org, id)
		if err != nil {
			return err
		}
		currency := d.Currency
		if d.Amount == 0 || currency == "" {
			var o store.Org
			if err := tx.First(&o, org).Error; err != nil {
				return err
			}
			currency = o.Settings.WithDefaults().Currency
		}
		updates, logged, err := dealChange(tx, org, d, currency, field, value)
		if err != nil {
			return err
		}
		if err := tx.Model(&d).Updates(updates).Error; err != nil {
			return err
		}
		return s.Log(tx, org, c.ID, c.CompanyID, actor, "deal:"+field, store.NonEmpty(logged))
	}
}

// RemoveDeal takes back a deal that was started by mistake. One that ran its course is
// marked won or lost instead, and the conversation's own can't be removed.
func (s *DealStore) RemoveDeal(org, id int64, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		d, c, err := s.otherDeal(tx, org, id)
		if err != nil {
			return err
		}
		if err := tx.Delete(&d).Error; err != nil {
			return err
		}
		return s.Log(tx, org, c.ID, c.CompanyID, actor, "deal:remove", &d.Kind)
	})
}
