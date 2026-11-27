package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
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

const DealNew = "new"

var DealKinds = []struct{ Key, Label string }{
	{DealNew, "New business"}, {"renewal", "Renewal"}, {"expansion", "Expansion"},
}

const defaultCurrency = "GBP"

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

var moneyRe = regexp.MustCompile(`^\d{0,12}(\.\d{0,2})?$`)

var leadingCode = regexp.MustCompile(`^[A-Za-z]{3}`)

// parseMoney reads "14,000", "£14000.5", "14 000.50" or "CHF 14,000" into hundredths:
// whatever formatMoney writes, since the card shows a value in the box it is edited in.
func parseMoney(text string) (int64, error) {
	s := strings.NewReplacer(",", "", " ", "", "£", "", "$", "", "€", "").Replace(text)
	s = leadingCode.ReplaceAllString(s, "")
	if !moneyRe.MatchString(s) || s == "." {
		return 0, fmt.Errorf("%q isn't an amount", text)
	}
	whole, frac, _ := strings.Cut(s, ".")
	n, err := strconv.ParseInt("0"+whole+(frac + "00")[:2], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q isn't an amount", text)
	}
	return n, nil
}

var currencySigns = map[string]string{"GBP": "£", "EUR": "€", "USD": "$"}

func formatMoney(amount int64, currency string) string {
	whole := strconv.FormatInt(amount/100, 10)
	for i := len(whole) - 3; i > 0; i -= 3 {
		whole = whole[:i] + "," + whole[i:]
	}
	if cents := amount % 100; cents != 0 {
		whole += fmt.Sprintf(".%02d", cents)
	}
	if sign, ok := currencySigns[currency]; ok {
		return sign + whole
	}
	return strings.TrimSpace(currency + " " + whole)
}

func (d Deal) Value() string {
	if d.Amount == 0 {
		return ""
	}
	return formatMoney(d.Amount, d.Currency)
}

func kindOfDeal(kind string) bool {
	for _, k := range DealKinds {
		if k.Key == kind {
			return true
		}
	}
	return false
}

// dealChange is what setting one of a deal's fields writes, and what the history says
// of it. Free text about the deal stays out of the history, as a contact's does.
func dealChange(tx *gorm.DB, org int64, d Deal, currency, field, value string) (map[string]any, string, error) {
	switch field {
	case "amount":
		amount, err := parseMoney(value)
		if err != nil {
			return nil, "", err
		}
		logged := ""
		if amount != 0 {
			logged = formatMoney(amount, currency)
		}
		return map[string]any{"amount": amount, "currency": currency}, logged, nil
	case "close_date", "next_date":
		if field == "next_date" && d.Seq == 0 {
			break
		}
		if value != "" {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return nil, "", fmt.Errorf("%q isn't a date", value)
			}
		}
		return map[string]any{field: nonEmpty(value)}, value, nil
	case "kind":
		if !kindOfDeal(value) {
			return nil, "", fmt.Errorf("unknown kind of deal %q", value)
		}
		return map[string]any{"kind": value}, value, nil
	case "owner":
		if value != "" && value != d.Owner {
			var n int64
			if err := tx.Table("memberships").Joins("join users on users.id = memberships.user_id").
				Where("memberships.org_id = ? and users.name = ?", org, value).Count(&n).Error; err != nil {
				return nil, "", err
			}
			if n == 0 {
				return nil, "", fmt.Errorf("%q isn't a member of this organisation", value)
			}
		}
		return map[string]any{"owner": value}, value, nil
	}
	if d.Seq > 0 {
		switch field {
		case "stage":
			if !validStage(value) {
				return nil, "", fmt.Errorf("unknown stage %q", value)
			}
			return map[string]any{"stage": value}, value, nil
		case "next_step", "lost_reason":
			return map[string]any{field: value}, "", nil
		}
	}
	return nil, "", fmt.Errorf("a deal has no %q", field)
}

// OtherDeal is a deal after the conversation's own, with the person it is with.
type OtherDeal struct {
	Deal
	With Contact
}

func (d Deal) needsNextStep(day string) bool {
	if d.Stage == "won" || d.Stage == "lost" {
		return false
	}
	return d.NextStep == "" || (d.NextDate != nil && *d.NextDate <= day)
}

// Change is one field given a new value.
type Change struct{ Field, Value string }
