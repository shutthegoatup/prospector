package app

import (
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/marketing"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/store"
	"github.com/shutthegoatup/prospector/internal/tickets"
)

const (
	drafting = "a draft being written; once sent, its own record (agreement_events, or the campaign's recipients) holds it"
	queue    = "the sending queue: each recipient's row is its record"
	asWas    = "puts rows back as they were, their history with them"
)

// leavesNoHistory are the methods that change an organisation's rows without an event.
var leavesNoHistory = map[string]string{
	"SaveVoice": "a member's own preferences", "SetAreas": "a member's own preferences",
	"ChangeAgreement": drafting, "PutAgreementFields": drafting, "PutAgreementFile": drafting, "AddRecipient": drafting, "ChangeRecipient": drafting,
	"RemoveRecipient": drafting, "ChangeCampaign": drafting,
	"TrySeal":      "a trial seal of a draft, which keeps nothing",
	"RenameKey":    "follows an agent's rename, which SaveAgentRow logs",
	"AddEnquiry":   "logged on its company; spam, or one from no company held here, is a row in Enquiries with nothing to hang an entry on",
	"MailFailed":   "a bounce or complaint is kept on the suppression list and the mail it came back from",
	"AddRun":       "an agent's run is shown with its agent; what it does is logged as it does it",
	"CampaignWent": queue, "CampaignFailed": queue, "SettleMail": queue, "ClaimMail": queue, "SkipMail": queue, "FinishIfDone": queue,
	"PauseCampaign":      "the server pauses a campaign whose mail keeps failing, and the campaign says why",
	"StartReply":         "a reply waiting to go; SettleReply logs it once sent",
	"SettleReply":        "one that failed to send is forgotten, as if never written",
	"Spend":              "a vendor's monthly count of lookups",
	"RedeemCustomerLink": "a customer signing in from their link",
	"SaveOrg":            "only tests call it; ChangeSettings is how settings change, and it logs",
	"ReplaceContacts":    asWas, "Restore": asWas, "ImportLegacy": asWas,
}

const (
	notInState = "needs a run or a claim in a state the seeded rows aren't in; their own tests show what happens"
	logsByHand = "logs, as read by hand; the fuzzing's words don't make a call it accepts"
)

// untried are the methods named for a change that the fuzzing never gets to make one.
// The list should only shrink.
var untried = map[string]string{
	"AskRunStop": notInState, "Claim": notInState, "ClaimNext": notInState, "DropKeyClaims": notInState, "EndRun": notInState,
	"HoldRun": notInState, "RenewRun": notInState, "StopLapsedRun": notInState, "TakeOverRun": notInState,
	"AddWebhook": logsByHand, "ChangeWebhook": logsByHand,
	"AddCampaign": logsByHand, "AddMember": logsByHand, "AddRole": logsByHand, "AddSubscription": logsByHand,
	"AgentAddContact": logsByHand, "AgentAddFact": logsByHand, "AgentAddRole": logsByHand, "AgentNote": logsByHand,
	"ChangeContactFields": logsByHand, "ChangeDeal": logsByHand, "ChangeDealFields": logsByHand, "ChangeOffering": logsByHand,
	"ChangeSettings": logsByHand, "ChangeSubscription": logsByHand, "ChangeSubscriptionFields": logsByHand, "CreateKey": logsByHand,
	"InviteCustomer": logsByHand, "PickContactFor": logsByHand, "RemoveCampaign": logsByHand,
	"RemoveDeal": logsByHand, "RemoveMember": logsByHand, "RemoveSSODomain": logsByHand, "ResumeCampaign": logsByHand,
	"SaveEnricher": logsByHand, "SetDealFields": logsByHand, "SetEnquiryFields": logsByHand, "SetKeyScopes": logsByHand,
	"SetOnboardingStep": logsByHand, "SetRole": logsByHand, "StartDeal": logsByHand, "Unsuppress": logsByHand,
	"SendCampaign": logsByHand, "CancelCampaign": logsByHand,
}

// changesByName is what a method that changes things is named for, so one the fuzzing
// never gets to write shows instead of passing for want of trying.
var changesByName = regexp.MustCompile(`^(Add|Agent(Add|Draft|Finish|Mark|Note)|Ask|Cancel|Change|Claim|Converse|Create|Decide|Delete|Drop|End|Erase|Finish|Hear|Hold|Import|Invite|Judge|Keep|Mark|New|Pause|Pick|Put|Queue|Quick|Redeem|Remove|Rename|Renew|Replace|Restore|Resume|Revoke|Save|Send|Set|Settle|Skip|Spend|Start|Stop|Suppress|Take|Try|Unsuppress|Verify)([A-Z]|$)`)

