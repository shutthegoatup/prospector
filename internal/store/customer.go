package store

import "time"

// CustomerGrant lets one contact see one onboarding.
type CustomerGrant struct {
	ID           int64      `gorm:"primaryKey" json:"id"`
	OrgID        int64      `gorm:"uniqueIndex:idx_customer_grant,priority:1;not null" json:"-"`
	OnboardingID int64      `gorm:"uniqueIndex:idx_customer_grant,priority:2;not null" json:"onboarding_id"`
	ContactID    int64      `gorm:"uniqueIndex:idx_customer_grant,priority:3;index;not null" json:"contact_id"`
	InvitedBy    string     `gorm:"not null;default:''" json:"invited_by"`
	CreatedAt    time.Time  `json:"created_at"`
	RevokedAt    *time.Time `json:"revoked_at"`
}

// CustomerLink is a mailed link, stored as its hash. It opens the page once.
type CustomerLink struct {
	ID        int64  `gorm:"primaryKey"`
	OrgID     int64  `gorm:"index;not null"`
	GrantID   int64  `gorm:"index;not null"`
	TokenHash string `gorm:"uniqueIndex;not null"`
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// CustomerSession is one browser the page was opened in, stored as its cookie's hash.
type CustomerSession struct {
	ID        int64  `gorm:"primaryKey"`
	OrgID     int64  `gorm:"index;not null"`
	GrantID   int64  `gorm:"index;not null"`
	TokenHash string `gorm:"uniqueIndex;not null"`
	ExpiresAt time.Time
	CreatedAt time.Time
}

func (l CustomerLink) Opens() bool {
	return l.UsedAt == nil && l.ExpiresAt.After(time.Now())
}
