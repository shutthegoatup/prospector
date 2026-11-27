package store

import "time"

// Suppression is an address the organisation's marketing mail never goes to again. One
// list serves every mailing: someone who unsubscribed from one has unsubscribed.
type Suppression struct {
	ID      int64  `gorm:"primaryKey" json:"id"`
	OrgID   int64  `gorm:"uniqueIndex:idx_suppression_address,priority:1;not null" json:"-"`
	Address string `gorm:"uniqueIndex:idx_suppression_address,priority:2;not null" json:"address"`
	// Reason is "unsubscribed" when they asked, by a link in a mail; "complained" when
	// they marked one as spam; "added" when a member or key put them here; and "bounced"
	// when it is here only because mail to it came back for good. Only an added or
	// bounced one can be taken off.
	Reason    string    `gorm:"not null;default:'added'" json:"reason"`
	By        string    `gorm:"not null;default:''" json:"by"`
	CreatedAt time.Time `json:"created_at"`
	// BouncedAt is when mail to it last came back for good, whatever the reason: a bounce
	// is about the address, not their wishes, so it is kept beside them.
	BouncedAt *time.Time `json:"bounced_at"`
}

// UnsubscribeLink is the secret in one mail's unsubscribe link, kept as its hash. It
// never runs out, because the mail it is in doesn't.
type UnsubscribeLink struct {
	ID        int64     `gorm:"primaryKey"`
	OrgID     int64     `gorm:"index;not null"`
	Address   string    `gorm:"index;not null"`
	TokenHash string    `gorm:"uniqueIndex;not null"`
	CreatedAt time.Time `gorm:"index"`
}
