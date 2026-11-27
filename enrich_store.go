package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *prospectingStore) Enrichers(org int64) ([]Enricher, error) {
	var out []Enricher
	return out, s.db.Where("org_id = ?", org).Order("position, id").Find(&out).Error
}

// SaveEnricher adds a vendor, or changes the one of that kind. An empty key keeps
// the saved one.
func (s *prospectingStore) SaveEnricher(org int64, in EnricherIn, by string) (*Enricher, error) {
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if vendors[kind] == nil {
		return nil, fmt.Errorf("unknown vendor %q: one of %s", in.Kind, strings.Join(vendorKinds(), ", "))
	}
	if in.MonthlyLimit < 0 || in.MonthlyLimit > maxMonthlyLimit {
		return nil, fmt.Errorf("monthly_limit is from 0 to %d", maxMonthlyLimit)
	}
	var e Enricher
	if err := s.db.Where("org_id = ? and kind = ?", org, kind).Limit(1).Find(&e).Error; err != nil {
		return nil, err
	}
	key := strings.TrimSpace(in.APIKey)
	if key == "" && e.APIKey == "" {
		return nil, errors.New("api_key is needed: the vendor's key for your account")
	}
	if key != "" {
		sealedKey, err := s.keys.seal(key)
		if err != nil {
			return nil, err
		}
		e.APIKey = sealedKey
	}
	e.OrgID, e.Kind, e.Position, e.MonthlyLimit, e.UpdatedBy, e.UpdatedAt = org, kind, in.Position, in.MonthlyLimit, by, time.Now()
	return &e, s.db.Save(&e).Error
}

func (s *prospectingStore) DeleteEnricher(org, id int64) error {
	res := s.db.Where("org_id = ? and id = ?", org, id).Delete(&Enricher{})
	if res.Error == nil && res.RowsAffected == 0 {
		return noSuch("no vendor %d in this organisation", id)
	}
	return res.Error
}

func (s *prospectingStore) EnricherUsed(org int64, kind string, now time.Time) int {
	var u EnrichUsage
	_ = logged(s.db.Where("org_id = ? and kind = ? and month = ?", org, kind, now.UTC().Format("2006-01")).Limit(1).Find(&u))
	return u.Calls
}

// Spend takes one lookup of a vendor's month, unless its limit is reached. One
// statement, so two lookups at once can't both take the last.
func (s *prospectingStore) Spend(org int64, e Enricher, now time.Time) (bool, error) {
	month := now.UTC().Format("2006-01")
	if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&EnrichUsage{OrgID: org, Kind: e.Kind, Month: month}).Error; err != nil {
		return false, err
	}
	res := s.db.Model(&EnrichUsage{}).Where("org_id = ? and kind = ? and month = ? and calls < ?", org, e.Kind, month, e.MonthlyLimit).
		Update("calls", gorm.Expr("calls + 1"))
	return res.RowsAffected == 1, res.Error
}

// KeepFind suggests what a vendor found for a person, and reports whether it was new.
func (s *prospectingStore) KeepFind(org int64, who Actor, contact int64, field, kind, value, status string) (made bool, err error) {
	return made, s.db.Transaction(func(tx *gorm.DB) error {
		if err := hold(tx, &Contact{}, org, contact); err != nil {
			return err
		}
		var c Contact
		if err := tx.Where("org_id = ? and id = ?", org, contact).First(&c).Error; err != nil {
			return missing(err, "no contact %d in this organisation", contact)
		}
		if made, err = s.suggestOne(tx, who, c, field, value, status, "", kind); err != nil {
			return err
		}
		return s.logAs(tx, org, c.ID, c.CompanyID, who, "enrich:"+field, str(kind+": "+lookupFound))
	})
}

// Erased says so if an address or a profile belongs to someone erased on request.
func (s *prospectingStore) Erased(org int64, email, link string) error {
	return erased(s.db, org, "", "", email, link)
}

// AskedLately is when a vendor was last asked for this field of this person, if it
// was recently enough that asking again would only spend a lookup.
func (s *prospectingStore) AskedLately(org, contact int64, field, kind string) *time.Time {
	var e Event
	res := s.db.Where("org_id = ? and contact_id = ? and action = ? and value like ? and at > ?", org, contact, "enrich:"+field, kind+":%", time.Now().Add(-enrichAgain)).
		Order("at desc").Limit(1).Find(&e)
	if logged(res) != nil || res.RowsAffected == 0 {
		return nil
	}
	return &e.At
}

// lookupsOf are each vendor's latest lookup per detail for these people, within the
// time a vendor isn't asked again, newest first.
func (s *prospectingStore) lookupsOf(org int64, people []int64) (map[int64][]Lookup, error) {
	out := map[int64][]Lookup{}
	if len(people) == 0 {
		return out, nil
	}
	var events []Event
	if err := s.db.Where("org_id = ? and contact_id in ? and action like 'enrich:%' and at > ?", org, people, time.Now().Add(-enrichAgain)).
		Order("at desc, id desc").Find(&events).Error; err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, e := range events {
		kind, said, ok := strings.Cut(deref(e.Value), ": ")
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
func (s *prospectingStore) LogEnrich(org int64, who Actor, contact int64, field, value string) error {
	c, err := s.ContactByID(org, contact)
	if err != nil {
		return err
	}
	return s.logAs(s.db, org, c.ID, c.CompanyID, who, "enrich:"+field, &value)
}
