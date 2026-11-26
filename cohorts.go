package main

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
)

// Cohort is a kind of company an organisation sells to: its companies fall into it by
// its rules, or by hand. Once any exist, exactly one is the catch-all, always last,
// which takes whatever no rule does. With none, every company is in none, as before.
type Cohort struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	OrgID      int64     `gorm:"uniqueIndex:idx_cohort_name,priority:1;not null" json:"-"`
	Name       string    `gorm:"uniqueIndex:idx_cohort_name,priority:2;not null" json:"name"`
	Position   int       `gorm:"not null;default:0" json:"position"`
	Colour     string    `gorm:"not null;default:''" json:"colour"`
	WhoTheyAre string    `gorm:"not null;default:''" json:"who_they_are"`
	CatchAll   bool      `gorm:"not null;default:false" json:"catch_all"`
	UpdatedBy  string    `gorm:"not null;default:''" json:"updated_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`

	Rules []CohortRule `gorm:"-" json:"-"`
	// Companies and ByHand count its companies, and those of them put there by hand.
	Companies int `gorm:"-" json:"-"`
	ByHand    int `gorm:"-" json:"-"`
}

// CohortRule is one way into a cohort; any of a cohort's rules is enough.
type CohortRule struct {
	ID       int64  `gorm:"primaryKey" json:"id"`
	OrgID    int64  `gorm:"index:idx_cohort_rules,priority:1;not null" json:"-"`
	CohortID int64  `gorm:"index:idx_cohort_rules,priority:2;not null" json:"cohort_id"`
	Position int    `gorm:"not null;default:0" json:"position"`
	Field    string `gorm:"not null;default:''" json:"field"`
	Op       string `gorm:"not null;default:''" json:"op"`
	// Value is a comma list for any_of, an amount in hundredths for at_least, and
	// monthly or yearly for billed.
	Value string `gorm:"not null;default:''" json:"value"`
}

const (
	cohortNameMax  = 60
	cohortRulesMax = 20
	opAnyOf        = "any_of"
	opAtLeast      = "at_least"
	opIs           = "is"
	// byRules is who the history says moved a company its rules placed.
	byRules = "rules"
)

var CohortColours = []string{"#7b8cff", "#ff9f43", "#35c58a", "#f06595", "#a77bff", "#22b8cf", "#e0b43c", "#b08968"}

var cohortFields = []struct{ Key, Op string }{
	{"segment", opAnyOf}, {"country", opAnyOf}, {"offering", opAnyOf}, {"conversation_stage", opAnyOf},
	{"deal_value", opAtLeast}, {"pays_a_year", opAtLeast}, {"billed", opIs}, {"deal_kind", opAnyOf}, {"found_by", opAnyOf},
}

