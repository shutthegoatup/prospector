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
