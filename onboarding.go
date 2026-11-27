package main

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Onboarding is one customer being set up: a copy of the organisation's steps, made
// the day it starts, so changing the steps in Settings later leaves it as it was.
// It belongs to a company, and a company can have several (a site or a product each).
type Onboarding struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	OrgID     int64     `gorm:"index:idx_onboardings_company,priority:1;not null" json:"-"`
	CompanyID int64     `gorm:"index:idx_onboardings_company,priority:2;not null" json:"company_id"`
	Name      string    `gorm:"not null;default:''" json:"name"`
	Start     string    `gorm:"not null;default:''" json:"start"`
	CreatedAt time.Time `json:"created_at"`
}

// OnboardingStep is one thing to be done before the customer is live.
type OnboardingStep struct {
	ID           int64  `gorm:"primaryKey" json:"id"`
	OrgID        int64  `gorm:"index;not null" json:"-"`
	OnboardingID int64  `gorm:"uniqueIndex:idx_onboarding_step,priority:1;not null" json:"onboarding_id"`
	Seq          int    `gorm:"uniqueIndex:idx_onboarding_step,priority:2;not null;default:0" json:"seq"`
	Name         string `gorm:"not null;default:''" json:"name"`
	// Who has to do it: the customer ("them") or this organisation ("us").
	Who    string  `gorm:"not null;default:'us'" json:"who"`
	Due    *string `json:"due"`
	DoneOn *string `json:"done_on"`
	DoneBy string  `gorm:"not null;default:''" json:"done_by"`
	// DoneEntry is the history entry a customer's mark wrote, which taking it back removes.
	DoneEntry int64 `gorm:"not null;default:0" json:"done_entry"`
}

const (
	StepTheirs = "them"
	StepOurs   = "us"
)

// StepPlan is one line of the organisation's onboarding steps.
type StepPlan struct {
	Name, Who string
	// Days after the start it is due; -1 when the line gives none.
	Days int
}

var stepDays = regexp.MustCompile(`(?i),\s*(\d+)\s*days?\s*$`)

const maxOnboardingSteps = 40

// Onboarding reads the steps an organisation set, one a line: "them: Send your
// product files, 7 days". Who is "them" or "us" (us when left out) and the days,
// counted from the start, can be left out too.
func (s OrgSettings) Onboarding() ([]StepPlan, error) {
	var out []StepPlan
	for _, line := range strings.Split(s.OnboardingSteps, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		p := StepPlan{Who: StepOurs, Days: -1}
		if m := stepDays.FindStringSubmatch(line); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil || n > 730 {
				return nil, fmt.Errorf("%q: a step is due within 730 days of the start", line)
			}
			p.Days, line = n, strings.TrimSpace(line[:len(line)-len(m[0])])
		}
		if who, rest, found := strings.Cut(line, ":"); found {
			switch strings.ToLower(strings.TrimSpace(who)) {
			case StepTheirs:
				p.Who, line = StepTheirs, strings.TrimSpace(rest)
			case StepOurs:
				line = strings.TrimSpace(rest)
			}
		}
		if line == "" {
			return nil, errors.New("every step needs a name: one line has only who does it or when it is due")
		}
		p.Name = clip(line, 200)
		out = append(out, p)
	}
	if len(out) > maxOnboardingSteps {
		return nil, fmt.Errorf("that is %d steps; %d is the most", len(out), maxOnboardingSteps)
	}
	return out, nil
}

// OnboardingIn is what someone gives to start one.
type OnboardingIn struct {
	Company int64  `json:"company"`
	Name    string `json:"name"`
	Start   string `json:"start"`
}

// StartOnboarding copies the organisation's steps for a company, dated from start.
func (s *Store) StartOnboarding(org int64, in OnboardingIn, actor string) (*Onboarding, error) {
	start, err := parseDay(in.Start)
	if err != nil {
		return nil, err
	}
	var ob Onboarding
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var c Company
		if err := tx.Where("org_id = ? and id = ?", org, in.Company).First(&c).Error; err != nil {
			return missing(err, "no company %d", in.Company)
		}
		var o Org
		if err := tx.First(&o, org).Error; err != nil {
			return err
		}
		plan, err := o.Settings.Onboarding()
		if err != nil {
			return err
		}
		if len(plan) == 0 {
			return errors.New("write your onboarding steps in Settings first")
		}
		ob = Onboarding{OrgID: org, CompanyID: c.ID, Name: clip(strings.TrimSpace(in.Name), 200), Start: in.Start}
		if err := tx.Create(&ob).Error; err != nil {
			return err
		}
		steps := make([]OnboardingStep, len(plan))
		for i, p := range plan {
			steps[i] = OnboardingStep{OrgID: org, OnboardingID: ob.ID, Seq: i + 1, Name: p.Name, Who: p.Who}
			if p.Days >= 0 {
				steps[i].Due = str(start.AddDate(0, 0, p.Days).Format(dayLayout))
			}
		}
		if err := tx.Create(&steps).Error; err != nil {
			return err
		}
		return s.log(tx, org, 0, &c.ID, actor, "onboard:start", &in.Start)
	})
	if err != nil {
		return nil, err
	}
	return &ob, nil
}

func (s *Store) OnboardingStepByID(org, id int64) (*OnboardingStep, error) {
	var st OnboardingStep
	if err := s.in(org).Where("id = ?", id).First(&st).Error; err != nil {
		return nil, missing(err, "no onboarding step %d", id)
	}
	return &st, nil
}

