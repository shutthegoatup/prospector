package main

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
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

const (
	PerMonth, PerYear = "month", "year"

	ThenRenews = "renews"
	ThenRolls  = "rolls"
	ThenEnds   = "ends"

	SubScheduled = "scheduled"
	SubActive    = "active"
	SubNotice    = "notice given"
	SubEnding    = "ending"
	SubEnded     = "ended"
)

var SubscriptionThens = []struct{ Key, Label string }{
	{ThenRenews, "renews for the same term"}, {ThenRolls, "rolls on, a period at a time"}, {ThenEnds, "ends"},
}

const dayLayout = "2006-01-02"

func parseDay(text string) (time.Time, error) {
	d, err := time.Parse(dayLayout, text)
	// A year typed short ("0026") would otherwise be terms counted for two thousand years.
	if err != nil || d.Year() < 1990 || d.Year() > 2200 {
		return d, fmt.Errorf("%q isn't a date", text)
	}
	return d, nil
}

// addMonths is the same day of the month n months on, or that month's last day when
// it has no such day: a term from 31 January runs to the end of February, not into March.
func addMonths(from time.Time, n int) time.Time {
	first := time.Date(from.Year(), from.Month()+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(from.Day(), last)-1)
}

func (s Subscription) period() int {
	if s.Per == PerMonth {
		return 1
	}
	return 12
}

// Yearly is the price for a year.
func (s Subscription) Yearly() int64 {
	return s.Amount * int64(12/s.period())
}

// Figure is the price as it is typed, without what it is for.
func (s Subscription) Figure() string {
	if s.Amount == 0 {
		return ""
	}
	return formatMoney(s.Amount, s.Currency)
}

func (s Subscription) Price() string {
	if s.Amount == 0 {
		return ""
	}
	return formatMoney(s.Amount, s.Currency) + " a " + s.Per
}

