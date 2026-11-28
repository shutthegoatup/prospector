package main

import (
	"strings"

	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/say"
)

// opArea is the area whose people may make each API write. Keys go by their scopes
// alone. Putting an enquiry's sender on the board hands them from support to sales, and
// starting a won customer's onboarding hands them back, so either may; "" is anyone, for
// the owners-only operations, which are held already.
var opArea = map[string]string{
	"ChangeEnquiry": identity.AreaSupport, "ReplyToEnquiry": identity.AreaSupport, "RecordEnquiryAnswer": identity.AreaSupport,
	"AddSubscription": identity.AreaSupport, "ChangeSubscription": identity.AreaSupport, "RemoveSubscription": identity.AreaSupport, "StartRenewal": identity.AreaSupport,
	"StartOnboarding": areaAny, "RemoveOnboarding": identity.AreaSupport, "ChangeOnboardingStep": identity.AreaSupport,
	"InviteCustomer": identity.AreaSupport, "WithdrawCustomerInvite": identity.AreaSupport,
	"StartDeal": identity.AreaSales, "ChangeDeal": identity.AreaSales, "RemoveDeal": identity.AreaSales, "ChangeOwnDeal": identity.AreaSales,
	"StartRun": identity.AreaSales, "StopRun": identity.AreaSales,
	"CreateTask": identity.AreaSales, "ChangeTask": identity.AreaSales, "AskTask": identity.AreaSales, "ChangeTaskRequest": identity.AreaSales,
	"AgentAddCompany": identity.AreaSales, "AgentAddFact": identity.AreaSales, "AgentAddContact": identity.AreaSales, "AgentDraft": identity.AreaSales, "AgentAddEnquiry": identity.AreaSupport, "AgentImport": areaAny, "AgentSnapshot": areaAny,
	"AddPerson": identity.AreaSales, "ChangeContact": identity.AreaSales, "MarkOutcome": identity.AreaSales, "PickContact": identity.AreaSales, "ReviewCompany": identity.AreaSales, "AddRole": identity.AreaSales, "JudgeRole": identity.AreaSales, "DecideSuggestion": identity.AreaSales,
	"AddNote": identity.AreaSales, "FinishRequest": identity.AreaSales, "DeleteNote": identity.AreaSales,
	"ConverseEnquiry": areaAny, "AddSuppression": areaAny, "RemoveSuppression": identity.AreaSales,
	"CreateCampaign": identity.AreaSales, "ChangeCampaign": identity.AreaSales, "RemoveCampaign": identity.AreaSales,
	"SendCampaign": identity.AreaSales, "CancelCampaign": identity.AreaSales, "ResumeCampaign": identity.AreaSales,
	"CreateAgreement": identity.AreaSales, "ChangeAgreement": identity.AreaSales, "RemoveAgreement": identity.AreaSales, "PutAgreementDocument": identity.AreaSales,
	"AddAgreementRecipient": identity.AreaSales, "ChangeAgreementRecipient": identity.AreaSales, "RemoveAgreementRecipient": identity.AreaSales, "PutAgreementFields": identity.AreaSales,
	"EnrichContact": identity.AreaSales,
	"CreateAgent":   "", "UpdateAgent": "", "DeleteAgent": "", "CreateKey": "", "ChangeKey": "", "RevokeKey": "", "DeleteEnquiry": "", "ChangeSettings": "", "SaveEnricher": "", "DeleteEnricher": "",
	"ExportPerson": "", "ErasePerson": "",
	"AddOffering": "", "ChangeOffering": "", "RemoveOffering": "",
	"AddMember": "", "ChangeMember": "", "RemoveMember": "", "GetMailServer": "", "SaveMailServer": "", "TestMailServer": "", "MakeBounceHook": "", "DropBounceHook": "",
	"GetSSO": "", "SaveSSO": "", "ListSSODomains": "", "AddSSODomain": "", "RemoveSSODomain": "", "VerifySSODomain": "", "ExportOrg": "",
}

// landing is the page someone starts on: Today is outreach, so someone who works only
// in support starts on what is theirs to answer.
func landing(m *Membership) string {
	if !m.Works(identity.AreaSales) && m.Works(identity.AreaSupport) {
		return "enquiries"
	}
	return "today"
}

func areaRefusal(area string) *say.Text {
	if area == areaAny {
		return say.New("you read only here: an owner can give you an area on the Team list in Settings")
	}
	return say.Errorf("that's %[1]s work, which you don't do here: an owner can add %[1]s to you on the Team list in Settings", area)
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