// SetOnboardingStep marks a step done or not, or moves the day it is due. The steps
// are the organisation's own wording, so the history names the one that was done.
func (s *Store) SetOnboardingStep(org, id int64, field, value, actor string) error {
	value = strings.TrimSpace(value)
	return s.db.Transaction(func(tx *gorm.DB) error {
		var st OnboardingStep
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&st).Error; err != nil {
			return missing(err, "no onboarding step %d", id)
		}
		var ob Onboarding
		if err := tx.Where("org_id = ? and id = ?", org, st.OnboardingID).First(&ob).Error; err != nil {
			return missing(err, "no onboarding %d", st.OnboardingID)
		}
		updates := map[string]any{}
		action, logged := "onboard:"+field, st.Name
		switch field {
		case "done":
			switch {
			case value == "":
				updates["done_on"], updates["done_by"], updates["done_entry"], action = nil, "", 0, "onboard:undone"
			case value != "yes":
				return fmt.Errorf("a step is done (yes) or not (empty), not %q", value)
			case st.DoneOn != nil:
				// Someone on a page that hadn't caught up: who did it, and when, stay as they were.
				return nil
			default:
				updates["done_on"], updates["done_by"] = today(), actor
			}
		case "due":
			if value != "" {
				if _, err := parseDay(value); err != nil {
					return err
				}
			}
			updates["due"], logged = nonEmpty(value), st.Name+": "+value
			if value == "" {
				logged = st.Name + ": no date"
			}
		default:
			return fmt.Errorf("a step has no %q to set", field)
		}
		if err := tx.Model(&st).Updates(updates).Error; err != nil {
			return err
		}
		return s.log(tx, org, 0, &ob.CompanyID, actor, action, nonEmpty(logged))
	})
}

// RemoveOnboarding takes back one started by mistake, with its steps.
func (s *Store) RemoveOnboarding(org, id int64, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var ob Onboarding
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&ob).Error; err != nil {
			return missing(err, "no onboarding %d", id)
		}
		if err := tx.Where("org_id = ? and onboarding_id = ?", org, id).Delete(&OnboardingStep{}).Error; err != nil {
			return err
		}
		if err := dropCustomerGrants(tx, org, tx.Where("org_id = ? and onboarding_id = ?", org, id), false); err != nil {
			return err
		}
		if err := tx.Delete(&ob).Error; err != nil {
			return err
		}
		return s.log(tx, org, 0, &ob.CompanyID, actor, "onboard:remove", nil)
	})
}

// OnboardingRow is an onboarding with its company's name, its steps and how far it has got.
type OnboardingRow struct {
	Onboarding
	Company string
	Steps   []OnboardingStep
	Done    int
	// Late are the steps due before day and not done; DueToday those due on it.
	Late, DueToday int
	// Next is the first step not done, nil once all are.
	Next *OnboardingStep
	// LiveOn is the day the last step was done, once every step is.
	LiveOn string
	// Invites are the people let see it, Invitable those at the company who could be, and
	// MailOn whether a link can be sent at all. The pages fill these in; see customerControls.
	Invites   []CustomerInvite
	Invitable []Contact
	MailOn    bool
}

func (r OnboardingRow) Live() bool { return r.LiveOn != "" }

// Onboardings lists an organisation's with the ones running late first, then the rest
// still running, then those that are done; company narrows it to one company's.
func (s *Store) Onboardings(org, company int64, day string) ([]OnboardingRow, error) {
	q := s.in(org)
	if company != 0 {
		q = q.Where("company_id = ?", company)
	}
	var obs []Onboarding
	if err := q.Order("start desc, id desc").Find(&obs).Error; err != nil {
		return nil, err
	}
	if len(obs) == 0 {
		return nil, nil
	}
	ids, companyIDs := make([]int64, len(obs)), make([]int64, len(obs))
	for i, ob := range obs {
		ids[i], companyIDs[i] = ob.ID, ob.CompanyID
	}
	var steps []OnboardingStep
	if err := s.in(org).Where("onboarding_id in ?", ids).Order("onboarding_id, seq").Find(&steps).Error; err != nil {
		return nil, err
	}
	var companies []Company
	if err := s.in(org).Where("id in ?", companyIDs).Find(&companies).Error; err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(companies))
	for _, c := range companies {
		names[c.ID] = c.Name
	}
	byOnboarding := map[int64][]OnboardingStep{}
	for _, st := range steps {
		byOnboarding[st.OnboardingID] = append(byOnboarding[st.OnboardingID], st)
	}
	rows := make([]OnboardingRow, len(obs))
	for i, ob := range obs {
		r := OnboardingRow{Onboarding: ob, Company: names[ob.CompanyID], Steps: byOnboarding[ob.ID]}
		last := ""
		for j := range r.Steps {
			st := &r.Steps[j]
			switch {
			case st.DoneOn != nil:
				r.Done++
				last = max(last, *st.DoneOn)
			case r.Next == nil:
				r.Next = st
			}
			if st.DoneOn == nil && st.Due != nil {
				if *st.Due < day {
					r.Late++
				} else if *st.Due == day {
					r.DueToday++
				}
			}
		}
		if len(r.Steps) > 0 && r.Done == len(r.Steps) {
			r.LiveOn = last
		}
		rows[i] = r
	}
	rank := func(r OnboardingRow) int {
		switch {
		case r.Live():
			return 2
		case r.Late > 0:
			return 0
		}
		return 1
	}
	sort.SliceStable(rows, func(i, j int) bool { return rank(rows[i]) < rank(rows[j]) })
	return rows, nil
}
