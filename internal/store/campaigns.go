package store

import (
	"time"

	"github.com/shutthegoatup/prospector/internal/say"
)

// Campaign is a marketing mail written once for many: everyone in conversation, or
// every customer. It goes only to people the organisation already has something with:
// cold outreach stays one person writing to one person.
type Campaign struct {
	ID       int64  `gorm:"primaryKey" json:"id"`
	OrgID    int64  `gorm:"index;not null" json:"-"`
	Name     string `gorm:"not null;default:''" json:"name"`
	Audience string `gorm:"not null;default:'conversations'" json:"audience"`
	// Kind is "marketing", or "service": a notice to current customers about what they have
	// (a price or terms change), which reaches them even if they unsubscribed from marketing.
	Kind    string `gorm:"not null;default:'marketing'" json:"kind"`
	Subject string `gorm:"not null;default:''" json:"subject"`
	Body    string `gorm:"not null;default:''" json:"body"`
	// Status is "draft" until it is sent; only a draft can be changed.
	Status    string    `gorm:"not null;default:'draft'" json:"status"`
	CreatedBy string    `gorm:"not null;default:''" json:"created_by"`
	UpdatedBy string    `gorm:"not null;default:''" json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// QueuedBy and QueuedAt say who sent it and when; SentAt is when the last mail went.
	QueuedBy string     `gorm:"not null;default:''" json:"queued_by"`
	QueuedAt *time.Time `json:"queued_at"`
	SentAt   *time.Time `json:"sent_at"`
	// Paused says why sending stopped by itself; Failures counts sends in a row that
	// didn't reach the mail server.
	Paused   string `gorm:"not null;default:''" json:"paused"`
	Failures int    `gorm:"not null;default:0" json:"-"`
}

const AudienceCustomers = "customers"

const KindService = "service"

// Fits is whether a campaign's kind and audience go together: a service notice is for
// current customers only, or it is marketing by another name.
func (c Campaign) Fits() error {
	if c.Kind == KindService && c.Audience != AudienceCustomers {
		return say.New("a service notice goes to current customers only: one to people in conversation is marketing")
	}
	return nil
}

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