// Standing is where a subscription stands on a day.
type Standing struct {
	Status string
	// TermEnd is the last day of the term that day falls in; empty before it starts.
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

// terms calls each for every term in turn, as its first day and its last, until each
// returns true. The first term is the commitment (or one price period when there is
// none); what follows repeats it, rolls a period at a time, or doesn't come.
func (s Subscription) terms(start time.Time, each func(first, last time.Time) bool) {
	length, step := s.TermMonths, s.TermMonths
	if length <= 0 {
		length, step = s.period(), s.period()
	}
	if s.Then == ThenRolls {
		step = s.period()
	}
	from := start
	// Bounded so a date centuries off can't spin: 2,400 terms is 200 years of months.
	for i := 0; i < 2400; i++ {
		// Every boundary is counted from the start, so a 31st doesn't drift to the 28th for good.
		next := addMonths(start, length+step*i)
		if each(from, next.AddDate(0, 0, -1)) || (s.Then == ThenEnds && s.TermMonths > 0) {
			return
		}
		from = next
	}
}

// On works out where the subscription stands on day (a date as 2006-01-02).
func (s Subscription) On(day string) Standing {
	start, err := parseDay(s.Start)
	if err != nil {
		return Standing{Status: SubScheduled}
	}
	text := func(d time.Time) string { return d.Format(dayLayout) }
	// Notice is counted back from the day the next term would start; with none to give,
	// the last day of the term is still in time.
	cancelBy := func(last time.Time) string { return text(last.AddDate(0, 0, 1-max(s.NoticeDays, 1))) }
	ends := s.Then == ThenEnds && s.TermMonths > 0

	var st Standing
	// The term day falls in, or the last there is.
	s.terms(start, func(first, last time.Time) bool {
		st.TermEnd = text(last)
		return day <= st.TermEnd
	})
	switch {
	case s.NoticeOn != nil:
		// Notice ends it at the close of the first term it was in time for.
		s.terms(start, func(first, last time.Time) bool {
			st.Ends = text(last)
			return *s.NoticeOn <= cancelBy(last)
		})
	case ends:
		st.Ends = st.TermEnd
	default:
		// The first term, from the one day is in, that notice could still end. With notice
		// longer than a term that is never the current one.
		s.terms(start, func(first, last time.Time) bool {
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

func mustDay(text string) time.Time {
	d, _ := time.Parse(dayLayout, text)
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

// soonDays is how far ahead a date counts as coming up: long enough to talk to the
// customer before a day to cancel by, short enough that the list means something.
const soonDays = 30

// within is a standing whose next date to know about falls within soonDays of day.
func (st Standing) within(day string) bool {
	at := st.Attention()
	return st.Status != SubEnded && at >= day && at <= mustDay(day).AddDate(0, 0, soonDays).Format(dayLayout)
}

// monthsNow is how long the term day falls in runs: the commitment, then whatever follows it.
func (s Subscription) monthsNow(day string) int {
	start, err := parseDay(s.Start)
	if err != nil || s.TermMonths <= 0 {
		return s.period()
	}
	if s.Then == ThenRolls && day >= addMonths(start, s.TermMonths).Format(dayLayout) {
		return s.period()
	}
	return s.TermMonths
}

// Soon is a subscription with something coming up worth looking at. One that turns
// over every month always has a term ending within the month, which is routine, so it
// counts only once it has a day it stops.
func (r SubscriptionRow) Soon(day string) bool {
	if r.Ends == "" && r.monthsNow(day) <= 1 {
		return false
	}
	return r.within(day)
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
		return errors.New("a term is between 0 and 120 months")
	}
	if notice < 0 || notice > 730 {
		return errors.New("notice is between 0 and 730 days")
	}
	return nil
}

func (s *Store) AddSubscription(org int64, in SubscriptionIn, actor string) (*Subscription, error) {
	in.Name = clip(strings.TrimSpace(in.Name), 200)
	if in.Name == "" {
		return nil, errors.New("say what they are on")
	}
	if in.Per == "" {
		in.Per = PerYear
	}
	if in.Then == "" {
		in.Then = ThenRenews
	}
	if in.Per != PerMonth && in.Per != PerYear {
		return nil, fmt.Errorf("a price is for a month or a year, not %q", in.Per)
	}
	if !validThen(in.Then) {
		return nil, fmt.Errorf("unknown end of term %q", in.Then)
	}
	if err := checkTerm(in.TermMonths, in.NoticeDays); err != nil {
		return nil, err
	}
	if in.TermMonths == 0 {
		// With nothing committed there is no term to renew or end: it rolls on.
		in.Then = ThenRolls
	}
	if _, err := parseDay(in.Start); err != nil {
		return nil, err
	}
	amount, err := parseMoney(in.Amount)
	if err != nil {
		return nil, err
	}
	var sub Subscription
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var c Company
		if err := tx.Where("org_id = ? and id = ?", org, in.Company).First(&c).Error; err != nil {
			return missing(err, "no company %d", in.Company)
		}
		var o Org
		if err := tx.First(&o, org).Error; err != nil {
			return err
		}
		sub = Subscription{OrgID: org, CompanyID: c.ID, Name: in.Name, Amount: amount, Currency: o.Settings.withDefaults().Currency,
			Per: in.Per, Start: in.Start, TermMonths: in.TermMonths, Then: in.Then, NoticeDays: in.NoticeDays}
		if err := tx.Create(&sub).Error; err != nil {
			return err
		}
		return s.log(tx, org, 0, &c.ID, actor, "sub:new", &in.Start)
	})
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// ChangeSubscription sets one field. The history keeps dates and terms, which say what
// was agreed when; a name is free text and a price is the customer's business, so
// those are noted as changed and no more.
func (s *Store) ChangeSubscription(org, id int64, field, value, actor string) error {
	value = strings.TrimSpace(value)
	return s.db.Transaction(func(tx *gorm.DB) error {
		var sub Subscription
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&sub).Error; err != nil {
			return missing(err, "no subscription %d", id)
		}
		updates := map[string]any{}
		logged := value
		switch field {
		case "name":
			if value == "" {
				return errors.New("say what they are on")
			}
			updates["name"], logged = clip(value, 200), ""
		case "amount":
			amount, err := parseMoney(value)
			if err != nil {
				return err
			}
			updates["amount"], logged = amount, ""
			if sub.Amount == 0 || sub.Currency == "" {
				var o Org
				if err := tx.First(&o, org).Error; err != nil {
					return err
				}
				updates["currency"] = o.Settings.withDefaults().Currency
			}
		case "per":
			if value != PerMonth && value != PerYear {
				return fmt.Errorf("a price is for a month or a year, not %q", value)
			}
			updates["per"] = value
		case "then":
			if !validThen(value) {
				return fmt.Errorf("unknown end of term %q", value)
			}
			if sub.TermMonths == 0 && value != ThenRolls {
				return errors.New("with no months committed it rolls on: say how many months first")
			}
			updates["then"] = value
		case "start":
			if _, err := parseDay(value); err != nil {
				return err
			}
			for _, later := range []*string{sub.NoticeOn, sub.EndedOn} {
				if later != nil && value > *later {
					return errors.New("that is after notice was given or it ended: clear that date first")
				}
			}
			updates["start"] = value
		case "notice_on", "ended_on":
			if value != "" {
				if _, err := parseDay(value); err != nil {
					return err
				}
				if value < sub.Start {
					return errors.New("that is before it started")
				}
			}
			updates[field] = nonEmpty(value)
		case "term_months", "notice_days":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%q isn't a number", value)
			}
			months, notice := sub.TermMonths, sub.NoticeDays
			if field == "term_months" {
				months = n
			} else {
				notice = n
			}
			if err := checkTerm(months, notice); err != nil {
				return err
			}
			updates[field] = n
			if months == 0 {
				updates["then"] = ThenRolls
			}
		default:
			return fmt.Errorf("a subscription has no %q", field)
		}
		if err := tx.Model(&sub).Updates(updates).Error; err != nil {
			return err
		}
		return s.log(tx, org, 0, &sub.CompanyID, actor, "sub:"+field, nonEmpty(logged))
	})
}

// RemoveSubscription takes back one recorded by mistake. One that ran its course is
// given an end date instead, so the record of it stays.
func (s *Store) RemoveSubscription(org, id int64, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var sub Subscription
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&sub).Error; err != nil {
			return missing(err, "no subscription %d", id)
		}
		if err := tx.Delete(&sub).Error; err != nil {
			return err
		}
		// A renewal deal for it carries on as a deal, pointing at nothing.
		if err := tx.Model(&Deal{}).Where("org_id = ? and subscription_id = ?", org, id).Update("subscription_id", 0).Error; err != nil {
			return err
		}
		return s.log(tx, org, 0, &sub.CompanyID, actor, "sub:remove", nil)
	})
}

