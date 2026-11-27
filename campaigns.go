package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
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

const (
	AudienceConversations = "conversations"
	AudienceCustomers     = "customers"
	KindMarketing         = "marketing"
	KindService           = "service"
	CampaignDraft         = "draft"
	CampaignSending       = "sending"
	CampaignPaused        = "paused"
	CampaignSent          = "sent"
	CampaignCancelled     = "cancelled"

	campaignSubjectLimit = 200
	campaignBodyLimit    = 20000
)

var CampaignAudiences = []struct{ Key, Label string }{
	{AudienceConversations, "Everyone in conversation"},
	{AudienceCustomers, "Everyone at a customer"},
}

func audienceLabel(key string) string {
	for _, a := range CampaignAudiences {
		if a.Key == key {
			return a.Label
		}
	}
	return ""
}

type CampaignIn struct {
	Name     string `json:"name"`
	Audience string `json:"audience"`
	Kind     string `json:"kind"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
}

// campaignField checks one field as a draft holds it.
func campaignField(field, value string) (string, error) {
	switch field {
	case "name":
		value = strings.TrimSpace(value)
		if value == "" {
			return "", errors.New("a campaign needs a name")
		}
		if utf8.RuneCountInString(value) > 120 {
			return "", errors.New("a campaign's name is 120 characters at most")
		}
	case "audience":
		if audienceLabel(value) == "" {
			return "", fmt.Errorf("a campaign goes to %q or %q", AudienceConversations, AudienceCustomers)
		}
	case "kind":
		if value != KindMarketing && value != KindService {
			return "", fmt.Errorf("a campaign is %q or %q", KindMarketing, KindService)
		}
	case "subject":
		// One line: a subject is a header.
		value = strings.Join(strings.Fields(value), " ")
		if utf8.RuneCountInString(value) > campaignSubjectLimit {
			return "", fmt.Errorf("a subject is %d characters at most", campaignSubjectLimit)
		}
	case "body":
		// Only trailing blank lines go: a line that ends in a space, as "-- " does, keeps it.
		value = strings.TrimRight(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
		if utf8.RuneCountInString(value) > campaignBodyLimit {
			return "", fmt.Errorf("a campaign is %d characters at most", campaignBodyLimit)
		}
	default:
		return "", fmt.Errorf("a campaign has no %q to set", field)
	}
	return value, nil
}

// Recipient is one person a campaign is for, and why they are left out when they are.
type Recipient struct {
	ContactID int64  `json:"contact_id"`
	Name      string `json:"name"`
	Company   string `json:"company"`
	Address   string `json:"address"`
	// LeftOut is empty for someone it goes to, and otherwise says why it doesn't.
	LeftOut string `json:"left_out"`
}

// audienceWork is what every campaign's audience is checked against, read once for a
// page of them.
type audienceWork struct {
	gate      *mailGate
	customers []int64
	noMail    map[string]bool
}

// leftOutBecause says why someone is left out, for the reasons there are. Any other
// error is a failure to check, and is not passed off as a reason.
func leftOutBecause(err error) (string, bool) {
	switch {
	case errors.Is(err, errSuppressed):
		return "on the list not to be mailed", true
	case errors.Is(err, errBounced):
		return "mail to them bounced", true
	case errors.Is(err, errComplained):
		return "marked a mail as spam", true
	case errors.Is(err, errErased):
		return "erased on request", true
	case errors.Is(err, errNeverContact):
		return "on the never-contact list", true
	}
	return "", false
}

// fits is whether a campaign's kind and audience go together: a service notice is for
// current customers only, or it is marketing by another name.
func (c Campaign) fits() error {
	if c.Kind == KindService && c.Audience != AudienceCustomers {
		return errors.New("a service notice goes to current customers only: one to people in conversation is marketing")
	}
	return nil
}
