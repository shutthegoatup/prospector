package store

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Deal is what a conversation is worth, when it should close and whose it is. Where
// it stands (stage, next step, why it was lost) is still the contact's.
type Deal struct {
	ID        int64 `gorm:"primaryKey" json:"id"`
	OrgID     int64 `gorm:"uniqueIndex:idx_deal_seq,priority:1;not null" json:"-"`
	ContactID int64 `gorm:"uniqueIndex:idx_deal_seq,priority:2;not null" json:"contact_id"`
	// Seq tells one contact's deals apart; 0 is the conversation's own. A database from
	// before it also has an index allowing each contact one deal, which nothing here
	// relies on.
	Seq  int    `gorm:"uniqueIndex:idx_deal_seq,priority:3;not null;default:0" json:"seq"`
	Kind string `gorm:"not null;default:'new'" json:"kind"`
	// Amount is in the currency's hundredths, so sums don't drift.
	Amount    int64     `gorm:"not null;default:0" json:"amount"`
	Currency  string    `gorm:"not null;default:''" json:"currency"`
	CloseDate *string   `json:"close_date"`
	Owner     string    `gorm:"not null;default:''" json:"owner"`
	UpdatedAt time.Time `json:"updated_at"`
	// A deal after the conversation's own says where it stands itself.
	Stage      string  `gorm:"not null;default:''" json:"stage"`
	NextStep   string  `gorm:"not null;default:''" json:"next_step"`
	NextDate   *string `json:"next_date"`
	LostReason string  `gorm:"not null;default:''" json:"lost_reason"`
	// SubscriptionID is the subscription a renewal is for; 0 when it is for none.
	SubscriptionID int64 `gorm:"index;not null;default:0" json:"subscription_id"`
}

const DefaultCurrency = "GBP"

var CurrencySigns = map[string]string{"GBP": "£", "EUR": "€", "USD": "$"}

func FormatMoney(amount int64, currency string) string {
	whole := strconv.FormatInt(amount/100, 10)
	for i := len(whole) - 3; i > 0; i -= 3 {
		whole = whole[:i] + "," + whole[i:]
	}
	if cents := amount % 100; cents != 0 {
		whole += fmt.Sprintf(".%02d", cents)
	}
	if sign, ok := CurrencySigns[currency]; ok {
		return sign + whole
	}
	return strings.TrimSpace(currency + " " + whole)
}

func (d Deal) Value() string {
	if d.Amount == 0 {
		return ""
	}
	return FormatMoney(d.Amount, d.Currency)
}

func (d Deal) NeedsNextStep(day string) bool {
	if d.Stage == "won" || d.Stage == "lost" {
		return false
	}
	return d.NextStep == "" || (d.NextDate != nil && *d.NextDate <= day)
}
