package sales

import (
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

const DealNew = "new"

var DealKinds = []struct{ Key, Label string }{
	{DealNew, "New business"}, {"renewal", "Renewal"}, {"expansion", "Expansion"},
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
func dealChange(tx *gorm.DB, org int64, d store.Deal, currency, field, value string) (map[string]any, string, error) {
	switch field {
	case "amount":
		amount, err := store.ParseMoney(value)
		if err != nil {
			return nil, "", err
		}
		logged := ""
		if amount != 0 {
			logged = store.FormatMoney(amount, currency)
		}
		return map[string]any{"amount": amount, "currency": currency}, logged, nil
	case "close_date", "next_date":
		if field == "next_date" && d.Seq == 0 {
			break
		}
		if value != "" {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return nil, "", say.Errorf("%q isn't a date", value)
			}
		}
		return map[string]any{field: store.NonEmpty(value)}, value, nil
	case "kind":
		if !kindOfDeal(value) {
			return nil, "", say.Errorf("unknown kind of deal %q", value)
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
				return nil, "", say.Errorf("%q isn't a member of this organisation", value)
			}
		}
		return map[string]any{"owner": value}, value, nil
	}
	if d.Seq > 0 {
		switch field {
		case "stage":
			if !store.ValidStage(value) {
				return nil, "", say.Errorf("unknown stage %q", value)
			}
			return map[string]any{"stage": value}, value, nil
		case "next_step", "lost_reason":
			return map[string]any{field: value}, "", nil
		}
	}
	return nil, "", say.Errorf("a deal has no %q", field)
}

// OtherDeal is a deal after the conversation's own, with the person it is with.
type OtherDeal struct {
	store.Deal
	With store.Contact
}