func cohortFieldOp(field string) string {
	for _, f := range cohortFields {
		if f.Key == field {
			return f.Op
		}
	}
	return ""
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// leadPhrase is what a segment says the company is: what comes before whom it serves.
func leadPhrase(segment string) string {
	s := " " + strings.ToLower(segment) + " "
	cut := len(s)
	for _, sep := range []string{" for ", " to ", " serving ", " with "} {
		if i := strings.Index(s, sep); i >= 0 && i < cut {
			cut = i
		}
	}
	return s[:cut]
}

func segmentWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// segmentMatch is the first of the rule's words found in the segment's lead phrase,
// each word matching one that starts with it, so "carmaker" finds "carmakers".
func segmentMatch(segment string, ruleWords []string) (string, bool) {
	lead := segmentWords(leadPhrase(segment))
	for _, rw := range ruleWords {
		want := segmentWords(rw)
		if len(want) == 0 {
			continue
		}
		for i := 0; i+len(want) <= len(lead); i++ {
			hit := true
			for j, w := range want {
				if !strings.HasPrefix(lead[i+j], w) {
					hit = false
					break
				}
			}
			if hit {
				return rw, true
			}
		}
	}
	return "", false
}

// cohortInputs is what a company's rules are judged on.
type cohortInputs struct {
	Segment, Country, FoundBy string
	FoundFor                  int64
	Offerings                 map[int64]bool
	Stages                    map[string]bool
	// BestDeal is the largest open deal in the organisation's currency; OtherCurrency
	// counts the open deals in another, which no rule can weigh.
	BestDeal      int64
	OtherCurrency int
	PaysAYear     int64
	Billed        map[string]bool
	DealKinds     map[string]bool
}

// cohortContext is what saying why a rule matched needs.
type cohortContext struct {
	Currency  string
	OwnName   string
	Offerings map[int64]string
}

func (cx cohortContext) offeringName(id int64) string {
	if id == 0 {
		return cx.OwnName
	}
	if n, ok := cx.Offerings[id]; ok {
		return n
	}
	return "offering " + strconv.FormatInt(id, 10)
}

func (r CohortRule) amount() int64 {
	n, _ := strconv.ParseInt(r.Value, 10, 64)
	return n
}

// matches says whether a company meets the rule, and in that case why, in words.
func (r CohortRule) matches(in cohortInputs, cx cohortContext) (string, bool) {
	values := splitList(r.Value)
	anyOf := func(have func(v string) bool) (string, bool) {
		for _, v := range values {
			if have(v) {
				return v, true
			}
		}
		return "", false
	}
	quoted := func(label string, v string, ok bool) (string, bool) {
		if !ok {
			return "", false
		}
		return fmt.Sprintf("%s %q", label, v), true
	}
	switch r.Field {
	case "segment":
		v, ok := segmentMatch(in.Segment, values)
		return quoted("segment", v, ok)
	case "country":
		v, ok := anyOf(func(v string) bool { return strings.EqualFold(v, strings.TrimSpace(in.Country)) })
		return quoted("country", v, ok)
	case "offering":
		v, ok := anyOf(func(v string) bool {
			id, err := strconv.ParseInt(v, 10, 64)
			return err == nil && (in.Offerings[id] || (id != 0 && in.FoundFor == id))
		})
		if !ok {
			return "", false
		}
		id, _ := strconv.ParseInt(v, 10, 64)
		return fmt.Sprintf("offering %q", cx.offeringName(id)), true
	case "conversation_stage":
		v, ok := anyOf(func(v string) bool { return in.Stages[v] })
		return quoted("conversation", v, ok)
	case "deal_kind":
		v, ok := anyOf(func(v string) bool { return in.DealKinds[v] })
		return quoted("deal kind", v, ok)
	case "found_by":
		v, ok := anyOf(func(v string) bool { return strings.EqualFold(v, in.FoundBy) })
		return quoted("found by", v, ok)
	case "deal_value":
		if in.BestDeal > 0 && in.BestDeal >= r.amount() {
			return "open deal of at least " + formatMoney(r.amount(), cx.Currency), true
		}
	case "pays_a_year":
		if in.PaysAYear > 0 && in.PaysAYear >= r.amount() {
			return "pays at least " + formatMoney(r.amount(), cx.Currency) + " a year", true
		}
	case "billed":
		if in.Billed[r.Value] {
			return "billed " + r.Value, true
		}
	}
	return "", false
}

// placement is the cohort a company's rules put it in, and why: the first cohort, in
// order, with a rule it meets, or the catch-all with no reason when none does.
func placement(cohorts []Cohort, in cohortInputs, cx cohortContext) (int64, string) {
	if len(cohorts) == 0 {
		return 0, ""
	}
	for _, c := range cohorts {
		for _, r := range c.Rules {
			if why, ok := r.matches(in, cx); ok {
				return c.ID, why
			}
		}
	}
	for _, c := range cohorts {
		if c.CatchAll {
			return c.ID, ""
		}
	}
	return cohorts[len(cohorts)-1].ID, ""
}

func loadCohorts(tx *gorm.DB, org int64) ([]Cohort, error) {
	var cohorts []Cohort
	if err := tx.Where("org_id = ?", org).Order("position, id").Find(&cohorts).Error; err != nil {
		return nil, err
	}
	if len(cohorts) == 0 {
		return nil, nil
	}
	var rules []CohortRule
	if err := tx.Where("org_id = ?", org).Order("cohort_id, position, id").Find(&rules).Error; err != nil {
		return nil, err
	}
	at := map[int64]int{}
	for i, c := range cohorts {
		at[c.ID] = i
	}
	for _, r := range rules {
		if i, ok := at[r.CohortID]; ok {
			cohorts[i].Rules = append(cohorts[i].Rules, r)
		}
	}
	return cohorts, nil
}

func loadCohortContext(tx *gorm.DB, org int64) (cohortContext, error) {
	var o Org
	if err := tx.First(&o, org).Error; err != nil {
		return cohortContext{}, err
	}
	cx := cohortContext{Currency: o.Settings.withDefaults().Currency, OwnName: o.Name, Offerings: map[int64]string{}}
	var offerings []Offering
	if err := tx.Select("id", "name").Where("org_id = ?", org).Find(&offerings).Error; err != nil {
		return cx, err
	}
	for _, of := range offerings {
		cx.Offerings[of.ID] = of.Name
	}
	return cx, nil
}

// loadInputs reads what the rules look at for these companies, or for all of the
// organisation's when ids is nil, a few queries for the lot.
func loadInputs(tx *gorm.DB, org int64, ids []int64, currency, day string) ([]Company, map[int64]*cohortInputs, error) {
	scoped := func(q *gorm.DB, column string) *gorm.DB {
		q = q.Where("org_id = ?", org)
		if ids != nil {
			q = q.Where(column+" in ?", ids)
		}
		return q
	}
	var companies []Company
	if err := scoped(tx, "id").Order("id").Find(&companies).Error; err != nil {
		return nil, nil, err
	}
	out := make(map[int64]*cohortInputs, len(companies))
	for _, c := range companies {
		out[c.ID] = &cohortInputs{Segment: c.Segment, Country: c.Country, FoundBy: c.FoundBy, FoundFor: c.FoundFor,
			Offerings: map[int64]bool{}, Stages: map[string]bool{}, Billed: map[string]bool{}, DealKinds: map[string]bool{}}
	}
	if len(companies) == 0 {
		return companies, out, nil
	}
	var people []Contact
	if err := scoped(tx, "company_id").Where("company_id is not null and key not like 'org:%'").Find(&people).Error; err != nil {
		return nil, nil, err
	}
	at := map[int64]Contact{}
	for _, p := range people {
		in := out[*p.CompanyID]
		if in == nil {
			continue
		}
		at[p.ID] = p
		if p.Status != ContactSuggested {
			in.Offerings[p.OfferingID] = true
		}
		if p.Replied != nil && p.Declined == nil && p.Status != ContactSuggested {
			in.Stages[p.ConversationStage()] = true
		}
	}
	var deals []Deal
	q := tx.Where("org_id = ?", org)
	if ids != nil {
		q = q.Where("contact_id in (?)", tx.Model(&Contact{}).Select("id").Where("org_id = ? and company_id in ?", org, ids))
	}
	if err := q.Find(&deals).Error; err != nil {
		return nil, nil, err
	}
	for _, d := range deals {
		p, ok := at[d.ContactID]
		if !ok {
			continue
		}
		in := out[*p.CompanyID]
		stage, open := d.Stage, false
		if d.Seq == 0 {
			stage = p.ConversationStage()
			open = p.Replied != nil && p.Declined == nil && p.Status != ContactSuggested && stage != "won" && stage != "lost"
			if p.Declined != nil && stage != "won" {
				stage = "lost"
			}
		} else {
			open = stage != "won" && stage != "lost"
		}
		if stage != "lost" {
			in.DealKinds[d.Kind] = true
		}
		if !open || d.Amount == 0 {
			continue
		}
		if d.Currency != "" && d.Currency != currency {
			in.OtherCurrency++
			continue
		}
		in.BestDeal = max(in.BestDeal, d.Amount)
	}
	var subs []Subscription
	if err := scoped(tx, "company_id").Find(&subs).Error; err != nil {
		return nil, nil, err
	}
	for _, sub := range subs {
		in := out[sub.CompanyID]
		if in == nil {
			continue
		}
		switch sub.On(day).Status {
		case SubActive, SubNotice, SubEnding:
		default:
			continue
		}
		in.Billed[map[string]string{PerMonth: "monthly", PerYear: "yearly"}[sub.Per]] = true
		if sub.Currency == "" || sub.Currency == currency {
			in.PaysAYear += sub.Yearly()
		}
	}
	return companies, out, nil
}

// cohortMove is a company going from one cohort to another, or one kept by hand where
// its rules would put it elsewhere.
type cohortMove struct {
	CompanyID int64
	Company   string
	From, To  int64
}

// placed is what placing a set of companies did.
type placed struct {
	Moves []cohortMove
	// Kept are companies put somewhere by hand that their rules would put elsewhere.
	Kept []cohortMove
	// OtherCurrency counts the open deals at them that no rule could weigh.
	OtherCurrency int
}

// placeCompany puts a company where its rules say, unless someone put it somewhere by
// hand. Every write that changes what the rules read calls it in its own transaction.
func placeCompany(tx *gorm.DB, org, company int64) error {
	_, err := placeCompanies(tx, org, []int64{company}, today(), byRules)
	return err
}

// placeContactCompany places the company a person is at, if any.
func placeContactCompany(tx *gorm.DB, org int64, company *int64) error {
	if company == nil {
		return nil
	}
	return placeCompany(tx, org, *company)
}

// placeAll is placeCompany for every company in the organisation: after its cohorts
// change, a bulk change to its companies, or with time, as subscriptions end.
func placeAll(tx *gorm.DB, org int64, day string) (placed, error) {
	return placeCompanies(tx, org, nil, day, byRules)
}

func placeCompanies(tx *gorm.DB, org int64, ids []int64, day, actor string) (placed, error) {
	return placeNaming(tx, org, ids, day, actor, nil)
}

// placeNaming places companies, naming in their history the cohorts in gone, which a
// removal has already taken out, by the names they had.
func placeNaming(tx *gorm.DB, org int64, ids []int64, day, actor string, gone map[int64]string) (placed, error) {
	var done placed
	cohorts, err := loadCohorts(tx, org)
	if err != nil || len(cohorts) == 0 {
		return done, err
	}
	cx, err := loadCohortContext(tx, org)
	if err != nil {
		return done, err
	}
	companies, inputs, err := loadInputs(tx, org, ids, cx.Currency, day)
	if err != nil {
		return done, err
	}
	names := map[int64]string{0: "no cohort"}
	for id, name := range gone {
		names[id] = name
	}
	exists := map[int64]bool{}
	for _, c := range cohorts {
		names[c.ID], exists[c.ID] = c.Name, true
	}
	for _, c := range companies {
		in := inputs[c.ID]
		done.OtherCurrency += in.OtherCurrency
		to, why := placement(cohorts, *in, cx)
		if c.CohortByHand && exists[c.CohortID] {
			if to != c.CohortID {
				done.Kept = append(done.Kept, cohortMove{c.ID, c.Name, c.CohortID, to})
			}
			continue
		}
		if to == c.CohortID && why == c.CohortWhy && !c.CohortByHand {
			continue
		}
		if err := tx.Model(&Company{}).Where("org_id = ? and id = ?", org, c.ID).
			UpdateColumns(map[string]any{"cohort_id": to, "cohort_why": why, "cohort_by_hand": false}).Error; err != nil {
			return done, err
		}
		if to == c.CohortID {
			continue
		}
		done.Moves = append(done.Moves, cohortMove{c.ID, c.Name, c.CohortID, to})
		if err := logCohortMove(tx, org, c.ID, actor, names[c.CohortID], names[to], whySaid(why)); err != nil {
			return done, err
		}
	}
	return done, nil
}

func whySaid(why string) string {
	if why == "" {
		return "no rule matched"
	}
	return why
}

func logCohortMove(tx *gorm.DB, org, company int64, actor, from, to, why string) error {
	value := from + " → " + to + " · " + why
	return tx.Create(&Event{OrgID: org, CompanyID: &company, User: actor, Action: "company:cohort", Value: &value, At: time.Now()}).Error
}

// CohortChange is what a cohort is to become; nil leaves a field as it is.
type CohortChange struct {
	Name       *string
	Colour     *string
	WhoTheyAre *string
	CatchAll   *bool
	Rules      *[]CohortRule
}

func checkRule(tx *gorm.DB, org int64, r CohortRule) (CohortRule, error) {
	r.Field = strings.TrimSpace(r.Field)
	op := cohortFieldOp(r.Field)
	if op == "" {
		return r, fmt.Errorf("a rule can't look at %q", r.Field)
	}
	if r.Op == "" {
		r.Op = op
	}
	if r.Op != op {
		return r, fmt.Errorf("a %s rule is %s, not %s", r.Field, op, r.Op)
	}
	values := splitList(r.Value)
	switch r.Op {
	case opAtLeast:
		n, err := strconv.ParseInt(strings.TrimSpace(r.Value), 10, 64)
		if err != nil || n < 0 {
			return r, fmt.Errorf("a %s rule needs an amount in hundredths, not %q", r.Field, r.Value)
		}
		r.Value = strconv.FormatInt(n, 10)
		return r, nil
	case opIs:
		if v := strings.ToLower(strings.TrimSpace(r.Value)); v == "monthly" || v == "yearly" {
			r.Value = v
			return r, nil
		}
		return r, fmt.Errorf("billed is monthly or yearly, not %q", r.Value)
	}
	if len(values) == 0 {
		return r, fmt.Errorf("a %s rule needs something to match", r.Field)
	}
	for i, v := range values {
		switch r.Field {
		case "segment":
			values[i] = strings.ToLower(v)
		case "country":
			values[i] = strings.ToUpper(v)
		case "offering":
			id, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return r, fmt.Errorf("an offering rule names offerings by id, not %q", v)
			}
			if id != 0 {
				var n int64
				if err := tx.Model(&Offering{}).Where("org_id = ? and id = ?", org, id).Count(&n).Error; err != nil {
					return r, err
				}
				if n == 0 {
					return r, noSuch("no offering %d", id)
				}
			}
		case "conversation_stage":
			if !validStage(v) {
				return r, fmt.Errorf("unknown conversation stage %q", v)
			}
		case "deal_kind":
			if !kindOfDeal(v) {
				return r, fmt.Errorf("unknown kind of deal %q", v)
			}
		}
	}
	r.Value = strings.Join(slices.Compact(values), ", ")
	if utf8.RuneCountInString(r.Value) > 500 {
		return r, errors.New("a rule's words are 500 characters at most")
	}
	return r, nil
}

