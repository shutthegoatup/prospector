package tz

import (
	"testing"
	"time"
)

// sameClock compares zones by the time they show in winter and summer: libphonenumber
// names some countries by a neighbour on the same rules (Estonia as Europe/Bucharest).
func sameClock(t *testing.T, a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	la, err := time.LoadLocation(a)
	if err != nil {
		t.Fatal(err)
	}
	lb, err := time.LoadLocation(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []time.Time{time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)} {
		_, oa := d.In(la).Zone()
		_, ob := d.In(lb).Zone()
		if oa != ob {
			return false
		}
	}
	return true
}

func TestGuess(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ phone, maker, want string }{
		{"+44 20 7946 0000", "", "Europe/London"},
		{"+1 832 555 0100 / +1 855-555-0100", "Prairie Motors (US)", "America/Chicago"},
		{"+1 250-555-0100", "Northwest Boats (US)", "America/Vancouver"},
		{"+1 833 555 0100", "Tollfree Carts (US)", "America/New_York"},
		{"+372 5000 0000", "Baltic Cabins (EE)", "Europe/Tallinn"},
		{"+39 06 0000 0000", "", "Europe/Rome"},
		{"", "Example Guitars (UK)", "Europe/London"},
		{"", "Nordic Saunas (FI/PL)", "Europe/Helsinki"},
		{"", "Nobody", ""},
	} {
		got := ""
		if zone := Guess(c.phone, c.maker); zone != nil {
			got = *zone
		}
		if !sameClock(t, got, c.want) {
			t.Errorf("Guess(%q, %q) = %q, want %q", c.phone, c.maker, got, c.want)
		}
	}
}
