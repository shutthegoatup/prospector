package app

import (
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/internal/store"
)

// TestADayTypedWithAShortYearIsRefused: "0026-03-01" is two thousand years ago, not a
// typo to keep, wherever a day is written, as a subscription's start already was.
func TestADayTypedWithAShortYearIsRefused(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	c := repliedPerson(t, s, o, "Hannah Pryce")
	deal := must(s.StartDeal(o.ID, c.ID, "renewal", "Ada Lovelace"))
	if err := s.ChangeDeal(o.ID, deal.ID, "close_date", "0026-03-01", "Ada Lovelace"); err == nil {
		t.Error("a deal took a close date two thousand years ago")
	}
	if err := s.ChangeDeal(o.ID, deal.ID, "close_date", "2026-03-01", "Ada Lovelace"); err != nil {
		t.Errorf("a deal's close date: %v", err)
	}
	if err := s.ChangeContactFields(o.ID, c.ID, []store.Change{{Field: "next_date", Value: "0026-03-01"}}, "Ada Lovelace"); err == nil {
		t.Error("a person took a next date two thousand years ago")
	}
	if err := s.Mark(o.ID, c.ID, "replied", "0026-03-01", "Ada Lovelace"); err == nil {
		t.Error("a mark took a day two thousand years ago")
	}
	if err := s.Mark(o.ID, c.ID, "replied", time.Now().Format(time.DateOnly), "Ada Lovelace"); err != nil {
		t.Errorf("a mark today: %v", err)
	}
}