func cohortNameFree(tx *gorm.DB, org, id int64, name string) error {
	var n int64
	if err := tx.Model(&Cohort{}).Where("org_id = ? and id <> ? and lower(name) = ?", org, id, strings.ToLower(name)).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return conflict(fmt.Sprintf("there is already a cohort called %q", name))
	}
	return nil
}

// applyCohort checks a change and writes it to the cohort and its rules.
func applyCohort(tx *gorm.DB, org int64, c *Cohort, in CohortChange, actor string, others []Cohort) error {
	put(&c.Name, in.Name)
	put(&c.WhoTheyAre, in.WhoTheyAre)
	put(&c.Colour, in.Colour)
	c.Name, c.WhoTheyAre = strings.TrimSpace(c.Name), strings.TrimSpace(c.WhoTheyAre)
	if c.Name == "" {
		return errors.New("a cohort needs a name")
	}
	if utf8.RuneCountInString(c.Name) > cohortNameMax {
		return fmt.Errorf("a cohort's name is %d characters at most", cohortNameMax)
	}
	if utf8.RuneCountInString(c.WhoTheyAre) > 1000 {
		return errors.New("who they are is 1000 characters at most")
	}
	c.Colour = strings.ToLower(strings.TrimSpace(c.Colour))
	if c.Colour == "" {
		c.Colour = freeColour(others)
	}
	if !slices.Contains(CohortColours, c.Colour) {
		return fmt.Errorf("a cohort's colour is one of %s", strings.Join(CohortColours, ", "))
	}
	if err := cohortNameFree(tx, org, c.ID, c.Name); err != nil {
		return err
	}
	c.UpdatedBy = actor
	if c.ID == 0 {
		if err := tx.Create(c).Error; err != nil {
			return err
		}
	} else if err := tx.Model(c).Select("name", "colour", "who_they_are", "updated_by", "updated_at").Updates(c).Error; err != nil {
		return err
	}
	if in.Rules == nil {
		return nil
	}
	if len(*in.Rules) > cohortRulesMax {
		return fmt.Errorf("a cohort has %d rules at most", cohortRulesMax)
	}
	if err := tx.Where("org_id = ? and cohort_id = ?", org, c.ID).Delete(&CohortRule{}).Error; err != nil {
		return err
	}
	c.Rules = nil
	for i, r := range *in.Rules {
		r, err := checkRule(tx, org, r)
		if err != nil {
			return err
		}
		r.ID, r.OrgID, r.CohortID, r.Position = 0, org, c.ID, i
		if err := tx.Create(&r).Error; err != nil {
			return err
		}
		c.Rules = append(c.Rules, r)
	}
	return nil
}

