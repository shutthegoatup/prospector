package store

import "time"

// Enricher is a data vendor an organisation pays, under its own key, to find a detail
// its people and agents couldn't. Vendors are asked in Position order, each no more
// than MonthlyLimit times a calendar month.
type Enricher struct {
	ID    int64  `gorm:"primaryKey"`
	OrgID int64  `gorm:"uniqueIndex:idx_enrichers_org_kind;not null"`
	Kind  string `gorm:"uniqueIndex:idx_enrichers_org_kind;not null"`
	// APIKey is sealed under the server's key, never kept or shown in clear.
	APIKey       string `gorm:"not null;default:''"`
	Position     int    `gorm:"not null;default:0"`
	MonthlyLimit int    `gorm:"not null;default:0"`
	UpdatedBy    string `gorm:"not null;default:''"`
	UpdatedAt    time.Time
}

// EnrichUsage counts the lookups asked of one vendor in one month, so the limit holds
// across servers and restarts.
type EnrichUsage struct {
	ID    int64  `gorm:"primaryKey"`
	OrgID int64  `gorm:"uniqueIndex:idx_enrich_usage;not null"`
	Kind  string `gorm:"uniqueIndex:idx_enrich_usage;not null"`
	Month string `gorm:"uniqueIndex:idx_enrich_usage;not null"`
	Calls int    `gorm:"not null;default:0"`
}

// Unlookable says why nothing about someone may be sent to a vendor, or "" if it may:
// they declined, or they or their company's domain is on the never-contact list.
func (set OrgSettings) Unlookable(c Contact, domain string) string {
	if c.Declined != nil {
		return "they declined: nothing about them is looked up"
	}
	if err := set.Listed(Deref(c.Email), Deref(c.Linkedin)); err != nil {
		return err.Error()
	}
	if domain != "" {
		if err := set.Listed("someone@"+domain, ""); err != nil {
			return err.Error()
		}
	}
	return ""
}
