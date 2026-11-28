package store

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shutthegoatup/prospector/internal/say"
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

const StepTheirs = "them"

const StepOurs = "us"

// StepPlan is one line of the organisation's onboarding steps.
type StepPlan struct {
	Name, Who string
	// Days after the start it is due; -1 when the line gives none.
	Days int
}

var StepDays = regexp.MustCompile(`(?i),\s*(\d+)\s*days?\s*$`)

const MaxOnboardingSteps = 40

// Onboarding reads the steps an organisation set, one a line: "them: Send your
// product files, 7 days". Who is "them" or "us" (us when left out) and the days,
// counted from the start, can be left out too.
func (s OrgSettings) Onboarding() ([]StepPlan, error) {
	var out []StepPlan
	for line := range strings.SplitSeq(s.OnboardingSteps, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		p := StepPlan{Who: StepOurs, Days: -1}
		if m := StepDays.FindStringSubmatch(line); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil || n > 730 {
				return nil, say.Errorf("%q: a step is due within 730 days of the start", line)
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
			return nil, say.New("every step needs a name: one line has only who does it or when it is due")
		}
		p.Name = Clip(line, 200)
		out = append(out, p)
	}
	if len(out) > MaxOnboardingSteps {
		return nil, say.Errorf("that is %d steps; %d is the most", len(out), MaxOnboardingSteps)
	}
	return out, nil
}