func freeColour(others []Cohort) string {
	for _, colour := range CohortColours {
		if !slices.ContainsFunc(others, func(c Cohort) bool { return c.Colour == colour }) {
			return colour
		}
	}
	return CohortColours[len(others)%len(CohortColours)]
}

// renumber writes the cohorts' positions in the order given, the catch-all last.
func renumber(tx *gorm.DB, org int64, cohorts []Cohort) error {
	slices.SortStableFunc(cohorts, func(a, b Cohort) int {
		switch {
		case a.CatchAll == b.CatchAll:
			return 0
		case a.CatchAll:
			return 1
		}
		return -1
	})
	for i, c := range cohorts {
		if err := tx.Model(&Cohort{}).Where("org_id = ? and id = ?", org, c.ID).
			UpdateColumns(map[string]any{"position": i, "catch_all": c.CatchAll}).Error; err != nil {
			return err
		}
	}
	return nil
}

// saveCohort adds a cohort (id 0) or changes one, puts the catch-all last, and places
// every company again. It is what saving does and what a preview does before taking it back.
func saveCohort(tx *gorm.DB, org, id int64, in CohortChange, actor, day string) (*Cohort, placed, error) {
	if err := holdOrg(tx, org); err != nil {
		return nil, placed{}, err
	}
	cohorts, err := loadCohorts(tx, org)
	if err != nil {
		return nil, placed{}, err
	}
	var c *Cohort
	others := []Cohort{}
	for i := range cohorts {
		if cohorts[i].ID == id && id != 0 {
			c = &cohorts[i]
		} else {
			others = append(others, cohorts[i])
		}
	}
	if id != 0 && c == nil {
		return nil, placed{}, noSuch("no cohort %d", id)
	}
	if c == nil {
		c = &Cohort{OrgID: org, Position: len(cohorts), CatchAll: len(cohorts) == 0}
	}
	if in.CatchAll != nil {
		switch {
		case *in.CatchAll:
			c.CatchAll = true
		case c.CatchAll && len(others) > 0:
			return nil, placed{}, errors.New("there is always a catch-all: make another cohort the catch-all instead")
		}
	}
	if err := applyCohort(tx, org, c, in, actor, others); err != nil {
		return nil, placed{}, err
	}
	order := others
	if c.CatchAll {
		for i := range order {
			order[i].CatchAll = false
		}
		order = append(order, *c)
	} else {
		// A new one goes before the catch-all; one changed keeps its place.
		at := len(order)
		for i, o := range order {
			if o.Position > c.Position || (id == 0 && o.CatchAll) {
				at = i
				break
			}
		}
		order = slices.Insert(order, at, *c)
	}
	if err := renumber(tx, org, order); err != nil {
		return nil, placed{}, err
	}
	what := "cohort:changed"
	if id == 0 {
		what = "cohort:added"
	}
	if err := tx.Create(&Event{OrgID: org, User: actor, Action: what, Value: &c.Name, At: time.Now()}).Error; err != nil {
		return nil, placed{}, err
	}
	done, err := placeAll(tx, org, day)
	if err != nil {
		return nil, placed{}, err
	}
	return c, done, tx.Where("org_id = ? and id = ?", org, c.ID).First(c).Error
}

