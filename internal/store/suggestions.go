package store

import "time"

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