// TestEveryChangeGoesInTheHistory calls each of the store's methods every way the
// isolation fuzzing does, as the organisation that owns the rows, and fails on a call
// that changed them without adding to the history.
func TestEveryChangeGoesInTheHistory(t *testing.T) {
	t.Parallel()
	methods := orgMethods(t)
	for name := range merged(leavesNoHistory, untried) {
		if !slices.ContainsFunc(methods, func(m reflect.Method) bool { return m.Name == name }) {
			t.Errorf("%s is listed in history_complete_test.go but the store has no such method", name)
		}
	}
	for i := range isolationShares {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			s := tempStore(t)
			first := seedFirst(t, s)
			before := holdings(t, s, first.ID)
			lastEvent := latestEvent(s, first.ID)
			for _, m := range share(methods, i) {
				untold, changed := tellsTheHistory(t, s, m, first, before, lastEvent)
				_, exempt := leavesNoHistory[m.Name]
				_, listed := untried[m.Name]
				switch {
				case len(untold) > 0 && !exempt:
					t.Errorf("Store.%s changed %s and said nothing in the history: log it, or name it in leavesNoHistory with why", m.Name, strings.Join(untold, ", "))
				case !changed && !listed && !exempt && changesByName.MatchString(m.Name):
					t.Errorf("Store.%s changed nothing however it was called, so whether it logs is unknown: give the fuzzing what it needs, or name it in untried with why", m.Name)
				case changed && listed:
					t.Errorf("Store.%s is tried now: take it out of untried", m.Name)
				}
			}
		})
	}
}

// tellsTheHistory calls m every way, each undone, and says which tables a call changed
// with no event to show for it, and whether any call changed anything.
func tellsTheHistory(t *testing.T, s *Store, m reflect.Method, o *store.Org, before map[string]string, lastEvent int64) (untold []string, changed bool) {
	t.Helper()
	org := o.ID
	everyWay(s, true, m, o, func(_ string, _ bool, on *Store) bool {
		// Whether the call wrote anything, then whether it logged, is far cheaper to ask
		// than every row, and settles most calls.
		var wrote bool
		if on.DB.Raw("select txid_current_if_assigned() is not null").Scan(&wrote).Error != nil || !wrote {
			return false
		}
		if latestEvent(on, org) > lastEvent {
			changed = true
			return false
		}
		after, err := holdingsOn(t, on, org)
		if err != nil {
			return false
		}
		for _, table := range slices.Sorted(maps.Keys(merged(before, after))) {
			if before[table] != after[table] {
				changed = true
				if table != "events" {
					untold = append(untold, table)
				}
			}
		}
		return len(untold) > 0
	})
	return untold, changed
}

// The changes to who and what can reach an organisation are in its history, saying who
// made them, though the fuzzing above can't make most of them.
func TestAccessChangesGoInTheHistory(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	const who = "Owner One"
	_, k := must2(s.CreateKey(o.ID, "scout", []string{"read"}, who))
	do := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	do(s.SetKeyScopes(o.ID, k.ID, []string{"read"}, who))
	do(s.SetKeyScopes(o.ID, k.ID, []string{"read", "research:write"}, who))
	do(s.RevokeKey(o.ID, k.ID, who))
	do(s.RevokeKey(o.ID, k.ID, who))
	e := must(s.SaveEnricher(o.ID, prospecting.EnricherIn{Kind: "hunter", APIKey: "hunter-key"}, who))
	do(s.DeleteEnricher(o.ID, e.ID, who))
	d := must(s.AddSSODomain(o.ID, "alpha.example", who))
	do(s.VerifySSODomain(o.ID, d.ID, time.Now(), who))
	do(s.RemoveSSODomain(o.ID, d.Domain, who))
	do(s.RemoveSSODomain(o.ID, d.Domain, who))
	secret, err := s.NewBounceHook(o.ID, who)
	do(err)
	if secret == "" {
		t.Fatal("no webhook secret")
	}
	do(s.DropBounceHook(o.ID, who))
	do(s.SaveOrgMail(o.ID, &store.OrgMail{Host: "smtp.alpha.example", Port: 587}, who))
	do(s.DeleteOrgMail(o.ID, who))
	u := must(s.CreateUser("bo@alpha.example", "Bo Member", "a long enough password"))
	do(s.AddMember(o.ID, u.ID, "member", "", who))
	do(s.SetRole(o.ID, u.ID, "owner", who))
	do(s.SetRole(o.ID, u.ID, "owner", who))
	row := must(s.Suppress(o.ID, "ines@larkmoor.example", who))
	must(s.Suppress(o.ID, "ines@larkmoor.example", who))
	do(s.Unsuppress(o.ID, row.ID, who))
	c := must(s.AddCampaign(o.ID, marketing.CampaignIn{Name: "Spring news"}, who))
	do(s.RemoveCampaign(o.ID, c.ID, who))

	var events []store.Event
	do(s.DB.Where("org_id = ?", o.ID).Order("id").Find(&events).Error)
	var got []string
	for _, e := range events {
		if e.User != who {
			t.Errorf("%s was recorded as done by %q", e.Action, e.User)
		}
		got = append(got, e.Action+" "+store.Deref(e.Value))
	}
	// Scopes sent again unchanged, a key revoked twice, a domain removed twice and a role
	// given twice, and an address added twice, say nothing more. The do-not-mail list's entries
	// leave its address out.
	want := []string{"key:new scout: read", "key:scopes scout: read research:write", "key:revoke scout",
		"enricher:save hunter", "enricher:remove hunter", "sso:add-domain alpha.example", "sso:verify-domain alpha.example",
		"sso:remove-domain alpha.example", "webhook:new ", "webhook:remove ", "smtp:change smtp.alpha.example", "smtp:remove ",
		"member:add bo@alpha.example: member", "member:role bo@alpha.example: owner",
		"suppression:add ", "suppression:remove added", "campaign:add Spring news", "campaign:remove Spring news"}
	if !slices.Equal(got, want) {
		t.Errorf("history:\n got %q\nwant %q", got, want)
	}
}