// Cohorts are the organisation's cohorts in order, the catch-all last, each with its
// rules and how many companies are in it.
func (s *Store) Cohorts(org int64) ([]Cohort, error) {
	cohorts, err := loadCohorts(s.db, org)
	if err != nil || len(cohorts) == 0 {
		return []Cohort{}, err
	}
	var counts []struct {
		CohortID     int64
		CohortByHand bool
		N            int
	}
	if err := s.in(org).Model(&Company{}).Select("cohort_id, cohort_by_hand, count(*) as n").Group("cohort_id, cohort_by_hand").Scan(&counts).Error; err != nil {
		return nil, err
	}
	for i := range cohorts {
		for _, n := range counts {
			if n.CohortID == cohorts[i].ID {
				cohorts[i].Companies += n.N
				if n.CohortByHand {
					cohorts[i].ByHand += n.N
				}
			}
		}
	}
	return cohorts, nil
}

func (s *Store) cohort(org, id int64) (*Cohort, error) {
	cohorts, err := s.Cohorts(org)
	if err != nil {
		return nil, err
	}
	for _, c := range cohorts {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, noSuch("no cohort %d", id)
}

func (s *Store) AddCohort(org int64, in CohortChange, actor string) (*Cohort, error) {
	var c *Cohort
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		c, _, err = saveCohort(tx, org, 0, in, actor, today())
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.cohort(org, c.ID)
}

func (s *Store) ChangeCohort(org, id int64, in CohortChange, actor string) (*Cohort, error) {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		_, _, err := saveCohort(tx, org, id, in, actor, today())
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.cohort(org, id)
}

// CohortPreview is who saving a cohort would move: into it, out of it, and those put
// somewhere by hand who stay where they are.
type CohortPreview struct {
	Cohort        Cohort
	In, Out, Kept []CohortMoveRow
	OtherCurrency int
}

type CohortMoveRow struct {
	CompanyID        int64
	Company          string
	From, To         int64
	FromName, ToName string
}

var errPreviewed = errors.New("previewed")

// PreviewCohort saves the cohort (a new one for id 0) and places every company again,
// as saving would, then takes it all back, so what it says is what saving does.
func (s *Store) PreviewCohort(org, id int64, in CohortChange) (*CohortPreview, error) {
	var out CohortPreview
	err := s.db.Transaction(func(tx *gorm.DB) error {
		c, done, err := saveCohort(tx, org, id, in, "preview", today())
		if err != nil {
			return err
		}
		cohorts, err := loadCohorts(tx, org)
		if err != nil {
			return err
		}
		names := map[int64]string{0: "no cohort"}
		for _, k := range cohorts {
			names[k.ID] = k.Name
		}
		row := func(m cohortMove) CohortMoveRow {
			out := CohortMoveRow{m.CompanyID, m.Company, m.From, m.To, names[m.From], names[m.To]}
			if id == 0 {
				// Taken back with the rest, the new cohort's id belongs to nothing.
				for _, at := range []*int64{&out.From, &out.To} {
					if *at == c.ID {
						*at = 0
					}
				}
			}
			return out
		}
		out = CohortPreview{Cohort: *c, In: []CohortMoveRow{}, Out: []CohortMoveRow{}, Kept: []CohortMoveRow{}, OtherCurrency: done.OtherCurrency}
		for _, m := range done.Moves {
			switch c.ID {
			case m.To:
				out.In = append(out.In, row(m))
			case m.From:
				out.Out = append(out.Out, row(m))
			}
		}
		for _, m := range done.Kept {
			if m.From == c.ID || m.To == c.ID {
				out.Kept = append(out.Kept, row(m))
			}
		}
		if id == 0 {
			out.Cohort.ID = 0
		}
		return errPreviewed
	})
	if !errors.Is(err, errPreviewed) {
		return nil, err
	}
	return &out, nil
}

// MoveCohort puts a cohort at a position in the order, counting from 0. The catch-all
// stays last.
func (s *Store) MoveCohort(org, id int64, position int, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := holdOrg(tx, org); err != nil {
			return err
		}
		cohorts, err := loadCohorts(tx, org)
		if err != nil {
			return err
		}
		at := slices.IndexFunc(cohorts, func(c Cohort) bool { return c.ID == id })
		if at < 0 {
			return noSuch("no cohort %d", id)
		}
		c := cohorts[at]
		if c.CatchAll {
			return errors.New("the catch-all is always last: make another cohort the catch-all to move it")
		}
		cohorts = slices.Delete(cohorts, at, at+1)
		position = max(0, min(position, len(cohorts)-1))
		cohorts = slices.Insert(cohorts, position, c)
		if err := renumber(tx, org, cohorts); err != nil {
			return err
		}
		if err := tx.Create(&Event{OrgID: org, User: actor, Action: "cohort:moved", Value: &c.Name, At: time.Now()}).Error; err != nil {
			return err
		}
		_, err = placeAll(tx, org, today())
		return err
	})
}

