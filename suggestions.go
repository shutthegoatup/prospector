package main

import (
	"strings"
	"time"
)

// Suggestion is an email, phone or profile an agent found for someone already here.
// It changes nothing until a person accepts it.
type Suggestion struct {
	ID          int64  `gorm:"primaryKey" json:"id"`
	OrgID       int64  `gorm:"index;not null" json:"-"`
	ContactID   int64  `gorm:"index;not null" json:"contact_id"`
	Field       string `gorm:"not null" json:"field"`
	Value       string `gorm:"not null" json:"value"`
	EmailStatus string `gorm:"not null;default:''" json:"email_status,omitempty"`
	SourceURL   string `gorm:"not null;default:''" json:"source_url"`
	// Vendor is the enrichment vendor that found it, which stands in for a page.
	Vendor    string     `gorm:"not null;default:''" json:"vendor,omitempty"`
	By        string     `gorm:"not null" json:"by"`
	KeyID     int64      `gorm:"not null;default:0" json:"-"`
	At        time.Time  `json:"at"`
	Decision  string     `gorm:"index;not null;default:''" json:"decision,omitempty"`
	DecidedBy string     `gorm:"not null;default:''" json:"decided_by,omitempty"`
	DecidedAt *time.Time `json:"decided_at,omitempty"`
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

func waitingFor(before []Suggestion, field, value string) bool {
	for _, b := range before {
		if b.Field == field && sameDetail(field, b.Value, value) {
			return b.Decision == ""
		}
	}
	return false
}

// PendingDetail is a waiting suggestion beside what the person has now.
type PendingDetail struct {
	Suggestion
	Current string
}