// SubscriptionRow is a subscription with its company's name and where it stands.
type SubscriptionRow struct {
	Subscription
	Company string
	Standing
	// Renewing is whether a renewal deal for it is open on the Conversations board.
	Renewing bool
	// People are those at the company already in conversation: who a renewal can be with.
	People []Contact
}

// Subscriptions lists an organisation's, the one needing attention soonest first;
// company narrows it to one company's when it isn't 0.
func (s *Store) Subscriptions(org, company int64, day string) ([]SubscriptionRow, error) {
	q := s.in(org)
	if company != 0 {
		q = q.Where("company_id = ?", company)
	}
	var subs []Subscription
	if err := q.Order("id").Find(&subs).Error; err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, nil
	}
	var companies []Company
	if err := s.in(org).Where("id in (?)", s.in(org).Model(&Subscription{}).Select("company_id")).Find(&companies).Error; err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(companies))
	for _, c := range companies {
		names[c.ID] = c.Name
	}
	rows := make([]SubscriptionRow, len(subs))
	for i, sub := range subs {
		rows[i] = SubscriptionRow{Subscription: sub, Company: names[sub.CompanyID], Standing: sub.On(day)}
	}
	var renewing []int64
	if err := s.in(org).Model(&Deal{}).Where("subscription_id <> 0 and stage not in ?", []string{"won", "lost"}).
		Pluck("subscription_id", &renewing).Error; err != nil {
		return nil, err
	}
	var people []Contact
	if err := s.in(org).Where("company_id in (?)", s.in(org).Model(&Subscription{}).Select("company_id")).
		Where(inConversation).Order("id").Find(&people).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].Renewing = slices.Contains(renewing, rows[i].ID)
		for _, p := range people {
			if p.CompanyID != nil && *p.CompanyID == rows[i].CompanyID {
				rows[i].People = append(rows[i].People, p)
			}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Attention() < rows[j].Attention() })
	return rows, nil
}

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