// RemoveCohort deletes a cohort and its rules. Its companies, those put there by hand
// too, are placed again by the rules; removing the catch-all makes the last one left
// the catch-all, and removing the last cohort leaves every company in none.
func (s *Store) RemoveCohort(org, id int64, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := holdOrg(tx, org); err != nil {
			return err
		}
		cohorts, err := loadCohorts(tx, org)
		if err != nil {
			return err
		}
		at := slices.IndexFunc(cohorts, func(c Cohort) bool { return c.ID == id })
		if at < 0 {
			return noSuch("no cohort %d", id)
		}
		gone := cohorts[at]
		cohorts = slices.Delete(cohorts, at, at+1)
		if gone.CatchAll && len(cohorts) > 0 {
			cohorts[len(cohorts)-1].CatchAll = true
		}
		if err := tx.Where("org_id = ? and cohort_id = ?", org, id).Delete(&CohortRule{}).Error; err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and id = ?", org, id).Delete(&Cohort{}).Error; err != nil {
			return err
		}
		if err := renumber(tx, org, cohorts); err != nil {
			return err
		}
		if err := tx.Create(&Event{OrgID: org, User: actor, Action: "cohort:removed", Value: &gone.Name, At: time.Now()}).Error; err != nil {
			return err
		}
		if len(cohorts) == 0 {
			var ids []int64
			if err := tx.Model(&Company{}).Where("org_id = ? and cohort_id = ?", org, id).Pluck("id", &ids).Error; err != nil {
				return err
			}
			for _, company := range ids {
				if err := logCohortMove(tx, org, company, byRules, gone.Name, "no cohort", "its cohort was removed"); err != nil {
					return err
				}
			}
			return tx.Model(&Company{}).Where("org_id = ?", org).
				UpdateColumns(map[string]any{"cohort_id": 0, "cohort_why": "", "cohort_by_hand": false}).Error
		}
		if err := tx.Model(&Company{}).Where("org_id = ? and cohort_id = ?", org, id).UpdateColumn("cohort_by_hand", false).Error; err != nil {
			return err
		}
		_, err = placeNaming(tx, org, nil, today(), byRules, map[int64]string{id: gone.Name})
		return err
	})
}

