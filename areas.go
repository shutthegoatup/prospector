package main

import (
	"context"
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
// alone. Putting an enquiry's sender on the board hands them from support to sales, and
// starting a won customer's onboarding hands them back, so either may; "" is anyone, for
// the owners-only operations, which are held already.
var opArea = map[string]string{
	"ChangeEnquiry": areaSupport, "ReplyToEnquiry": areaSupport, "RecordEnquiryAnswer": areaSupport,
	"AddSubscription": areaSupport, "ChangeSubscription": areaSupport, "RemoveSubscription": areaSupport, "StartRenewal": areaSupport,
	"StartOnboarding": areaAny, "RemoveOnboarding": areaSupport, "ChangeOnboardingStep": areaSupport,
	"InviteCustomer": areaSupport, "WithdrawCustomerInvite": areaSupport,
	"StartDeal": areaSales, "ChangeDeal": areaSales, "RemoveDeal": areaSales, "ChangeOwnDeal": areaSales,
	"StartRun": areaSales, "StopRun": areaSales,
	"CreateTask": areaSales, "ChangeTask": areaSales, "AskTask": areaSales, "ChangeTaskRequest": areaSales,
	"AddNote": areaSales, "FinishRequest": areaSales,
	"AddPerson": areaSales, "ChangeContact": areaSales, "PickContact": areaSales, "ReviewCompany": areaSales, "DecideSuggestion": areaSales,
	"ConverseEnquiry": areaAny, "AddSuppression": areaAny, "RemoveSuppression": areaSales,
	"CreateCampaign": areaSales, "ChangeCampaign": areaSales, "RemoveCampaign": areaSales,
	"SendCampaign": areaSales, "CancelCampaign": areaSales, "ResumeCampaign": areaSales,
	"CreateAgreement": areaSales, "ChangeAgreement": areaSales, "RemoveAgreement": areaSales, "PutAgreementDocument": areaSales,
	"AddAgreementRecipient": areaSales, "ChangeAgreementRecipient": areaSales, "RemoveAgreementRecipient": areaSales, "PutAgreementFields": areaSales,
	"CreateAgent": "", "UpdateAgent": "", "DeleteAgent": "", "DeleteEnquiry": "", "ChangeSettings": "",
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

// canDo says whether whoever a page is rendered for may change an area's work, so the
// page offers only what they can use. The routes hold it either way.
func canDo(ctx context.Context, area string) bool {
	q, _ := ctx.Value(reqKey{}).(*Req)
	return q != nil && q.Member.Works(area)
}

// pageArea is the area whose work a page in the sidebar is.
var pageArea = map[string]string{
	"today": areaSales, "research": areaSales, "conversations": areaSales, "campaigns": areaSales, "tasks": areaSales,
	"enquiries": areaSupport, "onboarding": areaSupport, "subscriptions": areaSupport,
}

// landing is the page someone starts on: Today is outreach, so someone who works only
// in support starts on what is theirs to answer.
func landing(m *Membership) string {
	if !m.Works(areaSales) && m.Works(areaSupport) {
		return "enquiries"
	}
	return "today"
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
