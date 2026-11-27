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

// Deals are an organisation's deals by the contact each belongs to.
func (s *Store) Deals(org int64) (map[int64]Deal, error) {
	var rows []Deal
	if err := s.in(org).Where("seq = 0").Find(&rows).Error; err != nil {
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
	if err := tx.Where("org_id = ? and contact_id = ? and seq = 0", org, contact).Limit(1).Find(&d).Error; err != nil {
		return err
	}
	// A deal already valued keeps its currency: the setting changing mustn't turn
	// pounds into euros the next time someone corrects the figure.
	currency := o.Settings.withDefaults().Currency
	if d.Amount != 0 && d.Currency != "" {
		currency = d.Currency
	}
	updates, logged, err := dealChange(tx, org, d, currency, field, value)
	if err != nil {
		return err
	}
	if d.ID == 0 {
		// Two fields changed at once are two requests; whichever comes second finds the row.
		first := Deal{OrgID: org, ContactID: contact, Kind: DealNew, Currency: currency}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&first).Error; err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and contact_id = ? and seq = 0", org, contact).First(&d).Error; err != nil {
			return err
		}
	}
	if err := tx.Model(&d).Updates(updates).Error; err != nil {
		return err
	}
	return s.log(tx, org, contact, c.CompanyID, actor, "deal:"+field, nonEmpty(logged))
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

// OtherDeals are the renewals, expansions and further deals, by the stage each stands
// at. One stays on the board when its person later declines, unlike their conversation:
// it has a value and an outcome of its own, and somebody has to close it.
func (s *Store) OtherDeals(org int64) (map[string][]OtherDeal, error) {
	var deals []Deal
	if err := s.in(org).Where("seq > 0").Order("next_date nulls first, id").Find(&deals).Error; err != nil {
		return nil, err
	}
	if len(deals) == 0 {
		return map[string][]OtherDeal{}, nil
	}
	var people []Contact
	if err := s.in(org).Where("id in (?)", s.in(org).Model(&Deal{}).Select("contact_id").Where("seq > 0")).Find(&people).Error; err != nil {
		return nil, err
	}
	with := make(map[int64]Contact, len(people))
	for _, c := range people {
		with[c.ID] = c
	}
	out := map[string][]OtherDeal{}
	for _, d := range deals {
		if c, ok := with[d.ContactID]; ok {
			out[d.Stage] = append(out[d.Stage], OtherDeal{d, c})
		}
	}
	return out, nil
}

// OwnDeal is the deal a conversation is, empty if it has none yet.
func (s *Store) OwnDeal(org, contact int64) (*Deal, error) {
	var d Deal
	return &d, s.in(org).Where("contact_id = ? and seq = 0", contact).Limit(1).Find(&d).Error
}

// StartDeal opens another deal with someone already in conversation: a renewal, an
// expansion or a second piece of new business. It starts at the first stage.
func (s *Store) StartDeal(org, contact int64, kind, actor string) (*Deal, error) {
	if !kindOfDeal(kind) {
		return nil, fmt.Errorf("unknown kind of deal %q", kind)
	}
	var d Deal
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// Held, so two started together are numbered one after the other.
		if err := hold(tx, &Contact{}, org, contact); err != nil {
			return err
		}
		var c Contact
		if err := tx.Where("org_id = ? and id = ?", org, contact).First(&c).Error; err != nil {
			return missing(err, "no contact %d", contact)
		}
		if c.Replied == nil || c.Declined != nil || c.Status == ContactSuggested {
			return errors.New("another deal is for someone you are in conversation with")
		}
		var o Org
		if err := tx.First(&o, org).Error; err != nil {
			return err
		}
		var last int
		if err := tx.Model(&Deal{}).Where("org_id = ? and contact_id = ?", org, contact).
			Select("coalesce(max(seq), 0)").Scan(&last).Error; err != nil {
			return err
		}
		d = Deal{OrgID: org, ContactID: contact, Seq: last + 1, Kind: kind, Stage: Stages[0].Key, Currency: o.Settings.withDefaults().Currency}
		if err := tx.Create(&d).Error; err != nil {
			return err
		}
		return s.log(tx, org, contact, c.CompanyID, actor, "deal:start", &kind)
	})
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) otherDeal(tx *gorm.DB, org, id int64) (Deal, Contact, error) {
	var d Deal
	if err := tx.Where("org_id = ? and id = ? and seq > 0", org, id).First(&d).Error; err != nil {
		return d, Contact{}, missing(err, "no deal %d", id)
	}
	var c Contact
	if err := tx.Where("org_id = ? and id = ?", org, d.ContactID).First(&c).Error; err != nil {
		return d, c, missing(err, "no contact %d", d.ContactID)
	}
	return d, c, nil
}

// ChangeDeal sets one field of a deal after the conversation's own. Those hold their
// stage and next step themselves; the conversation's own are still its contact's.
func (s *Store) ChangeDeal(org, id int64, field, value, actor string) error {
	return s.ChangeDealFields(org, id, []Change{{field, value}}, actor)
}

// Change is one field given a new value.
type Change struct{ Field, Value string }

// ChangeDealFields makes several changes to a deal after the conversation's own, or
// none: one that is refused takes back those before it.
func (s *Store) ChangeDealFields(org, id int64, changes []Change, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, c := range changes {
			if err := s.changeDeal(tx, org, id, c.Field, strings.TrimSpace(c.Value), actor); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetDealFields does the same for a conversation's own deal, which is its contact's.
func (s *Store) SetDealFields(org, contact int64, changes []Change, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, c := range changes {
			if err := s.setDeal(tx, org, contact, c.Field, c.Value, actor); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) changeDeal(tx *gorm.DB, org, id int64, field, value, actor string) error {
	{
		d, c, err := s.otherDeal(tx, org, id)
		if err != nil {
			return err
		}
		currency := d.Currency
		if d.Amount == 0 || currency == "" {
			var o Org
			if err := tx.First(&o, org).Error; err != nil {
				return err
			}
			currency = o.Settings.withDefaults().Currency
		}
		updates, logged, err := dealChange(tx, org, d, currency, field, value)
		if err != nil {
			return err
		}
		if err := tx.Model(&d).Updates(updates).Error; err != nil {
			return err
		}
		return s.log(tx, org, c.ID, c.CompanyID, actor, "deal:"+field, nonEmpty(logged))
	}
}

// RemoveDeal takes back a deal that was started by mistake. One that ran its course is
// marked won or lost instead, and the conversation's own can't be removed.
func (s *Store) RemoveDeal(org, id int64, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		d, c, err := s.otherDeal(tx, org, id)
		if err != nil {
			return err
		}
		if err := tx.Delete(&d).Error; err != nil {
			return err
		}
		return s.log(tx, org, c.ID, c.CompanyID, actor, "deal:remove", &d.Kind)
	})
}
