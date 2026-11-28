package store

import (
	"strings"
	"time"

	"github.com/shutthegoatup/prospector/internal/say"
)

// WebhookEndpoint is where an organisation's events are posted, as one of its keys may
// read them: none once the key is revoked or loses the read scope.
type WebhookEndpoint struct {
	ID     int64  `gorm:"primaryKey"`
	OrgID  int64  `gorm:"index;not null"`
	KeyID  int64  `gorm:"not null"`
	URL    string `gorm:"not null"`
	Events string `gorm:"not null;default:''"`
	// Secret signs what is sent, sealed. PreviousSecret signs it too until PreviousUntil,
	// so a receiver changes over to a new one without missing a delivery.
	Secret         string `gorm:"not null"`
	PreviousSecret string `gorm:"not null;default:''"`
	PreviousUntil  *time.Time
	// DisabledAt is when sending to it stopped, and DisabledWhy why.
	DisabledAt  *time.Time
	DisabledWhy string    `gorm:"not null;default:''"`
	CreatedBy   string    `gorm:"not null;default:''"`
	CreatedAt   time.Time `gorm:"not null"`
}

// EventList is what an endpoint is sent, by name.
func (e WebhookEndpoint) EventList() []string { return strings.Fields(e.Events) }

// An EventType is what an endpoint can be sent, named as what happened to what. Each
// comes from the actions history records: some by name, some by family.
type EventType struct {
	Name     string
	About    *say.Text
	actions  []Action
	families []Family
}

// EventTypes are every kind of event, in the order they are listed. One that matches an
// action by name is looked at before any that matches it by family.
var EventTypes = []EventType{
	{Name: "company.added", About: say.New("A company was added."), actions: []Action{ActAddCompany}},
	{Name: "company.stage_changed", About: say.New("A company was qualified, rejected or put back for review."), families: []Family{CompanyTo}},
	{Name: "contact.added", About: say.New("A person was added."), actions: []Action{ActAddPerson, ActAddContact}},
	{Name: "contact.changed", About: say.New("Something about a person was changed."), families: []Family{SetField}},
	{Name: "contact.marked", About: say.New("Something was done with a person: emailed, connected, replied, a meeting…"), families: []Family{Marked}},
	{Name: "deal.started", About: say.New("A deal was started."), actions: []Action{ActDealStart}},
	{Name: "deal.removed", About: say.New("A deal was removed."), actions: []Action{ActDealDrop}},
	{Name: "deal.changed", About: say.New("A deal's stage, amount, dates or owner changed."), families: []Family{DealField}},
	{Name: "subscription.added", About: say.New("A subscription was added."), actions: []Action{ActSubNew}},
	{Name: "subscription.removed", About: say.New("A subscription was removed."), actions: []Action{ActSubDrop}},
	{Name: "subscription.changed", About: say.New("A subscription's terms changed."), families: []Family{SubField}},
	{Name: "enquiry.received", About: say.New("An enquiry came in."), actions: []Action{ActEnquiry}},
	{Name: "enquiry.replied", About: say.New("An enquiry was answered."), actions: []Action{ActEnquiryReply}},
	{Name: "enquiry.heard_back", About: say.New("Whoever sent an enquiry wrote back."), actions: []Action{ActEnquiryHeard}},
	{Name: "enquiry.removed", About: say.New("An enquiry was removed."), actions: []Action{ActEnquiryRemove}},
	{Name: "enquiry.changed", About: say.New("An enquiry's state, owner or note changed."), families: []Family{EnquiryField}},
	{Name: "onboarding.started", About: say.New("A customer's onboarding was started."), actions: []Action{ActOnboardStart}},
	{Name: "onboarding.step_done", About: say.New("A step of an onboarding was done."), actions: []Action{ActOnboardDone}},
	{Name: "task.created", About: say.New("A research task was written."), actions: []Action{ActTaskNew}},
	{Name: "task.done", About: say.New("A research task was finished."), actions: []Action{ActTaskDone}},
	{Name: "agreement.added", About: say.New("An agreement was added."), actions: []Action{ActAgreementAdd}},
	{Name: "suppression.added", About: say.New("An address went on the list not to be mailed."), actions: []Action{ActSuppressionAdd}},
	{Name: "campaign.sent", About: say.New("A campaign started sending."), actions: []Action{ActCampaignSend}},
}

// EventTypeOf is the event an action is published as, or "" for one that isn't.
func EventTypeOf(action string) string {
	for _, t := range EventTypes {
		for _, a := range t.actions {
			if a.String() == action {
				return t.Name
			}
		}
	}
	for _, t := range EventTypes {
		for _, f := range t.families {
			if f.Has(action) {
				return t.Name
			}
		}
	}
	return ""
}

// KnownEventType is whether name is one of EventTypes.
func KnownEventType(name string) bool {
	for _, t := range EventTypes {
		if t.Name == name {
			return true
		}
	}
	return false
}
