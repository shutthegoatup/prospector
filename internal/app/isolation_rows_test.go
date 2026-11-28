package app

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/internal/marketing"
	"github.com/shutthegoatup/prospector/internal/onboarding"
	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/store"
)

// TestRunsAndMailsStayInTheirOrganisation tries, as another organisation, the methods
// that change a run, a mail or a domain only in a state the fuzzing in
// TestNoMethodReachesIntoAnotherOrganisation can't guess: a run held by a named server,
// a mail still queued. Each is given the first organisation's own ids and values, so
// all that keeps it out is the organisation. The same call as the first then reaches,
// so the first try proved something. Every call starts from the same rows.
func TestRunsAndMailsStayInTheirOrganisation(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	first, second := mustOrg(t, s, "alpha"), mustOrg(t, s, "beta")
	now := time.Now()

	task := must(s.CreateTask(first.ID, "Find them", "a brief", "Owner One"))
	var request store.TaskRequest
	if err := s.DB.Where("task_id = ?", task.ID).First(&request).Error; err != nil {
		t.Fatal(err)
	}
	agent := store.Agent{OrgID: first.ID, Name: "scout"}
	if err := s.DB.Create(&agent).Error; err != nil {
		t.Fatal(err)
	}
	lease := now.Add(-time.Minute).UnixMilli()
	run := must(s.AddRun(first.ID, store.AgentRun{TaskID: task.ID, AgentID: agent.ID, Status: store.RunWorking, Owner: "server-a", LeaseUntil: lease, StartedAt: now}))
	_, key, err := s.CreateKey(first.ID, "scout's key", []string{"read"}, "Owner One")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeKey(first.ID, key.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&request).Update("done_at", now).Error; err != nil {
		t.Fatal(err)
	}
	claim := store.RequestClaim{RequestID: request.ID, OrgID: first.ID, TaskID: task.ID, Holder: fmt.Sprintf("key:%d", key.ID), Actor: "scout", ClaimedAt: now.UnixMilli()}
	domain := must(s.AddSSODomain(first.ID, "alpha.example"))
	campaign := store.Campaign{OrgID: first.ID, Name: "Spring", Status: marketing.CampaignSending}
	if err := s.DB.Create(&campaign).Error; err != nil {
		t.Fatal(err)
	}
	queued := store.CampaignRecipient{OrgID: first.ID, CampaignID: campaign.ID, Address: "ann@alpha.example", Status: marketing.MailQueued}
	sent := store.CampaignRecipient{OrgID: first.ID, CampaignID: campaign.ID, Address: "bob@alpha.example", Status: marketing.MailSent, SentAt: &now}
	note := store.TaskNote{OrgID: first.ID, TaskID: task.ID, CompanyID: 7, Actor: "scout", Kind: "skipped", Text: "not for us"}
	for _, row := range []any{&claim, &queued, &sent, &note} {
		if err := s.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	ann := person(t, s, first, "Ann Alpha", "Alpha Works", "ann@alpha.example", false)
	company := store.Company{ID: *ann.CompanyID}
	if err := s.DB.Model(&company).Update("stage", prospecting.CompanyReview).Error; err != nil {
		t.Fatal(err)
	}
	enquiry := store.Enquiry{OrgID: first.ID, Name: "Ann Alpha", Email: "ann@alpha.example", CompanyID: ann.CompanyID, ContactID: &ann.ID}
	if err := s.DB.Create(&enquiry).Error; err != nil {
		t.Fatal(err)
	}
	reply := store.EnquiryMessage{OrgID: first.ID, EnquiryID: enquiry.ID, Ours: true, Text: "Thanks", By: "Owner One", CreatedAt: now}
	plan := store.Onboarding{OrgID: first.ID, CompanyID: company.ID, Name: "Setup"}
	subscription := store.Subscription{OrgID: first.ID, CompanyID: company.ID, Name: "Licence", Start: "2020-01-01"}
	for _, row := range []any{&reply, &plan, &subscription} {
		if err := s.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	step := store.OnboardingStep{OrgID: first.ID, OnboardingID: plan.ID, Seq: 1, Name: "Send files", Who: store.StepTheirs}
	grant := store.CustomerGrant{OrgID: first.ID, OnboardingID: plan.ID, ContactID: ann.ID}
	for _, row := range []any{&step, &grant} {
		if err := s.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	link := store.RandomToken(16)
	if err := s.DB.Create(&store.CustomerLink{OrgID: first.ID, GrantID: grant.ID, TokenHash: store.HashToken(link), ExpiresAt: now.Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	customer := &onboarding.CustomerAccess{Grant: grant, Org: *first, Onboarding: plan, Contact: *ann}

	answer := func(v any, err error) any {
		if err != nil {
			return nil
		}
		return v
	}
	// changes marks a call whose answer says nothing: what shows is what it changed.
	const changes = "changes"
	tries := []struct {
		name    string
		nothing any
		call    func(s *Store, org int64) any
	}{
		{"TaskOpen", false, func(s *Store, org int64) any { return s.TaskOpen(org, task.ID) == nil }},
		{"RunsOn", int64(0), func(s *Store, org int64) any { return answer(s.RunsOn(org, task.ID, agent.ID)) }},
		{"AgentRunsGoing", 0, func(s *Store, org int64) any { return len(must(s.AgentRunsGoing(org, agent.ID))) }},
		{"KeyRevoked", false, func(s *Store, org int64) any { return answer(s.KeyRevoked(org, key.ID)) }},
		{"RequestDone", false, func(s *Store, org int64) any { return answer(s.RequestDone(org, request.ID)) }},
		{"SSODomain", false, func(s *Store, org int64) any { return answer(s.SSODomain(org, domain.Domain)) != nil }},
		{"MailSentSince", int64(0), func(s *Store, org int64) any { return answer(s.MailSentSince(org, now.Add(-time.Hour))) }},
		{"NextQueued", false, func(s *Store, org int64) any {
			r, err := s.NextQueued(org, campaign.ID)
			return err == nil && r != nil
		}},
		{"CampaignFailed", nil, func(s *Store, org int64) any { return answer(s.CampaignFailed(org, campaign.ID)) }},
		{"AskRunStop", nil, func(s *Store, org int64) any { return answer(s.AskRunStop(org, run.ID)) }},
		{"RenewRun", false, func(s *Store, org int64) any { return answer(s.RenewRun(org, run.ID, run.Owner, now.Add(time.Hour))) }},
		{"HoldRun", false, func(s *Store, org int64) any {
			return answer(s.HoldRun(org, *run, store.RunWaiting, nil, now.Add(time.Hour)))
		}},
		{"TakeOverRun", false, func(s *Store, org int64) any {
			return answer(s.TakeOverRun(org, run.ID, lease, "server-b", now.Add(time.Hour), 1))
		}},
		{"EndRun", changes, func(s *Store, org int64) any { return s.EndRun(org, *run, RunFailed, "ended", now) }},
		{"StopLapsedRun", changes, func(s *Store, org int64) any { return s.StopLapsedRun(org, run.ID, lease, now) }},
		{"DropKeyClaims", changes, func(s *Store, org int64) any { return s.DropKeyClaims(org, task.ID, key.ID) }},
		{"VerifySSODomain", changes, func(s *Store, org int64) any { return s.VerifySSODomain(org, domain.ID, now) }},
		{"RemoveSSODomain", changes, func(s *Store, org int64) any { return s.RemoveSSODomain(org, domain.Domain) }},
		{"CompanyNotes", int64(0), func(s *Store, org int64) any {
			_, total, err := s.CompanyNotes(org, note.CompanyID, 0, 10)
			return answer(total, err)
		}},
		{"AgentNameTaken", false, func(s *Store, org int64) any { return answer(s.AgentNameTaken(org, agent.Name, 0)) }},
		{"SkipMail", changes, func(s *Store, org int64) any { return s.SkipMail(org, queued.ID, "skipped") }},
		{"ClaimMail", false, func(s *Store, org int64) any { return answer(s.ClaimMail(org, queued.ID, queued.Address)) }},
		{"SettleMail", changes, func(s *Store, org int64) any { return s.SettleMail(org, sent.ID, marketing.MailUnknown, "unknown") }},
		{"CampaignWent", changes, func(s *Store, org int64) any { return s.CampaignWent(org, campaign.ID) }},
		{"PauseCampaign", changes, func(s *Store, org int64) any { return s.PauseCampaign(org, campaign.ID, "paused") }},
		{"FinishIfDone", changes, func(s *Store, org int64) any { return s.FinishIfDone(org, campaign.ID) }},
		{"AgentNamed", false, func(s *Store, org int64) any { return answer(s.AgentNamed(org, agent.Name)) != nil }},
		{"AgentKey", changes, func(s *Store, org int64) any {
			return answer(s.AgentKey(org, &store.Agent{ID: agent.ID, OrgID: first.ID, Name: agent.Name}))
		}},
		{"SaveAgentRow", changes, func(s *Store, org int64) any {
			return s.SaveAgentRow(org, &store.Agent{ID: agent.ID, OrgID: first.ID, Name: "renamed"})
		}},
		{"OnTask", false, func(s *Store, org int64) any {
			_, err := s.OnTask(org, store.Actor{Name: "scout"}, task.ID)
			return err == nil
		}},
		{"TaskBriefsFor", 0, func(s *Store, org int64) any {
			briefs, _, err := s.TaskBriefsFor(org, store.Actor{}, false, nil, paging.Page{})
			return len(must(briefs, err))
		}},
		{"ResearchList", 0, func(s *Store, org int64) any {
			rows, _, _, err := s.ResearchList(org, 0, first.Settings, "review", 0, paging.Page{})
			return len(must(rows, err))
		}},
		{"HistoryCount", 0, func(s *Store, org int64) any { return answer(s.HistoryCount(org, company.ID)) }},
		{"CustomerCompanies", 0, func(s *Store, org int64) any { return len(must(s.CustomerCompanies(org))) }},
		{"AroundEnquiries", 0, func(s *Store, org int64) any {
			around := must(s.AroundEnquiries(org, []store.Enquiry{enquiry}))
			return len(around.Companies) + len(around.Threads)
		}},
		{"StartReply", changes, func(s *Store, org int64) any {
			_, _, err := s.StartReply(&store.Org{ID: org, Name: "x"}, enquiry.ID, "Another word", "Owner One")
			return err
		}},
		{"SettleReply", changes, func(s *Store, org int64) any { return s.SettleReply(org, &reply, true, "Owner One") }},
		{"MailFailed", changes, func(s *Store, org int64) any {
			return s.MailFailed(org, sent.Address, marketing.SuppressBounced, "gone")
		}},
		{"CustomerSteps", 0, func(s *Store, org int64) any { return len(must(s.CustomerSteps(org, customer))) }},
		{"CustomerSetStep", changes, func(s *Store, org int64) any { return s.CustomerSetStep(org, customer, step.ID, true) }},
		{"RedeemCustomerLink", changes, func(s *Store, org int64) any { return answer(s.RedeemCustomerLink(org, customer, link)) }},
	}
	ready := map[string]func(s *Store) error{
		// Only a campaign with nothing left to send can be finished.
		"FinishIfDone": func(s *Store) error { return s.SkipMail(first.ID, queued.ID, "skipped") },
	}
	try := func(name string, call func(s *Store, org int64) any, org int64) (got any, changed bool) {
		on := s.through(s.DB.Begin())
		defer on.DB.Rollback()
		if ready[name] != nil {
			if err := ready[name](on); err != nil {
				t.Fatal(err)
			}
		}
		before := holdings(t, on, first.ID)
		got = call(on, org)
		return got, !reflect.DeepEqual(holdings(t, on, first.ID), before)
	}
	for _, c := range tries {
		got, changed := try(c.name, c.call, second.ID)
		if changed {
			t.Errorf("%s, called as another organisation, changed the first one's rows", c.name)
		}
		if c.nothing != changes && !reflect.DeepEqual(got, c.nothing) {
			t.Errorf("%s, called as another organisation, answered %v", c.name, got)
		}
		got, changed = try(c.name, c.call, first.ID)
		if c.nothing == changes && !changed || c.nothing != changes && reflect.DeepEqual(got, c.nothing) {
			t.Errorf("%s, called as the first organisation, didn't reach its rows either, so the call as another proved nothing", c.name)
		}
	}
}
