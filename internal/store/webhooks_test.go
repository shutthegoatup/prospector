package store

import (
	"slices"
	"testing"
)

const (
	aboutUs      = "about the organisation's own set-up, not its records: kept out of what its integrations are sent"
	agentsOwn    = "the agents' own working, which the agent activity API reads"
	stillToCome  = "not published yet: add an event type when an integration needs it"
	exportsNever = "exporting and erasing say nothing to an integration, which could only learn what was asked about whom"
)

// notPublished are the actions no event is sent for, and why. An action is in one or
// the other, so a new one has to be thought about.
var notPublished = map[string]string{
	ActExport.String(): exportsNever, ActExportOrg.String(): exportsNever, ActErase.String(): exportsNever, ActImport.String(): stillToCome,
	ActMemberAdd.String(): aboutUs, ActMemberJoin.String(): aboutUs, ActMemberRole.String(): aboutUs, ActMemberRemove.String(): aboutUs,
	ActSettings.String(): aboutUs, ActSSO.String(): aboutUs, ActSSOAddDomain.String(): aboutUs, ActSSOVerify.String(): aboutUs,
	ActSSORemove.String(): aboutUs, ActKeyNew.String(): aboutUs, ActKeyScopes.String(): aboutUs, ActKeyRevoke.String(): aboutUs,
	ActAgentNew.String(): aboutUs, ActAgentChange.String(): aboutUs, ActAgentRemove.String(): aboutUs, ActWebhookNew.String(): aboutUs,
	ActWebhookDrop.String(): aboutUs, ActEndpointNew.String(): aboutUs, ActEndpointSet.String(): aboutUs, ActEndpointDrop.String(): aboutUs,
	ActEndpointKey.String(): aboutUs, ActSMTPChange.String(): aboutUs, ActSMTPRemove.String(): aboutUs, ActEnricherSave.String(): aboutUs,
	ActEnricherDrop.String(): aboutUs, ActOfferingAdded.String(): aboutUs, ActOfferingChanged.String(): aboutUs, ActOfferingRemoved.String(): aboutUs,
	ActPick.String(): stillToCome, ActAddFacts.String(): stillToCome, ActAddFact.String(): stillToCome, ActAddRole.String(): stillToCome,
	ActTaskMatch.String(): agentsOwn, ActTaskAsk.String(): agentsOwn, ActTaskPickup.String(): agentsOwn, ActTaskReopen.String(): stillToCome,
	ActTaskClose.String(): stillToCome, ActTaskOpen.String(): stillToCome, ActTaskHeld.String(): agentsOwn, ActTaskWithheld.String(): agentsOwn,
	ActNoteRemove.String(): stillToCome, ActOnboardRemove.String(): stillToCome, ActOnboardInvite.String(): stillToCome,
	ActOnboardRevoke.String(): stillToCome, ActOnboardUndone.String(): stillToCome,
	ActAgreementRemove.String(): stillToCome, ActSuppressionRemove.String(): stillToCome, ActCampaignAdd.String(): stillToCome,
	ActCampaignRemove.String(): stillToCome, ActCampaignCancel.String(): stillToCome, ActCampaignResume.String(): stillToCome,
}

// notPublishedFamilies are the families no event is sent for, and why.
var notPublishedFamilies = map[Family]string{
	OnboardField: stillToCome, VerdictTo: stillToCome, RoleTo: stillToCome, Suggested: agentsOwn, Dismissed: agentsOwn,
	Enriched: stillToCome, Noted: agentsOwn, Drafted: agentsOwn,
}

func TestEveryActionIsPublishedOrSaysWhyNot(t *testing.T) {
	t.Parallel()
	for _, a := range Actions() {
		_, left := notPublished[a.String()]
		switch published := EventTypeOf(a.String()) != ""; {
		case !published && !left:
			t.Errorf("%s is neither an event type's nor in notPublished with why", a)
		case published && left:
			t.Errorf("%s is published, but notPublished says it isn't", a)
		}
	}
	for _, f := range []Family{SetField, Marked, DealField, SubField, EnquiryField, OnboardField, CompanyTo, VerdictTo, RoleTo, Suggested, Dismissed, Enriched, Noted, Drafted} {
		_, left := notPublishedFamilies[f]
		if published := EventTypeOf(f.Of("x").String()) != ""; published == left {
			t.Errorf("the %s family: published %v, and in notPublishedFamilies %v", f.prefix, published, left)
		}
	}
}

func TestAnActionByNameComesBeforeItsFamily(t *testing.T) {
	t.Parallel()
	for action, want := range map[string]string{
		ActDealStart.String(): "deal.started", ActDealDrop.String(): "deal.removed", DealField.Of("stage").String(): "deal.changed",
		ActSubNew.String(): "subscription.added", SubField.Of("amount").String(): "subscription.changed",
		ActEnquiry.String(): "enquiry.received", EnquiryField.Of("owner").String(): "enquiry.changed",
	} {
		if got := EventTypeOf(action); got != want {
			t.Errorf("%s is sent as %q, want %q", action, got, want)
		}
	}
	var names []string
	for _, e := range EventTypes {
		if slices.Contains(names, e.Name) {
			t.Errorf("%s is listed twice", e.Name)
		}
		names = append(names, e.Name)
	}
}