// A company's history says when an agreement was started for it or a draft removed,
// and when an enquiry from it was removed.
func TestAgreementsAndEnquiriesGoInTheCompanysHistory(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	c := store.Company{OrgID: o.ID, Name: "Larkmoor", Domain: "larkmoor.example"}
	if err := s.DB.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	a := must(s.AddAgreement(o.ID, c.ID, "Pilot terms", "Owner One"))
	if err := s.RemoveAgreement(o.ID, a.ID, "Owner One"); err != nil {
		t.Fatal(err)
	}
	q := must(s.AddEnquiry(o.ID, "form", tickets.EnquiryIn{Name: "Ines Varga", Email: "ines@larkmoor.example", Message: "Do you do pilots?"}))
	if err := s.DeleteEnquiry(o.ID, q.ID, "Owner One"); err != nil {
		t.Fatal(err)
	}
	items := must(s.HistoryAt(o.ID, c.ID, 0, 10))
	var lines []string
	for _, h := range items {
		lines = append(lines, crm.HistoryText(h).Error())
	}
	want := []string{"removed an enquiry", "sent an enquiry: Other", "removed a draft agreement: Pilot terms", "started an agreement: Pilot terms"}
	if !slices.Equal(lines, want) {
		t.Errorf("history:\n got %q\nwant %q", lines, want)
	}
}

// What happens to a campaign once it is sent is in the history, and who did it; the
// server pausing it is said on the campaign instead.
func TestCampaignsGoInTheHistory(t *testing.T) {
	t.Parallel()
	_, s, o, _, _, _ := campaignSite(t)
	c := draft(t, s, o)
	must(s.SendCampaign(o, c.ID, "Ada Lovelace"))
	if err := s.PauseCampaign(o.ID, c.ID, "the mail server refused"); err != nil {
		t.Fatal(err)
	}
	must(s.ResumeCampaign(o.ID, c.ID, "Ada Lovelace"))
	must(s.CancelCampaign(o.ID, c.ID, "Ada Lovelace"))
	var events []store.Event
	if err := s.DB.Where("org_id = ? and action like 'campaign:%'", o.ID).Order("id").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range events {
		got = append(got, e.User+" "+e.Action+" "+store.Deref(e.Value))
	}
	want := []string{"Ada Lovelace campaign:add Spring price list", "Ada Lovelace campaign:send Spring price list",
		"Ada Lovelace campaign:resume Spring price list", "Ada Lovelace campaign:cancel Spring price list"}
	if !slices.Equal(got, want) {
		t.Errorf("history:\n got %q\nwant %q", got, want)
	}
}

func latestEvent(s *Store, org int64) (id int64) {
	s.DB.Raw("select coalesce(max(id), 0) from events where org_id = ?", org).Scan(&id)
	return id
}

func merged(a, b map[string]string) map[string]string {
	out := maps.Clone(a)
	maps.Copy(out, b)
	return out
}
