package main

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/internal/paging"
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
	var request TaskRequest
	if err := s.DB.Where("task_id = ?", task.ID).First(&request).Error; err != nil {
		t.Fatal(err)
	}
	agent := Agent{OrgID: first.ID, Name: "scout"}
	if err := s.DB.Create(&agent).Error; err != nil {
		t.Fatal(err)
	}
	lease := now.Add(-time.Minute).UnixMilli()
	run := must(s.AddRun(first.ID, AgentRun{TaskID: task.ID, AgentID: agent.ID, Status: RunWorking, Owner: "server-a", LeaseUntil: lease, StartedAt: now}))
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
	claim := RequestClaim{RequestID: request.ID, OrgID: first.ID, TaskID: task.ID, Holder: fmt.Sprintf("key:%d", key.ID), Actor: "scout", ClaimedAt: now.UnixMilli()}
	domain := must(s.AddSSODomain(first.ID, "alpha.example"))
	campaign := Campaign{OrgID: first.ID, Name: "Spring", Status: CampaignSending}
	if err := s.DB.Create(&campaign).Error; err != nil {
		t.Fatal(err)
	}
	queued := CampaignRecipient{OrgID: first.ID, CampaignID: campaign.ID, Address: "ann@alpha.example", Status: MailQueued}
	sent := CampaignRecipient{OrgID: first.ID, CampaignID: campaign.ID, Address: "bob@alpha.example", Status: MailSent, SentAt: &now}
	note := TaskNote{OrgID: first.ID, TaskID: task.ID, CompanyID: 7, Actor: "scout", Kind: "skipped", Text: "not for us"}
	for _, row := range []any{&claim, &queued, &sent, &note} {
		if err := s.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	ann := person(t, s, first, "Ann Alpha", "Alpha Works", "ann@alpha.example", false)
	company := Company{ID: *ann.CompanyID}
	if err := s.DB.Model(&company).Update("stage", CompanyReview).Error; err != nil {
		t.Fatal(err)
	}
	enquiry := Enquiry{OrgID: first.ID, Name: "Ann Alpha", Email: "ann@alpha.example", CompanyID: ann.CompanyID, ContactID: &ann.ID}
	if err := s.DB.Create(&enquiry).Error; err != nil {
		t.Fatal(err)
	}
	reply := EnquiryMessage{OrgID: first.ID, EnquiryID: enquiry.ID, Ours: true, Text: "Thanks", By: "Owner One", CreatedAt: now}
	onboarding := Onboarding{OrgID: first.ID, CompanyID: company.ID, Name: "Setup"}
	subscription := Subscription{OrgID: first.ID, CompanyID: company.ID, Name: "Licence", Start: "2020-01-01"}
	for _, row := range []any{&reply, &onboarding, &subscription} {
		if err := s.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	step := OnboardingStep{OrgID: first.ID, OnboardingID: onboarding.ID, Seq: 1, Name: "Send files", Who: StepTheirs}
	grant := CustomerGrant{OrgID: first.ID, OnboardingID: onboarding.ID, ContactID: ann.ID}
	for _, row := range []any{&step, &grant} {
		if err := s.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	link := randomToken(16)
	if err := s.DB.Create(&CustomerLink{OrgID: first.ID, GrantID: grant.ID, TokenHash: hashToken(link), ExpiresAt: now.Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	customer := &CustomerAccess{Grant: grant, Org: *first, Onboarding: onboarding, Contact: *ann}

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
			return answer(s.HoldRun(org, *run, RunWaiting, nil, now.Add(time.Hour)))
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
		{"SettleMail", changes, func(s *Store, org int64) any { return s.SettleMail(org, sent.ID, MailUnknown, "unknown") }},
		{"CampaignWent", changes, func(s *Store, org int64) any { return s.CampaignWent(org, campaign.ID) }},
		{"PauseCampaign", changes, func(s *Store, org int64) any { return s.PauseCampaign(org, campaign.ID, "paused") }},
		{"FinishIfDone", changes, func(s *Store, org int64) any { return s.FinishIfDone(org, campaign.ID) }},
		{"AgentNamed", false, func(s *Store, org int64) any { return answer(s.AgentNamed(org, agent.Name)) != nil }},
		{"AgentKey", changes, func(s *Store, org int64) any {
			return answer(s.AgentKey(org, &Agent{ID: agent.ID, OrgID: first.ID, Name: agent.Name}))
		}},
		{"SaveAgentRow", changes, func(s *Store, org int64) any {
			return s.SaveAgentRow(org, &Agent{ID: agent.ID, OrgID: first.ID, Name: "renamed"})
		}},
		{"OnTask", false, func(s *Store, org int64) any {
			_, err := s.OnTask(org, Actor{Name: "scout"}, task.ID)
			return err == nil
		}},
		{"TaskBriefsFor", 0, func(s *Store, org int64) any {
			briefs, _, err := s.TaskBriefsFor(org, Actor{}, false, nil, paging.Page{})
			return len(must(briefs, err))
		}},
		{"ResearchList", 0, func(s *Store, org int64) any {
			rows, _, _, err := s.ResearchList(org, 0, first.Settings, "review", 0, paging.Page{})
			return len(must(rows, err))
		}},
		{"HistoryCount", 0, func(s *Store, org int64) any { return answer(s.HistoryCount(org, company.ID)) }},
		{"CustomerCompanies", 0, func(s *Store, org int64) any { return len(must(s.CustomerCompanies(org))) }},
		{"AroundEnquiries", 0, func(s *Store, org int64) any {
			around := must(s.AroundEnquiries(org, []Enquiry{enquiry}))
			return len(around.Companies) + len(around.Threads)
		}},
		{"StartReply", changes, func(s *Store, org int64) any {
			_, _, err := s.StartReply(&Org{ID: org, Name: "x"}, enquiry.ID, "Another word", "Owner One")
			return err
		}},
		{"SettleReply", changes, func(s *Store, org int64) any { return s.SettleReply(org, &reply, true, "Owner One") }},
		{"MailFailed", changes, func(s *Store, org int64) any { return s.MailFailed(org, sent.Address, SuppressBounced, "gone") }},
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
