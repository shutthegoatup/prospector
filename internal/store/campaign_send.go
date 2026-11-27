package store

import "time"

// CampaignRecipient is one mail of a campaign: whom it is to, and how it went. The rows
// are made when the campaign is sent, so who it was for is fixed then, and each is
// claimed just before its mail goes and never sent again: a server stopped mid-send
// leaves at most that one mail uncertain, and never mails anyone twice.
type CampaignRecipient struct {
	ID         int64  `gorm:"primaryKey" json:"id"`
	OrgID      int64  `gorm:"index;index:idx_recipients_org_status_sent,priority:1;not null" json:"-"`
	CampaignID int64  `gorm:"uniqueIndex:idx_campaign_address,priority:1;index:idx_campaign_status,priority:1;not null" json:"campaign_id"`
	Address    string `gorm:"uniqueIndex:idx_campaign_address,priority:2;not null" json:"address"`
	// ContactID is who it was for, until they are erased or a restore drops them.
	ContactID *int64 `gorm:"index" json:"contact_id"`
	Name      string `gorm:"not null;default:''" json:"name"`
	Status    string `gorm:"index:idx_campaign_status,priority:2;index:idx_recipients_status_claimed,priority:1;index:idx_recipients_org_status_sent,priority:2;not null;default:'queued'" json:"status"`
	// Reason says why one wasn't sent, or why it is unknown whether it went.
	Reason    string     `gorm:"not null;default:''" json:"reason"`
	ClaimedAt *time.Time `gorm:"index:idx_recipients_status_claimed,priority:2" json:"-"`
	SentAt    *time.Time `gorm:"index:idx_recipients_org_status_sent,priority:3" json:"sent_at"`
}
