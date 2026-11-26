package main

import (
	"errors"
	"fmt"
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
	"AddPerson": areaSales, "ChangeContact": areaSales, "MarkOutcome": areaSales, "PickContact": areaSales, "ReviewCompany": areaSales, "AddRole": areaSales, "JudgeRole": areaSales, "DecideSuggestion": areaSales,
	"AddNote": areaSales, "FinishRequest": areaSales, "DeleteNote": areaSales,
	"ConverseEnquiry": areaAny, "AddSuppression": areaAny, "RemoveSuppression": areaSales,
	"CreateCampaign": areaSales, "ChangeCampaign": areaSales, "RemoveCampaign": areaSales,
	"SendCampaign": areaSales, "CancelCampaign": areaSales, "ResumeCampaign": areaSales,
	"CreateAgreement": areaSales, "ChangeAgreement": areaSales, "RemoveAgreement": areaSales, "PutAgreementDocument": areaSales,
	"AddAgreementRecipient": areaSales, "ChangeAgreementRecipient": areaSales, "RemoveAgreementRecipient": areaSales, "PutAgreementFields": areaSales,
	"EnrichContact": areaSales,
	"CreateAgent":   "", "UpdateAgent": "", "DeleteAgent": "", "CreateKey": "", "ChangeKey": "", "RevokeKey": "", "DeleteEnquiry": "", "ChangeSettings": "", "SaveEnricher": "", "DeleteEnricher": "",
	"ExportPerson": "", "ErasePerson": "",
	"AddOffering": "", "ChangeOffering": "", "RemoveOffering": "",
	"AddMember": "", "ChangeMember": "", "RemoveMember": "", "GetMailServer": "", "SaveMailServer": "", "TestMailServer": "",
	"GetSSO": "", "SaveSSO": "", "ListSSODomains": "", "AddSSODomain": "", "RemoveSSODomain": "", "VerifySSODomain": "", "ExportOrg": "",
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
func memberWork(m Membership) string {
	if m.Role == "owner" {
		return "owner"
	}
	if m.Areas == "" {
		return "member · reads only"
	}
	return "member · " + strings.ReplaceAll(m.Areas, " ", ", ")
}
