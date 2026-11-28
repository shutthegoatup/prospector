package store

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/say"
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

var moneyRe = regexp.MustCompile(`^\d{0,12}(\.\d{0,2})?$`)

var leadingCode = regexp.MustCompile(`^[A-Za-z]{3}`)

// ParseMoney reads "14,000", "£14000.5", "14 000.50" or "CHF 14,000" into hundredths:
// whatever formatMoney writes, since the card shows a value in the box it is edited in.
func ParseMoney(text string) (int64, error) {
	s := strings.NewReplacer(",", "", " ", "", "£", "", "$", "", "€", "").Replace(text)
	s = leadingCode.ReplaceAllString(s, "")
	if !moneyRe.MatchString(s) || s == "." {
		return 0, say.Errorf("%q isn't an amount", text)
	}
	whole, frac, _ := strings.Cut(s, ".")
	n, err := strconv.ParseInt("0"+whole+(frac + "00")[:2], 10, 64)
	if err != nil {
		return 0, say.Errorf("%q isn't an amount", text)
	}
	return n, nil
}

// Change is one field given a new value.
type Change struct{ Field, Value string }

// Changes are what a request sent to change, in the order they are made.
type Changes []Change

// Set adds field's change when the request sent a value for it.
func (c Changes) Set(field string, value *string) Changes {
	if value == nil {
		return c
	}
	return append(c, Change{Field: field, Value: *value})
}

// Apply makes each change through apply in one transaction, or none: one that is
// refused takes back those before it.
func (c *Core) Apply(changes []Change, apply func(tx *gorm.DB, ch Change) error) error {
	return c.DB.Transaction(func(tx *gorm.DB) error {
		for _, ch := range changes {
			if err := apply(tx, ch); err != nil {
				return err
			}
		}
		return nil
	})
}
