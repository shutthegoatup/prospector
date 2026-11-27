package customers

import (
	"sort"
	"strings"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// OnboardingStore is the onboarding area's queries: a customer's steps to going live.
type OnboardingStore struct{ *store.Core }

// StartOnboarding copies the organisation's steps for a company, dated from start.
func (s *OnboardingStore) StartOnboarding(org int64, in OnboardingIn, actor string) (*store.Onboarding, error) {
	start, err := store.ParseDay(in.Start)
	if err != nil {
		return nil, err
	}
	var ob store.Onboarding
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		var c store.Company
		if err := tx.Where("org_id = ? and id = ?", org, in.Company).First(&c).Error; err != nil {
			return store.Missing(err, "no company %d", in.Company)
		}
		var o store.Org
		if err := tx.First(&o, org).Error; err != nil {
			return err
		}
		plan, err := o.Settings.Onboarding()
		if err != nil {
			return err
		}
		if len(plan) == 0 {
			return say.New("write your onboarding steps in Settings first")
		}
		ob = store.Onboarding{OrgID: org, CompanyID: c.ID, Name: store.Clip(strings.TrimSpace(in.Name), 200), Start: in.Start}
		if err := tx.Create(&ob).Error; err != nil {
			return err
		}
		steps := make([]store.OnboardingStep, len(plan))
		for i, p := range plan {
			steps[i] = store.OnboardingStep{OrgID: org, OnboardingID: ob.ID, Seq: i + 1, Name: p.Name, Who: p.Who}
			if p.Days >= 0 {
				steps[i].Due = store.Str(start.AddDate(0, 0, p.Days).Format(store.DayLayout))
			}
		}
		if err := tx.Create(&steps).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, &c.ID, actor, "onboard:start", &in.Start)
	})
	if err != nil {
		return nil, err
	}
	return &ob, nil
}

func (s *OnboardingStore) OnboardingStepByID(org, id int64) (*store.OnboardingStep, error) {
	var st store.OnboardingStep
	if err := s.InOrg(org).Where("id = ?", id).First(&st).Error; err != nil {
		return nil, store.Missing(err, "no onboarding step %d", id)
	}
	return &st, nil
}

// SetOnboardingStep marks a step done or not, or moves the day it is due. The steps
// are the organisation's own wording, so the history names the one that was done.
func (s *OnboardingStore) SetOnboardingStep(org, id int64, field, value, actor string) error {
	value = strings.TrimSpace(value)
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var st store.OnboardingStep
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&st).Error; err != nil {
			return store.Missing(err, "no onboarding step %d", id)
		}
		var ob store.Onboarding
		if err := tx.Where("org_id = ? and id = ?", org, st.OnboardingID).First(&ob).Error; err != nil {
			return store.Missing(err, "no onboarding %d", st.OnboardingID)
		}
		updates := map[string]any{}
		action, logged := "onboard:"+field, st.Name
		switch field {
		case "done":
			switch {
			case value == "":
				updates["done_on"], updates["done_by"], updates["done_entry"], action = nil, "", 0, "onboard:undone"
			case value != "yes":
				return say.Errorf("a step is done (yes) or not (empty), not %q", value)
			case st.DoneOn != nil:
				// Someone on a page that hadn't caught up: who did it, and when, stay as they were.
				return nil
			default:
				updates["done_on"], updates["done_by"] = store.Today(), actor
			}
		case "due":
			if value != "" {
				if _, err := store.ParseDay(value); err != nil {
					return err
				}
			}
			updates["due"], logged = store.NonEmpty(value), st.Name+": "+value
			if value == "" {
				logged = st.Name + ": no date"
			}
		default:
			return say.Errorf("a step has no %q to set", field)
		}
		if err := tx.Model(&st).Updates(updates).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, &ob.CompanyID, actor, action, store.NonEmpty(logged))
	})
}

// RemoveOnboarding takes back one started by mistake, with its steps.
func (s *OnboardingStore) RemoveOnboarding(org, id int64, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var ob store.Onboarding
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&ob).Error; err != nil {
			return store.Missing(err, "no onboarding %d", id)
		}
		if err := tx.Where("org_id = ? and onboarding_id = ?", org, id).Delete(&store.OnboardingStep{}).Error; err != nil {
			return err
		}
		if err := DropCustomerGrants(tx, org, tx.Where("org_id = ? and onboarding_id = ?", org, id), false); err != nil {
			return err
		}
		if err := tx.Delete(&ob).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, &ob.CompanyID, actor, "onboard:remove", nil)
	})
}

// Onboardings lists an organisation's with the ones running late first, then the rest
// still running, then those that are done; company narrows it to one company's.
func (s *OnboardingStore) Onboardings(org, company int64, day string) ([]OnboardingRow, error) {
	q := s.InOrg(org)
	if company != 0 {
		q = q.Where("company_id = ?", company)
	}
	var obs []store.Onboarding
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
	var steps []store.OnboardingStep
	if err := s.InOrg(org).Where("onboarding_id in ?", ids).Order("onboarding_id, seq").Find(&steps).Error; err != nil {
		return nil, err
	}
	var companies []store.Company
	if err := s.InOrg(org).Where("id in ?", companyIDs).Find(&companies).Error; err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(companies))
	for _, c := range companies {
		names[c.ID] = c.Name
	}
	byOnboarding := map[int64][]store.OnboardingStep{}
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
