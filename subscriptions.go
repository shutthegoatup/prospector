package main

import (
	"sort"
	"strings"
)

// contracted is what the subscriptions still running add up to for a year, a sum for
// each currency: pounds and euros don't add.
func contracted(rows []SubscriptionRow) string {
	sums := map[string]int64{}
	for _, r := range rows {
		if (r.Status == SubActive || r.Status == SubNotice || r.Status == SubEnding) && r.Amount != 0 {
			sums[r.Currency] += r.Yearly()
		}
	}
	var parts []string
	for currency, sum := range sums {
		parts = append(parts, formatMoney(sum, currency))
	}
	sort.Strings(parts)
	return strings.Join(parts, " + ")
}
