package store

import (
	"time"

	"github.com/shutthegoatup/prospector/internal/say"
)

// Subscription is what a customer is on: what it is, what it costs, when it started
// and what was agreed about its term. Where it stands today is worked out from those
// (see On), never stored, so nothing has to run for a renewal to come round.
//
// It belongs to a company, not a person: erasing someone leaves it, and it holds
// nothing that identifies them.
type Subscription struct {
	ID        int64  `gorm:"primaryKey" json:"id"`
	OrgID     int64  `gorm:"index:idx_subscriptions_company,priority:1;not null" json:"-"`
	CompanyID int64  `gorm:"index:idx_subscriptions_company,priority:2;not null" json:"company_id"`
	Name      string `gorm:"not null;default:''" json:"name"`
	// Amount is the price for one Per, in the currency's hundredths.
	Amount   int64  `gorm:"not null;default:0" json:"amount"`
	Currency string `gorm:"not null;default:''" json:"currency"`
	Per      string `gorm:"not null;default:'year'" json:"per"`
	Start    string `gorm:"not null;default:''" json:"start"`
	// TermMonths is how long they committed for; 0 is no commitment, a price period at a time.
	TermMonths int `gorm:"not null;default:0" json:"term_months"`
	// Then is what happens when the term is up.
	Then       string `gorm:"not null;default:'renews'" json:"then"`
	NoticeDays int    `gorm:"not null;default:0" json:"notice_days"`
	// NoticeOn is the day either side gave notice to end it.
	NoticeOn *string `json:"notice_on"`
	// EndedOn is the day it stopped, when that wasn't simply the term running out.
	EndedOn   *string   `json:"ended_on"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const PerMonth, PerYear = "month", "year"

const ThenRolls = "rolls"

const ThenEnds = "ends"

const SubScheduled = "scheduled"

const SubActive = "active"

const SubNotice = "notice given"

const SubEnding = "ending"

const SubEnded = "ended"

func ParseDay(text string) (time.Time, error) {
	d, err := time.Parse(time.DateOnly, text)
	// A year typed short ("0026") would otherwise be terms counted for two thousand years.
	if err != nil || d.Year() < 1990 || d.Year() > 2200 {
		return d, say.Errorf("%q isn't a date", text)
	}
	return d, nil
}

// AddMonths is the same day of the month n months on, or that month's last day when
// it has no such day: a term from 31 January runs to the end of February, not into March.
func AddMonths(from time.Time, n int) time.Time {
	first := time.Date(from.Year(), from.Month()+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(from.Day(), last)-1)
}

func (s Subscription) Period() int {
	if s.Per == PerMonth {
		return 1
	}
	return 12
}

// Yearly is the price for a year.
func (s Subscription) Yearly() int64 {
	return s.Amount * int64(12/s.Period())
}

// Figure is the price as it is typed, without what it is for.
func (s Subscription) Figure() string {
	if s.Amount == 0 {
		return ""
	}
	return FormatMoney(s.Amount, s.Currency)
}

func (s Subscription) Price() string {
	if s.Amount == 0 {
		return ""
	}
	return FormatMoney(s.Amount, s.Currency) + " a " + s.Per
}

// Standing is where a subscription stands on a day.
type Standing struct {
	Status string
	// TermEnd is the last day of the term that day falls in, or of the first term before it starts.
	TermEnd string
	// CancelBy is the next day still to come by which notice can be given, and Out the
	// last day of the term that notice would end it at. Both are empty when it ends
	// anyway, or notice has been given.
	CancelBy, Out string
	// Ends is the last day, once that is settled: notice given, an end recorded, or a
	// term that doesn't renew.
	Ends string
}

// Missed is when the chance to stop at the end of this term has gone: notice now
// would only end a later one.
func (st Standing) Missed() bool {
	return st.Out != "" && st.Out != st.TermEnd
}

// Terms calls each for every term in turn, as its first day and its last, until each
// returns true. The first term is the commitment (or one price period when there is
// none); what follows repeats it, rolls a period at a time, or doesn't come.
func (s Subscription) Terms(start time.Time, each func(first, last time.Time) bool) {
	length, step := s.TermMonths, s.TermMonths
	if length <= 0 {
		length, step = s.Period(), s.Period()
	}
	if s.Then == ThenRolls {
		step = s.Period()
	}
	from := start
	// Bounded so a date centuries off can't spin: 2,400 terms is 200 years of months.
	for i := range 2400 {
		// Every boundary is counted from the start, so a 31st doesn't drift to the 28th for good.
		next := AddMonths(start, length+step*i)
		if each(from, next.AddDate(0, 0, -1)) || (s.Then == ThenEnds && s.TermMonths > 0) {
			return
		}
		from = next
	}
}

// On works out where the subscription stands on day (a date as 2006-01-02).
func (s Subscription) On(day string) Standing {
	start, err := ParseDay(s.Start)
	if err != nil {
		return Standing{Status: SubScheduled}
	}
	text := func(d time.Time) string { return d.Format(time.DateOnly) }
	// Notice is counted back from the day the next term would start; with none to give,
	// the last day of the term is still in time.
	cancelBy := func(last time.Time) string { return text(last.AddDate(0, 0, 1-max(s.NoticeDays, 1))) }
	ends := s.Then == ThenEnds && s.TermMonths > 0

	var st Standing
	// The term day falls in, or the last there is.
	s.Terms(start, func(first, last time.Time) bool {
		st.TermEnd = text(last)
		return day <= st.TermEnd
	})
	switch {
	case s.NoticeOn != nil:
		// Notice ends it at the close of the first term it was in time for.
		s.Terms(start, func(first, last time.Time) bool {
			st.Ends = text(last)
			return *s.NoticeOn <= cancelBy(last)
		})
	case ends:
		st.Ends = st.TermEnd
	default:
		// The first term, from the one day is in, that notice could still end. With notice
		// longer than a term that is never the current one.
		s.Terms(start, func(first, last time.Time) bool {
			st.CancelBy, st.Out = cancelBy(last), text(last)
			return day <= st.Out && day <= st.CancelBy
		})
	}
	if s.EndedOn != nil && (st.Ends == "" || *s.EndedOn < st.Ends) {
		st.Ends, st.CancelBy, st.Out = *s.EndedOn, "", ""
	}
	switch {
	case day < s.Start:
		st.Status = SubScheduled
	case st.Ends != "" && st.Ends < day:
		st.Status, st.CancelBy, st.Out = SubEnded, "", ""
	case s.NoticeOn != nil:
		st.Status = SubNotice
	case s.EndedOn != nil:
		st.Status = SubEnding
	default:
		st.Status = SubActive
	}
	return st
}

func MustDay(text string) time.Time {
	d, _ := time.Parse(time.DateOnly, text)
	return d
}

// Attention is the next day someone has to know about: the sooner of the day to cancel
// by and the day the term ends, or the day it stops once that is settled. Ended ones
// sort last.
func (st Standing) Attention() string {
	switch {
	case st.Status == SubEnded:
		return "9999"
	case st.Ends != "":
		return st.Ends
	case st.CancelBy != "" && st.CancelBy < st.TermEnd:
		return st.CancelBy
	}
	return st.TermEnd
}

// SoonDays is how far ahead a date counts as coming up: long enough to talk to the
// customer before a day to cancel by, short enough that the list means something.
const SoonDays = 30

// Within is a standing whose next date to know about falls within soonDays of day.
func (st Standing) Within(day string) bool {
	at := st.Attention()
	return st.Status != SubEnded && at >= day && at <= MustDay(day).AddDate(0, 0, SoonDays).Format(time.DateOnly)
}

// MonthsNow is how long the term day falls in runs: the commitment, then whatever follows it.
func (s Subscription) MonthsNow(day string) int {
	start, err := ParseDay(s.Start)
	if err != nil || s.TermMonths <= 0 {
		return s.Period()
	}
	if s.Then == ThenRolls && day >= AddMonths(start, s.TermMonths).Format(time.DateOnly) {
		return s.Period()
	}
	return s.TermMonths
}
