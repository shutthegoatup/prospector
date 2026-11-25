package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Suggestion is an email, phone or profile an agent found for someone already here.
// It changes nothing until a person accepts it.
type Suggestion struct {
	ID          int64      `gorm:"primaryKey" json:"id"`
	OrgID       int64      `gorm:"index;not null" json:"-"`
	ContactID   int64      `gorm:"index;not null" json:"contact_id"`
	Field       string     `gorm:"not null" json:"field"`
	Value       string     `gorm:"not null" json:"value"`
	EmailStatus string     `gorm:"not null;default:''" json:"email_status,omitempty"`
	SourceURL   string     `gorm:"not null;default:''" json:"source_url"`
	By          string     `gorm:"not null" json:"by"`
	KeyID       int64      `gorm:"not null;default:0" json:"-"`
	At          time.Time  `json:"at"`
	Decision    string     `gorm:"index;not null;default:''" json:"decision,omitempty"`
	DecidedBy   string     `gorm:"not null;default:''" json:"decided_by,omitempty"`
	DecidedAt   *time.Time `json:"decided_at,omitempty"`
}

const (
	SuggestionAccepted  = "accepted"
	SuggestionDismissed = "dismissed"
)

var suggestable = []string{"email", "phone", "linkedin"}

// maxWaiting is how many suggestions for one field of one person may wait at once, and
// maxDetail how long one may be.
const (
	maxWaiting = 3
	maxDetail  = 320
)

// sameDetail compares the way a person would: an address by its letters, a profile by
// its path, a number by its digits.
func sameDetail(field, a, b string) bool {
	switch field {
	case "email":
		// The column can hold several addresses; one of them is already held.
		for _, held := range emailRe.FindAllString(strings.ToLower(b), -1) {
			if strings.EqualFold(strings.TrimSpace(a), strings.Trim(held, ".")) {
				return true
			}
		}
	case "linkedin":
		if pa, pb := linkedinPath(a), linkedinPath(b); pa != "" || pb != "" {
			return pa == pb
		}
	case "phone":
		return digits(a) == digits(b)
	}
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func digits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

func detailOf(c Contact, field string) string {
	switch field {
	case "email":
		return deref(c.Email)
	case "phone":
		return deref(c.Phone)
	case "linkedin":
		return deref(c.Linkedin)
	}
	return ""
}

// suggestDetails keeps what an agent sent for someone already here and that they
// don't have, and returns the fields now waiting. One already suggested, whether it
// is still waiting or was dismissed, isn't raised again. Someone matched by address
// or profile under another name gets nothing: the rest may be a colleague's.
func (s *Store) suggestDetails(tx *gorm.DB, who Actor, p Contact, in AddContactIn) ([]string, error) {
	if !strings.EqualFold(personKey(in.Name), personKey(displayName(p))) {
		return []string{}, nil
	}
	sent := map[string]string{"email": in.Email, "phone": in.Phone, "linkedin": in.Linkedin}
	var before []Suggestion
	if err := tx.Where("org_id = ? and contact_id = ?", p.OrgID, p.ID).Find(&before).Error; err != nil {
		return nil, err
	}
	waiting := []string{}
	for _, field := range suggestable {
		value := strings.TrimSpace(sent[field])
		if value == "" || sameDetail(field, value, detailOf(p, field)) {
			continue
		}
		known, pending := false, 0
		for _, b := range before {
			if b.Field == field && b.Decision == "" {
				pending++
			}
			if b.Field == field && sameDetail(field, b.Value, value) {
				known = true
				if b.Decision == "" {
					waiting = append(waiting, field)
				}
				break
			}
		}
		// An agent trying one guess after another mustn't bury the person's page.
		if known || pending >= maxWaiting || len(value) > maxDetail {
			continue
		}
		row := Suggestion{OrgID: p.OrgID, ContactID: p.ID, Field: field, Value: value, SourceURL: strings.TrimSpace(in.SourceURL),
			By: who.Name, KeyID: who.KeyID, At: time.Now()}
		if field == "email" {
			row.EmailStatus = in.EmailStatus
		}
		if err := tx.Create(&row).Error; err != nil {
			return nil, err
		}
		// The detail itself stays out of the history.
		if err := s.logAs(tx, p.OrgID, p.ID, p.CompanyID, who, "suggest:"+field, nil); err != nil {
			return nil, err
		}
		waiting = append(waiting, field)
	}
	return waiting, nil
}

// PendingDetail is a waiting suggestion beside what the person has now.
type PendingDetail struct {
	Suggestion
	Current string
}

// Pending groups the suggestions still waiting by the person they are for.
func (s *Store) Pending(org int64, people []Contact) (map[int64][]PendingDetail, error) {
	out := map[int64][]PendingDetail{}
	if len(people) == 0 {
		return out, nil
	}
	byID := map[int64]Contact{}
	ids := make([]int64, len(people))
	for i, p := range people {
		byID[p.ID], ids[i] = p, p.ID
	}
	var rows []Suggestion
	if err := s.db.Where("org_id = ? and decision = '' and contact_id in ?", org, ids).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		// They have since been given it some other way.
		if sameDetail(r.Field, r.Value, detailOf(byID[r.ContactID], r.Field)) {
			continue
		}
		out[r.ContactID] = append(out[r.ContactID], PendingDetail{Suggestion: r, Current: detailOf(byID[r.ContactID], r.Field)})
	}
	return out, nil
}

// DecideSuggestion accepts a suggestion, which sets the detail as if the person had
// typed it, or dismisses it. Either way it is settled once.
func (s *Store) DecideSuggestion(org, id int64, decision, actor string) error {
	if decision != SuggestionAccepted && decision != SuggestionDismissed {
		return fmt.Errorf("unknown decision %q", decision)
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var sg Suggestion
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&sg).Error; err != nil {
			return noSuch("no suggestion %d", id)
		}
		if sg.Decision != "" {
			return errors.New("that suggestion was already " + sg.Decision + " by " + sg.DecidedBy)
		}
		now := time.Now()
		res := tx.Model(&Suggestion{}).Where("id = ? and decision = ''", sg.ID).
			Updates(map[string]any{"decision": decision, "decided_by": actor, "decided_at": &now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errors.New("someone else just settled that suggestion")
		}
		if decision == SuggestionDismissed {
			// A restore can leave a suggestion behind without its person.
			var c Contact
			if err := tx.Where("org_id = ? and id = ?", org, sg.ContactID).Limit(1).Find(&c).Error; err != nil {
				return err
			}
			return s.log(tx, org, c.ID, c.CompanyID, actor, "dismiss:"+sg.Field, nil)
		}
		if err := s.set(tx, org, sg.ContactID, sg.Field, sg.Value, actor); err != nil {
			return err
		}
		if sg.Field == "email" {
			return tx.Model(&Contact{}).Where("org_id = ? and id = ?", org, sg.ContactID).Update("email_status", sg.EmailStatus).Error
		}
		return nil
	})
}
