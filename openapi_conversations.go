package main

import (
	"context"

	"github.com/shutthegoatup/prospector/api"
)

func (s apiServer) ListConversations(ctx context.Context, in api.ListConversationsRequestObject) (api.ListConversationsResponseObject, error) {
	q := reqFrom(ctx)
	rows, err := s.board(q.Org.ID)
	if err != nil {
		return nil, err
	}
	page, next, err := listPage(rows, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	started, err := s.a.s.Onboardings(q.Org.ID, 0, today())
	if err != nil {
		return nil, err
	}
	onboarding := map[int64]string{}
	for _, ob := range started {
		// A company with one onboarding still running is being onboarded, whatever else is live.
		if !ob.Live() {
			onboarding[ob.CompanyID] = "running"
		} else if onboarding[ob.CompanyID] == "" {
			onboarding[ob.CompanyID] = "live"
		}
	}
	out := api.ListConversations200JSONResponse{Headers: api.ListConversations200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.DealCard{}, next
	if plan, err := q.Org.Settings.Onboarding(); err == nil {
		out.Body.OnboardingSteps = len(plan)
	}
	for _, r := range page {
		d := dealOut(r.deal, r.with, r.own)
		card := api.DealCard{Id: d.Id, ContactId: d.ContactId, Person: d.Person, CompanyId: d.CompanyId, Seq: d.Seq, Kind: d.Kind,
			Stage: d.Stage, NextStep: d.NextStep, NextDate: d.NextDate, LostReason: d.LostReason, Amount: d.Amount, Currency: d.Currency,
			Value: d.Value, CloseDate: d.CloseDate, Owner: d.Owner, SubscriptionId: d.SubscriptionId,
			CallNotes: deref(r.with.CallNotes)}
		if r.with.CompanyID != nil {
			card.Company, card.Onboarding = companyName(deref(r.with.Maker)), onboarding[*r.with.CompanyID]
		}
		out.Body.Items = append(out.Body.Items, card)
	}
	return out, nil
}
