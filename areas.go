package main

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

const (
	areaSales   = "sales"
	areaSupport = "support"
	areaAny     = "any"
)

// Areas split a team's work. Everyone reads everything; changing something takes
// its area, and an owner has every area.
var Areas = []struct{ Name, What string }{
	{areaSales, "research, outreach, deals and tasks for agents"},
	{areaSupport, "enquiries and their replies, subscriptions and onboarding"},
}

// opArea is the area whose people may make each API write. Keys go by their scopes
// alone. Putting an enquiry's sender on the board hands them from support to sales, so
// either may; "" is anyone, for the owners-only operations, which are held already.
var opArea = map[string]string{
	"ChangeEnquiry": areaSupport, "ReplyToEnquiry": areaSupport, "RecordEnquiryAnswer": areaSupport,
	"AddSubscription": areaSupport, "ChangeSubscription": areaSupport, "RemoveSubscription": areaSupport, "StartRenewal": areaSupport,
	"StartOnboarding": areaSupport, "RemoveOnboarding": areaSupport, "ChangeOnboardingStep": areaSupport,
	"InviteCustomer": areaSupport, "WithdrawCustomerInvite": areaSupport,
	"StartDeal": areaSales, "ChangeDeal": areaSales, "RemoveDeal": areaSales, "ChangeOwnDeal": areaSales,
	"StartRun": areaSales, "StopRun": areaSales,
	"CreateTask": areaSales, "ChangeTask": areaSales, "AskTask": areaSales, "ChangeTaskRequest": areaSales,
	"ConverseEnquiry": areaAny, "AddSuppression": areaAny, "RemoveSuppression": areaSales,
	"CreateCampaign": areaSales, "ChangeCampaign": areaSales, "RemoveCampaign": areaSales,
	"SendCampaign": areaSales, "CancelCampaign": areaSales, "ResumeCampaign": areaSales,
	"CreateAgent": "", "UpdateAgent": "", "DeleteAgent": "", "DeleteEnquiry": "",
}

func (m *Membership) Works(area string) bool {
	if m == nil {
		return false
	}
	if area == areaAny {
		return m.Role == "owner" || strings.TrimSpace(m.Areas) != ""
	}
	return area == "" || m.Role == "owner" || slices.Contains(strings.Fields(m.Areas), area)
}

func areaRefusal(area string) string {
	if area == areaAny {
		return "you read only here: an owner can give you an area on the Team list in Settings"
	}
	return "that's " + area + " work, which you don't do here: an owner can add " + area + " to you on the Team list in Settings"
}

// works holds a page's write to the people of one area.
func (a *App) works(area string, next http.HandlerFunc) http.HandlerFunc {
	return a.member(func(w http.ResponseWriter, r *http.Request) {
		if !reqOf(r).Member.Works(area) {
			http.Error(w, areaRefusal(area), http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

// SetAreas says what someone works on. None leaves them reading only.
func (s *Store) SetAreas(org, user int64, areas []string) error {
	for _, ar := range areas {
		if ar != areaSales && ar != areaSupport {
			return fmt.Errorf("unknown area %q", ar)
		}
	}
	var keep []string
	for _, ar := range Areas {
		if slices.Contains(areas, ar.Name) {
			keep = append(keep, ar.Name)
		}
	}
	res := s.db.Model(&Membership{}).Where("org_id = ? and user_id = ?", org, user).Update("areas", strings.Join(keep, " "))
	if res.Error == nil && res.RowsAffected == 0 {
		return errors.New("that person isn't in this organisation")
	}
	return res.Error
}

func (a *App) uiMemberAreas(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID int64    `json:"user_id"`
		Areas  []string `json:"areas"`
	}
	if decode(w, r, &in) {
		done(w, a.s.SetAreas(reqOf(r).Org.ID, in.UserID, in.Areas))
	}
}