func (a *App) subscriptionsPage(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	rows, err := a.s.Subscriptions(q.Org.ID, 0, today())
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	a.page(w, r, "subscriptions", SubscriptionsView(q, rows, today()))
}

func (a *App) uiSubscription(w http.ResponseWriter, r *http.Request) {
	var in SubscriptionIn
	if decode(w, r, &in) {
		q := reqOf(r)
		_, err := a.s.AddSubscription(q.Org.ID, in, q.Actor())
		done(w, err)
	}
}

func (a *App) uiSubscriptionChange(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID    int64  `json:"id"`
		Field string `json:"field"`
		Value string `json:"value"`
	}
	if decode(w, r, &in) {
		q := reqOf(r)
		done(w, a.s.ChangeSubscription(q.Org.ID, in.ID, in.Field, in.Value, q.Actor()))
	}
}

func (a *App) uiSubscriptionRemove(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID int64 `json:"id"`
	}
	if decode(w, r, &in) {
		q := reqOf(r)
		done(w, a.s.RemoveSubscription(q.Org.ID, in.ID, q.Actor()))
	}
}

// StartRenewal opens a renewal deal for a subscription with someone at its company who
// is already in conversation. The deal starts with what the subscription costs for a
// year and closes when the term in hand ends, both of which can then be changed.
func (s *Store) StartRenewal(org, subscription, contact int64, actor string) (*Deal, error) {
	var d Deal
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// Held, so two started together are numbered one after the other.
		if err := hold(tx, &Contact{}, org, contact); err != nil {
			return err
		}
		var sub Subscription
		if err := tx.Where("org_id = ? and id = ?", org, subscription).First(&sub).Error; err != nil {
			return missing(err, "no subscription %d", subscription)
		}
		var c Contact
		if err := tx.Where("org_id = ? and id = ?", org, contact).First(&c).Error; err != nil {
			return missing(err, "no contact %d", contact)
		}
		if c.CompanyID == nil || *c.CompanyID != sub.CompanyID {
			return errors.New("that person is not at the company the subscription is with")
		}
		if c.Replied == nil || c.Declined != nil || c.Status == ContactSuggested {
			return errors.New("a renewal is with someone you are in conversation with")
		}
		st := sub.On(today())
		if st.Status == SubEnded {
			return errors.New("that subscription has ended: record a new one when they come back")
		}
		var open int64
		if err := tx.Model(&Deal{}).Where("org_id = ? and subscription_id = ? and stage not in ?", org, sub.ID, []string{"won", "lost"}).
			Count(&open).Error; err != nil {
			return err
		}
		if open > 0 {
			return errors.New("a renewal for it is already under way on the Conversations board")
		}
		var last int
		if err := tx.Model(&Deal{}).Where("org_id = ? and contact_id = ?", org, contact).
			Select("coalesce(max(seq), 0)").Scan(&last).Error; err != nil {
			return err
		}
		closes := st.TermEnd
		if st.Ends != "" {
			closes = st.Ends
		}
		d = Deal{OrgID: org, ContactID: contact, Seq: last + 1, Kind: "renewal", Stage: Stages[0].Key, SubscriptionID: sub.ID,
			Amount: sub.Yearly(), Currency: sub.Currency, CloseDate: nonEmpty(closes)}
		if err := tx.Create(&d).Error; err != nil {
			return err
		}
		return s.log(tx, org, contact, c.CompanyID, actor, "deal:start", str("renewal"))
	})
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (a *App) uiSubscriptionRenew(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID      int64  `json:"id"`
		Contact string `json:"contact"`
	}
	if !decode(w, r, &in) {
		return
	}
	// A select posts its value as text.
	contact, err := strconv.ParseInt(in.Contact, 10, 64)
	if err != nil {
		fail(w, errors.New("choose who the renewal is with"), http.StatusBadRequest)
		return
	}
	q := reqOf(r)
	_, err = a.s.StartRenewal(q.Org.ID, in.ID, contact, q.Actor())
	done(w, err)
}
