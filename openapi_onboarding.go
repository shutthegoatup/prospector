package main

import (
	"context"
	"errors"
	"log"

	"github.com/shutthegoatup/prospector/api"
)

func stepOut(st OnboardingStep) api.OnboardingStep {
	return api.OnboardingStep{Id: st.ID, OnboardingId: st.OnboardingID, Seq: st.Seq, Name: st.Name, Who: api.OnboardingStepWho(st.Who),
		Due: deref(st.Due), DoneOn: deref(st.DoneOn), DoneBy: st.DoneBy}
}

func onboardingOut(row OnboardingRow, invites []CustomerInvite) api.Onboarding {
	out := api.Onboarding{Id: row.ID, CompanyId: row.CompanyID, Company: row.Company, Name: row.Name, Start: row.Start, CreatedAt: row.CreatedAt,
		Steps: []api.OnboardingStep{}, Done: row.Done, Late: row.Late, LiveOn: row.LiveOn, Invites: []api.OnboardingInvite{}, Invitable: []api.Invitable{}, CanInvite: row.MailOn}
	for _, p := range row.Invitable {
		out.Invitable = append(out.Invitable, api.Invitable{Id: p.ID, Name: displayName(p), Email: deref(p.Email)})
	}
	for _, st := range row.Steps {
		out.Steps = append(out.Steps, stepOut(st))
	}
	for _, inv := range invites {
		out.Invites = append(out.Invites, api.OnboardingInvite{Id: inv.ID, ContactId: inv.ContactID, Name: inv.Name, Email: inv.Email,
			InvitedBy: inv.InvitedBy, CreatedAt: inv.CreatedAt, Withdrawn: inv.RevokedAt != nil})
	}
	return out
}

func (s apiServer) onboardings(org, company int64) ([]api.Onboarding, error) {
	rows, err := s.a.s.Onboardings(org, company, today())
	if err != nil {
		return nil, err
	}
	// Who has been and could be invited, and whether mail can go, as the page shows them.
	if err := s.a.customerControls(org, rows); err != nil {
		return nil, err
	}
	out := []api.Onboarding{}
	for _, row := range rows {
		out = append(out, onboardingOut(row, row.Invites))
	}
	return out, nil
}

func (s apiServer) ListOnboardings(ctx context.Context, in api.ListOnboardingsRequestObject) (api.ListOnboardingsResponseObject, error) {
	all, err := s.onboardings(reqFrom(ctx).Org.ID, in.Params.Company)
	if err != nil {
		return nil, err
	}
	page, next, err := listPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListOnboardings200JSONResponse{Headers: api.ListOnboardings200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = append([]api.Onboarding{}, page...), next
	return out, nil
}

func (s apiServer) StartOnboarding(ctx context.Context, in api.StartOnboardingRequestObject) (api.StartOnboardingResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("say which company: {\"company\": id}")
	}
	q := reqFrom(ctx)
	start := in.Body.Start
	if start == "" {
		start = today()
	}
	ob, err := s.a.s.StartOnboarding(q.Org.ID, OnboardingIn{Company: in.Body.Company, Name: in.Body.Name, Start: start}, q.Actor())
	if err != nil {
		return nil, err
	}
	all, err := s.onboardings(q.Org.ID, ob.CompanyID)
	if err != nil {
		return nil, err
	}
	for _, o := range all {
		if o.Id == ob.ID {
			return api.StartOnboarding201JSONResponse(o), nil
		}
	}
	return nil, noSuch("no onboarding %d", ob.ID)
}

func (s apiServer) RemoveOnboarding(ctx context.Context, in api.RemoveOnboardingRequestObject) (api.RemoveOnboardingResponseObject, error) {
	q := reqFrom(ctx)
	if err := s.a.s.RemoveOnboarding(q.Org.ID, in.Id, q.Actor()); err != nil {
		return nil, err
	}
	return api.RemoveOnboarding204Response{}, nil
}

func (s apiServer) ChangeOnboardingStep(ctx context.Context, in api.ChangeOnboardingStepRequestObject) (api.ChangeOnboardingStepResponseObject, error) {
	if in.Body == nil || (in.Body.Done == nil && in.Body.Due == nil) {
		return nil, errors.New("send what to change: done, due, or both")
	}
	q := reqFrom(ctx)
	if in.Body.Due != nil {
		if err := s.a.s.SetOnboardingStep(q.Org.ID, in.Id, "due", *in.Body.Due, q.Actor()); err != nil {
			return nil, err
		}
	}
	if in.Body.Done != nil {
		value := ""
		if *in.Body.Done {
			value = "yes"
		}
		if err := s.a.s.SetOnboardingStep(q.Org.ID, in.Id, "done", value, q.Actor()); err != nil {
			return nil, err
		}
	}
	var st OnboardingStep
	if err := s.a.s.in(q.Org.ID).Where("id = ?", in.Id).First(&st).Error; err != nil {
		return nil, missing(err, "no onboarding step %d", in.Id)
	}
	return api.ChangeOnboardingStep200JSONResponse(stepOut(st)), nil
}

func (s apiServer) InviteCustomer(ctx context.Context, in api.InviteCustomerRequestObject) (api.InviteCustomerResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("say who to invite: {\"contact\": id}")
	}
	q := reqFrom(ctx)
	// Checked before anyone is let in: access with no way to send the link is no use.
	if !s.a.customerMailOn(q.Org.ID) {
		return nil, errNoCustomerMail
	}
	token, c, err := s.a.s.InviteCustomer(q.Org, in.Id, in.Body.Contact, q.Actor())
	if err != nil {
		return nil, err
	}
	if err := s.a.customerLinkMail(ctx, q.Org, deref(c.Email), token); err != nil {
		// Nothing was sent, so the wait before another try doesn't apply.
		s.a.s.dropCustomerLink(token)
		log.Printf("a customer's link wasn't sent: %v", err)
		return nil, notSent{errors.New("they were let in, but the mail server didn't take the link; invite them again to send another")}
	}
	return api.InviteCustomer204Response{}, nil
}

func (s apiServer) WithdrawCustomerInvite(ctx context.Context, in api.WithdrawCustomerInviteRequestObject) (api.WithdrawCustomerInviteResponseObject, error) {
	q := reqFrom(ctx)
	if err := s.a.s.RevokeCustomer(q.Org.ID, in.Id, q.Actor()); err != nil {
		return nil, err
	}
	return api.WithdrawCustomerInvite204Response{}, nil
}
