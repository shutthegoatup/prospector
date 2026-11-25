package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Deal is what a conversation is worth, when it should close and whose it is. Where
// it stands (stage, next step, why it was lost) is still the contact's.
type Deal struct {
	ID        int64  `gorm:"primaryKey" json:"id"`
	OrgID     int64  `gorm:"uniqueIndex:idx_deal_contact;not null" json:"-"`
	ContactID int64  `gorm:"uniqueIndex:idx_deal_contact;not null" json:"contact_id"`
	Kind      string `gorm:"not null;default:'new'" json:"kind"`
	// Amount is in the currency's hundredths, so sums don't drift.
	Amount    int64     `gorm:"not null;default:0" json:"amount"`
	Currency  string    `gorm:"not null;default:''" json:"currency"`
	CloseDate *string   `json:"close_date"`
	Owner     string    `gorm:"not null;default:''" json:"owner"`
	UpdatedAt time.Time `json:"updated_at"`
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

// Deals are an organisation's deals by the contact each belongs to.
func (s *Store) Deals(org int64) (map[int64]Deal, error) {
	var rows []Deal
	if err := s.in(org).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]Deal, len(rows))
	for _, d := range rows {
		out[d.ContactID] = d
	}
	return out, nil
}

func (s *Store) setDeal(tx *gorm.DB, org, contact int64, field, value, actor string) error {
	value = strings.TrimSpace(value)
	var c Contact
	if err := tx.Where("org_id = ? and id = ?", org, contact).First(&c).Error; err != nil {
		return missing(err, "no contact %d", contact)
	}
	if c.Replied == nil {
		return errors.New("a deal is for someone who has replied; mark the reply first")
	}
	var o Org
	if err := tx.First(&o, org).Error; err != nil {
		return err
	}
	var d Deal
	if err := tx.Where("org_id = ? and contact_id = ?", org, contact).Limit(1).Find(&d).Error; err != nil {
		return err
	}
	// A deal already valued keeps its currency: the setting changing mustn't turn
	// pounds into euros the next time someone corrects the figure.
	currency := o.Settings.withDefaults().Currency
	if d.Amount != 0 && d.Currency != "" {
		currency = d.Currency
	}
	updates := map[string]any{}
	logged := value
	switch field {
	case "amount":
		amount, err := parseMoney(value)
		if err != nil {
			return err
		}
		updates["amount"], updates["currency"] = amount, currency
		logged = ""
		if amount != 0 {
			logged = formatMoney(amount, currency)
		}
	case "close_date":
		if value != "" {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return fmt.Errorf("%q isn't a date", value)
			}
		}
		updates["close_date"] = nonEmpty(value)
	case "kind":
		known := false
		for _, k := range DealKinds {
			known = known || k.Key == value
		}
		if !known {
			return fmt.Errorf("unknown kind of deal %q", value)
		}
		updates["kind"] = value
	case "owner":
		if value != "" && value != d.Owner {
			var n int64
			if err := tx.Table("memberships").Joins("join users on users.id = memberships.user_id").
				Where("memberships.org_id = ? and users.name = ?", org, value).Count(&n).Error; err != nil {
				return err
			}
			if n == 0 {
				return fmt.Errorf("%q isn't a member of this organisation", value)
			}
		}
		updates["owner"] = value
	default:
		return fmt.Errorf("a deal has no %q", field)
	}
	if d.ID == 0 {
		// Two fields changed at once are two requests; whichever comes second finds the row.
		first := Deal{OrgID: org, ContactID: contact, Kind: DealNew, Currency: currency}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&first).Error; err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and contact_id = ?", org, contact).First(&d).Error; err != nil {
			return err
		}
	}
	if err := tx.Model(&d).Updates(updates).Error; err != nil {
		return err
	}
	return s.log(tx, org, contact, c.CompanyID, actor, "deal:"+field, nonEmpty(logged))
}