// SetCompanyCohort puts a company in a cohort by hand, where it stays whatever its rules
// say until it is put back to them.
func (s *Store) SetCompanyCohort(org, company, cohort int64, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := holdOrg(tx, org); err != nil {
			return err
		}
		var c Company
		if err := tx.Where("org_id = ? and id = ?", org, company).First(&c).Error; err != nil {
			return missing(err, "no company %d", company)
		}
		var to Cohort
		if err := tx.Where("org_id = ? and id = ?", org, cohort).First(&to).Error; err != nil {
			return missing(err, "no cohort %d", cohort)
		}
		if c.CohortByHand && c.CohortID == cohort {
			return nil
		}
		from := "no cohort"
		var was Cohort
		if err := tx.Where("org_id = ? and id = ?", org, c.CohortID).Limit(1).Find(&was).Error; err != nil {
			return err
		}
		if was.ID != 0 {
			from = was.Name
		}
		if err := tx.Model(&Company{}).Where("org_id = ? and id = ?", org, company).
			UpdateColumns(map[string]any{"cohort_id": cohort, "cohort_why": "", "cohort_by_hand": true}).Error; err != nil {
			return err
		}
		return logCohortMove(tx, org, company, actor, from, to.Name, "by hand")
	})
}

// CompanyByRule puts a company placed by hand back where its rules say.
func (s *Store) CompanyByRule(org, company int64, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := holdOrg(tx, org); err != nil {
			return err
		}
		var c Company
		if err := tx.Where("org_id = ? and id = ?", org, company).First(&c).Error; err != nil {
			return missing(err, "no company %d", company)
		}
		if !c.CohortByHand {
			return nil
		}
		if err := tx.Model(&Company{}).Where("org_id = ? and id = ?", org, company).UpdateColumn("cohort_by_hand", false).Error; err != nil {
			return err
		}
		done, err := placeCompanies(tx, org, []int64{company}, today(), actor)
		if err != nil || len(done.Moves) > 0 {
			return err
		}
		var now Company
		if err := tx.Where("org_id = ? and id = ?", org, company).First(&now).Error; err != nil {
			return err
		}
		var in Cohort
		if err := tx.Where("org_id = ? and id = ?", org, now.CohortID).Limit(1).Find(&in).Error; err != nil {
			return err
		}
		return logCohortMove(tx, org, company, actor, in.Name, in.Name, "back to by rule: "+whySaid(now.CohortWhy))
	})
}

