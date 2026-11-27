package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

const DealNew = "new"

var DealKinds = []struct{ Key, Label string }{
	{DealNew, "New business"}, {"renewal", "Renewal"}, {"expansion", "Expansion"},
}

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

// Change is one field given a new value.
type Change struct{ Field, Value string }
