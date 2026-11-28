package store

import (
	"slices"
	"strings"
)

// Action is what a history entry says was done. Only this package makes one, so every
// entry written comes from the vocabulary below; what is stored is its name, which
// never changes once released, since readers and older releases match on it.
type Action struct{ name string }

func (a Action) String() string { return a.name }

// Family is actions named by a prefix and a part: "set:" and a field, "mark:" and a step.
type Family struct{ prefix string }

// Of is the family's action for one part.
func (f Family) Of(part string) Action { return Action{f.prefix + part} }

// In says whether action is one of the family's, and which.
func (f Family) In(action string) (part string, ok bool) { return strings.CutPrefix(action, f.prefix) }

func (f Family) Has(action string) bool { return strings.HasPrefix(action, f.prefix) }

// Like matches the family's actions in SQL.
func (f Family) Like() string { return f.prefix + "%" }

var actions []Action

func named(name string) Action {
	a := Action{name}
	actions = append(actions, a)
	return a
}

// Actions is every action with a name of its own.
func Actions() []Action { return slices.Clone(actions) }

var (
	ActPick       = named("pick")
	ActAddPerson  = named("add:person")
	ActAddCompany = named("add:company")
	ActAddFacts   = named("add:facts")
	ActAddFact    = named("add:fact")
	ActAddContact = named("add:contact")
	ActAddRole    = named("add:role")
	ActExport     = named("export")
	ActExportOrg  = named("export:org")
	ActErase      = named("erase")
	ActImport     = named("import")

	ActMemberAdd    = named("member:add")
	ActMemberJoin   = named("member:join")
	ActMemberRole   = named("member:role")
	ActMemberRemove = named("member:remove")
	ActSettings     = named("settings:change")
	ActSSO          = named("sso:change")
	ActSSOAddDomain = named("sso:add-domain")
	ActSSOVerify    = named("sso:verify-domain")
	ActSSORemove    = named("sso:remove-domain")
	ActKeyNew       = named("key:new")
	ActKeyScopes    = named("key:scopes")
	ActKeyRevoke    = named("key:revoke")
	ActAgentNew     = named("agents:new")
	ActAgentChange  = named("agents:change")
	ActAgentRemove  = named("agents:remove")
	ActWebhookNew   = named("webhook:new")
	ActWebhookDrop  = named("webhook:remove")
	ActEndpointNew  = named("endpoint:new")
	ActEndpointSet  = named("endpoint:change")
	ActEndpointDrop = named("endpoint:remove")
	ActEndpointKey  = named("endpoint:secret")
	ActSMTPChange   = named("smtp:change")
	ActSMTPRemove   = named("smtp:remove")
	ActEnricherSave = named("enricher:save")
	ActEnricherDrop = named("enricher:remove")

	ActOfferingAdded   = named("offering:added")
	ActOfferingChanged = named("offering:changed")
	ActOfferingRemoved = named("offering:removed")

	ActTaskNew      = named("task:new")
	ActTaskMatch    = named("task:match")
	ActTaskAsk      = named("task:ask")
	ActTaskPickup   = named("task:pickup")
	ActTaskDone     = named("task:done")
	ActTaskReopen   = named("task:reopen")
	ActTaskClose    = named("task:close")
	ActTaskOpen     = named("task:open")
	ActTaskHeld     = named("task:held")
	ActTaskWithheld = named("task:withheld")
	ActNoteRemove   = named("note:remove")

	ActDealStart = named("deal:start")
	ActDealDrop  = named("deal:remove")
	ActSubNew    = named("sub:new")
	ActSubDrop   = named("sub:remove")

	ActOnboardStart  = named("onboard:start")
	ActOnboardRemove = named("onboard:remove")
	ActOnboardInvite = named("onboard:invite")
	ActOnboardRevoke = named("onboard:revoke")
	ActOnboardDone   = named("onboard:done")
	ActOnboardUndone = named("onboard:undone")

	ActEnquiry       = named("enquiry")
	ActEnquiryReply  = named("enquiry:reply")
	ActEnquiryHeard  = named("enquiry:heard")
	ActEnquiryRemove = named("enquiry:remove")

	ActAgreementAdd    = named("agreement:add")
	ActAgreementRemove = named("agreement:remove")

	ActSuppressionAdd    = named("suppression:add")
	ActSuppressionRemove = named("suppression:remove")
	ActCampaignAdd       = named("campaign:add")
	ActCampaignRemove    = named("campaign:remove")
	ActCampaignSend      = named("campaign:send")
	ActCampaignCancel    = named("campaign:cancel")
	ActCampaignResume    = named("campaign:resume")
)

// The families. Some share a prefix with named actions ("deal:start" beside "deal:"
// and a field), which are read as named first.
var (
	SetField     = Family{"set:"}
	Marked       = Family{"mark:"}
	DealField    = Family{"deal:"}
	SubField     = Family{"sub:"}
	EnquiryField = Family{"enquiry:"}
	OnboardField = Family{"onboard:"}
	CompanyTo    = Family{"company:"}
	VerdictTo    = Family{"verdict:"}
	RoleTo       = Family{"role:"}
	Suggested    = Family{"suggest:"}
	Dismissed    = Family{"dismiss:"}
	Enriched     = Family{"enrich:"}
	Noted        = Family{"note:"}
	Drafted      = Family{"draft:"}
)
