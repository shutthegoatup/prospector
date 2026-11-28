package prospecting

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

func (s *ProspectingStore) Enrichers(org int64) ([]store.Enricher, error) {
	var out []store.Enricher
	return out, s.DB.Where("org_id = ?", org).Order("position, id").Find(&out).Error
}

// SaveEnricher adds a vendor, or changes the one of that kind. An empty key keeps
// the saved one.
func (s *ProspectingStore) SaveEnricher(org int64, in EnricherIn, by string) (*store.Enricher, error) {
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if Vendors[kind] == nil {
		return nil, say.Errorf("unknown vendor %q: one of %s", in.Kind, strings.Join(vendorKinds(), ", "))
	}
	if in.MonthlyLimit < 0 || in.MonthlyLimit > maxMonthlyLimit {
		return nil, say.Errorf("monthly_limit is from 0 to %d", maxMonthlyLimit)
	}
	var e store.Enricher
	if err := s.DB.Where("org_id = ? and kind = ?", org, kind).Limit(1).Find(&e).Error; err != nil {
		return nil, err
	}
	key := strings.TrimSpace(in.APIKey)
	if key == "" && e.APIKey == "" {
		return nil, say.New("api_key is needed: the vendor's key for your account")
	}
	if key != "" {
		sealedKey, err := s.Keyring.Seal(key)
		if err != nil {
			return nil, err
		}
		e.APIKey = sealedKey
	}
	e.OrgID, e.Kind, e.Position, e.MonthlyLimit, e.UpdatedBy, e.UpdatedAt = org, kind, in.Position, in.MonthlyLimit, by, time.Now()
	return &e, s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&e).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, nil, by, "enricher:save", &e.Kind)
	})
}

func (s *ProspectingStore) DeleteEnricher(org, id int64, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var e store.Enricher
		res := tx.Clauses(clause.Returning{}).Where("org_id = ? and id = ?", org, id).Delete(&e)
		if res.Error == nil && res.RowsAffected == 0 {
			return store.NoSuch("no vendor %d in this organisation", id)
		}
		if res.Error != nil {
			return res.Error
		}
		return s.Log(tx, org, 0, nil, actor, "enricher:remove", &e.Kind)
	})
}

func (s *ProspectingStore) EnricherUsed(org int64, kind string, now time.Time) int {
	var u store.EnrichUsage
	_ = store.Logged(s.DB.Where("org_id = ? and kind = ? and month = ?", org, kind, now.UTC().Format("2006-01")).Limit(1).Find(&u))
	return u.Calls
}

// Spend takes one lookup of a vendor's month, unless its limit is reached. One
// statement, so two lookups at once can't both take the last.
func (s *ProspectingStore) Spend(org int64, e store.Enricher, now time.Time) (bool, error) {
	month := now.UTC().Format("2006-01")
	if err := s.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&store.EnrichUsage{OrgID: org, Kind: e.Kind, Month: month}).Error; err != nil {
		return false, err
	}
	res := s.DB.Model(&store.EnrichUsage{}).Where("org_id = ? and kind = ? and month = ? and calls < ?", org, e.Kind, month, e.MonthlyLimit).
		Update("calls", gorm.Expr("calls + 1"))
	return res.RowsAffected == 1, res.Error
}

// KeepFind suggests what a vendor found for a person, and reports whether it was new.
func (s *ProspectingStore) KeepFind(org int64, who store.Actor, contact int64, field, kind, value, status string) (made bool, err error) {
	return made, s.DB.Transaction(func(tx *gorm.DB) error {
		if err := store.Hold(tx, &store.Contact{}, org, contact); err != nil {
			return err
		}
		var c store.Contact
		if err := tx.Where("org_id = ? and id = ?", org, contact).First(&c).Error; err != nil {
			return store.Missing(err, "no contact %d in this organisation", contact)
		}
		if made, err = s.suggestOne(tx, who, c, field, value, status, "", kind); err != nil {
			return err
		}
		return s.LogAs(tx, org, c.ID, c.CompanyID, who, "enrich:"+field, new(kind+": "+LookupFound))
	})
}

// AskedLately is when a vendor was last asked for this field of this person, if it
// was recently enough that asking again would only spend a lookup.
func (s *ProspectingStore) AskedLately(org, contact int64, field, kind string) *time.Time {
	var e store.Event
	res := s.DB.Where("org_id = ? and contact_id = ? and action = ? and value like ? and at > ?", org, contact, "enrich:"+field, kind+":%", time.Now().Add(-EnrichAgain)).
		Order("at desc").Limit(1).Find(&e)
	if store.Logged(res) != nil || res.RowsAffected == 0 {
		return nil
	}
	return &e.At
}

// lookupsOf are each vendor's latest lookup per detail for these people, within the
// time a vendor isn't asked again, newest first.
func (s *ProspectingStore) lookupsOf(org int64, people []int64) (map[int64][]Lookup, error) {
	out := map[int64][]Lookup{}
	if len(people) == 0 {
		return out, nil
	}
	var events []store.Event
	if err := s.DB.Where("org_id = ? and contact_id in ? and action like 'enrich:%' and at > ?", org, people, time.Now().Add(-EnrichAgain)).
		Order("at desc, id desc").Find(&events).Error; err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, e := range events {
		kind, said, ok := strings.Cut(store.Deref(e.Value), ": ")
		if !ok {
			continue
		}
		field := strings.TrimPrefix(e.Action, "enrich:")
		if k := fmt.Sprint(e.ContactID, field, kind); !seen[k] {
			seen[k] = true
			out[e.ContactID] = append(out[e.ContactID], Lookup{Field: field, Vendor: kind, Outcome: said, At: e.At})
		}
	}
	return out, nil
}

// LogEnrich records a lookup that kept nothing, so the same one isn't paid for again soon.
func (s *ProspectingStore) LogEnrich(org int64, who store.Actor, contact int64, field, value string) error {
	c, err := s.CRM.ContactByID(org, contact)
	if err != nil {
		return err
	}
	return s.LogAs(s.DB, org, c.ID, c.CompanyID, who, "enrich:"+field, &value)
}
