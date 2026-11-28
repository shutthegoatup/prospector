package sales

import (
	"slices"
	"strings"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

const (
	ThenRenews = "renews"
)

var SubscriptionThens = []struct{ Key, Label string }{
	{ThenRenews, "renews for the same term"}, {store.ThenRolls, "rolls on, a period at a time"}, {store.ThenEnds, "ends"},
}

// Soon is a subscription with something coming up worth looking at. One that turns
// over every month always has a term ending within the month, which is routine, so it
// counts only once it has a day it stops.
func (r SubscriptionRow) Soon(day string) bool {
	if r.Ends == "" && r.MonthsNow(day) <= 1 {
		return false
	}
	return r.Within(day)
}

// SubscriptionIn is what someone types to record a subscription.
type SubscriptionIn struct {
	Company    int64  `json:"company"`
	Name       string `json:"name"`
	Amount     string `json:"amount"`
	Per        string `json:"per"`
	Start      string `json:"start"`
	TermMonths int    `json:"term_months"`
	Then       string `json:"then"`
	NoticeDays int    `json:"notice_days"`
}

func validThen(then string) bool {
	for _, t := range SubscriptionThens {
		if t.Key == then {
			return true
		}
	}
	return false
}

func checkTerm(months, notice int) error {
	if months < 0 || months > 120 {
		return say.New("a term is between 0 and 120 months")
	}
	if notice < 0 || notice > 730 {
		return say.New("notice is between 0 and 730 days")
	}
	return nil
}

// SubscriptionRow is a subscription with its company's name and where it stands.
type SubscriptionRow struct {
	store.Subscription
	Company string
	store.Standing
	// Renewing is whether a renewal deal for it is open on the Conversations board.
	Renewing bool
	// People are those at the company already in conversation: who a renewal can be with.
	People []store.Contact
}

// Contracted is what the subscriptions still running add up to for a year, a sum for
// each currency: pounds and euros don't add.
func Contracted(rows []SubscriptionRow) string {
	sums := map[string]int64{}
	for _, r := range rows {
		if (r.Status == store.SubActive || r.Status == store.SubNotice || r.Status == store.SubEnding) && r.Amount != 0 {
			sums[r.Currency] += r.Yearly()
		}
	}
	var parts []string
	for currency, sum := range sums {
		parts = append(parts, store.FormatMoney(sum, currency))
	}
	slices.Sort(parts)
	return strings.Join(parts, " + ")
}
