package onboarding

import (
	"context"
	"log"

	"github.com/shutthegoatup/prospector/api"

	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

func stepOut(st store.OnboardingStep) api.OnboardingStep {
	return api.OnboardingStep{Id: st.ID, OnboardingId: st.OnboardingID, Seq: st.Seq, Name: st.Name, Who: api.OnboardingStepWho(st.Who),
		Due: store.Deref(st.Due), DoneOn: store.Deref(st.DoneOn), DoneBy: st.DoneBy}
}

func onboardingOut(row OnboardingRow, invites []CustomerInvite) api.Onboarding {
	out := api.Onboarding{Id: row.ID, CompanyId: row.CompanyID, Company: row.Company, Name: row.Name, Start: row.Start, CreatedAt: row.CreatedAt,
		Steps: []api.OnboardingStep{}, Done: row.Done, Late: row.Late, LiveOn: row.LiveOn, Invites: []api.OnboardingInvite{}, Invitable: []api.Invitable{}, CanInvite: row.MailOn}
	for _, p := range row.Invitable {
		out.Invitable = append(out.Invitable, api.Invitable{Id: p.ID, Name: store.DisplayName(p), Email: store.Deref(p.Email)})
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

func (s OnboardingAPI) Onboardings(org, company int64) ([]api.Onboarding, error) {
	rows, err := s.Steps.Onboardings(org, company, store.Today())
	if err != nil {
		return nil, err
	}
	// Who has been and could be invited, and whether mail can go, as the page shows them.
	if err := s.App.CustomerControls(org, rows); err != nil {
		return nil, err
	}
	out := []api.Onboarding{}
	for _, row := range rows {
		out = append(out, onboardingOut(row, row.Invites))
	}
	return out, nil
}

func (s OnboardingAPI) ListOnboardings(ctx context.Context, in api.ListOnboardingsRequestObject) (api.ListOnboardingsResponseObject, error) {
	all, err := s.Onboardings(request.ReqFrom(ctx).Org.ID, in.Params.Company)
	if err != nil {
		return nil, err
	}
	page, next, err := paging.ListPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListOnboardings200JSONResponse{Headers: api.ListOnboardings200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = append([]api.Onboarding{}, page...), next
	return out, nil
}

func (s OnboardingAPI) StartOnboarding(ctx context.Context, in api.StartOnboardingRequestObject) (api.StartOnboardingResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("say which company: {\"company\": id}")
	}
	q := request.ReqFrom(ctx)
	start := in.Body.Start
	if start == "" {
		start = store.Today()
	}
	ob, err := s.Steps.StartOnboarding(q.Org.ID, OnboardingIn{Company: in.Body.Company, Name: in.Body.Name, Start: start}, q.Actor())
	if err != nil {
		return nil, err
	}
	all, err := s.Onboardings(q.Org.ID, ob.CompanyID)
	if err != nil {
		return nil, err
	}
	for _, o := range all {
		if o.Id == ob.ID {
			return api.StartOnboarding201JSONResponse(o), nil
		}
	}
	return nil, store.NoSuch("no onboarding %d", ob.ID)
}

func (s OnboardingAPI) RemoveOnboarding(ctx context.Context, in api.RemoveOnboardingRequestObject) (api.RemoveOnboardingResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.Steps.RemoveOnboarding(q.Org.ID, in.Id, q.Actor()); err != nil {
		return nil, err
	}
	return api.RemoveOnboarding204Response{}, nil
}

func (s OnboardingAPI) ChangeOnboardingStep(ctx context.Context, in api.ChangeOnboardingStepRequestObject) (api.ChangeOnboardingStepResponseObject, error) {
	if in.Body == nil || (in.Body.Done == nil && in.Body.Due == nil) {
		return nil, say.New("send what to change: done, due, or both")
	}
	q := request.ReqFrom(ctx)
	if in.Body.Due != nil {
		if err := s.Steps.SetOnboardingStep(q.Org.ID, in.Id, "due", *in.Body.Due, q.Actor()); err != nil {
			return nil, err
		}
	}
	if in.Body.Done != nil {
		value := ""
		if *in.Body.Done {
			value = "yes"
		}
		if err := s.Steps.SetOnboardingStep(q.Org.ID, in.Id, "done", value, q.Actor()); err != nil {
			return nil, err
		}
	}
	st, err := s.Steps.OnboardingStepByID(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	return api.ChangeOnboardingStep200JSONResponse(stepOut(*st)), nil
}

func (s OnboardingAPI) InviteCustomer(ctx context.Context, in api.InviteCustomerRequestObject) (api.InviteCustomerResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("say who to invite: {\"contact\": id}")
	}
	q := request.ReqFrom(ctx)
	// Checked before anyone is let in: access with no way to send the link is no use.
	if !s.App.CustomerMailOn(q.Org.ID) {
		return nil, ErrNoCustomerMail
	}
	token, c, err := s.Customers.InviteCustomer(q.Org, in.Id, in.Body.Contact, q.Actor())
	if err != nil {
		return nil, err
	}
	if err := s.App.CustomerLinkMail(ctx, q.Org, store.Deref(c.Email), token); err != nil {
		// Nothing was sent, so the wait before another try doesn't apply.
		s.Customers.DropCustomerLink(token)
		log.Printf("a customer's link wasn't sent: %v", err)
		return nil, store.NotSent{Err: say.New("they were let in, but the mail server didn't take the link; invite them again to send another")}
	}
	return api.InviteCustomer204Response{}, nil
}

func (s OnboardingAPI) WithdrawCustomerInvite(ctx context.Context, in api.WithdrawCustomerInviteRequestObject) (api.WithdrawCustomerInviteResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.Customers.RevokeCustomer(q.Org.ID, in.Id, q.Actor()); err != nil {
		return nil, err
	}
	return api.WithdrawCustomerInvite204Response{}, nil
}

// OnboardingAPI is the onboarding product's operations: each company's steps and the
// customers invited to follow them, which apiServer embeds.
type OnboardingAPI struct {
	Steps     *OnboardingStore
	Customers *CustomerStore
	App       onboardingApp
}

// onboardingApp is what onboarding asks of the app: whether it can mail a customer, the
// mail with their link, and what an organisation's customers may do.
type onboardingApp interface {
	CustomerMailOn(org int64) bool
	CustomerLinkMail(ctx context.Context, org *store.Org, to, token string) error
	CustomerControls(org int64, rows []OnboardingRow) error
}