// PlaceCompanies places every company in the organisation again, and says how many
// moved: the backstop for what changes with time, such as a subscription ending.
func (s *Store) PlaceCompanies(org int64) (int, error) {
	var moved int
	err := s.db.Transaction(func(tx *gorm.DB) error {
		done, err := placeAll(tx, org, today())
		moved = len(done.Moves)
		return err
	})
	return moved, err
}

// placeEverywhere places again every company of every organisation with cohorts.
func (s *Store) placeEverywhere() {
	var orgs []int64
	if logged(s.db.Model(&Cohort{}).Distinct().Pluck("org_id", &orgs)) != nil {
		return
	}
	for _, org := range orgs {
		if _, err := s.PlaceCompanies(org); err != nil {
			log.Printf("placing organisation %d's companies in cohorts: %v", org, err)
		}
	}
}

// cohortGuidance tells research agents what each cohort is, so the segments they write
// lead with what a company is and the cohorts' rules can read them.
func cohortGuidance(o *Org, cohorts []Cohort) string {
	if len(cohorts) == 0 {
		return ""
	}
	lines := []string{o.Name + " sorts companies into cohorts. Start each company's segment with what the company is, " +
		"before whom it serves: \"CGI studio for carmakers\", not \"carmakers' CGI\". The cohorts:"}
	for _, c := range cohorts {
		lines = append(lines, "- "+c.Name+prefixed(": ", c.WhoTheyAre))
	}
	return strings.Join(lines, "\n")
}

// cohortsFor is what agents are told of the organisation's cohorts; none if they can't be read.
func (a *App) cohortsFor(org int64) []Cohort {
	cohorts, err := loadCohorts(a.s.db, org)
	if err != nil {
		log.Printf("reading organisation %d's cohorts for its agents: %v", org, err)
	}
	return cohorts
}

// dropOffering takes a removed offering out of the rules that name it, and a rule left
// naming nothing with it.
func dropOffering(tx *gorm.DB, org, offering int64) error {
	var rules []CohortRule
	if err := tx.Where("org_id = ? and field = ?", org, "offering").Find(&rules).Error; err != nil {
		return err
	}
	gone := strconv.FormatInt(offering, 10)
	for _, r := range rules {
		values := splitList(r.Value)
		left := slices.DeleteFunc(slices.Clone(values), func(v string) bool { return v == gone })
		switch {
		case len(left) == len(values):
			continue
		case len(left) == 0:
			if err := tx.Delete(&r).Error; err != nil {
				return err
			}
		default:
			if err := tx.Model(&r).Update("value", strings.Join(left, ", ")).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
