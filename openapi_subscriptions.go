package main

import (
	"context"
	"errors"
	"strconv"

	"github.com/shutthegoatup/prospector/api"
)

func subscriptionOut(r SubscriptionRow) api.Subscription {
	return api.Subscription{Id: r.ID, CompanyId: r.CompanyID, Company: r.Company, Name: r.Name, Amount: r.Amount, Currency: r.Currency, Per: r.Per,
		Start: r.Start, TermMonths: r.TermMonths, Then: r.Then, NoticeDays: r.NoticeDays, NoticeOn: deref(r.NoticeOn), EndedOn: deref(r.EndedOn),
		Status: api.SubscriptionStatus(r.Status), TermEnd: r.TermEnd, CancelBy: r.CancelBy, CancelEnds: r.Out, Ends: r.Ends, Renewing: r.Renewing,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

// subscription is one as the API gives it, with where it stands today.
func (s apiServer) subscription(org, company, id int64) (api.Subscription, error) {
	rows, err := s.a.s.Subscriptions(org, company, today())
	if err != nil {
		return api.Subscription{}, err
	}
	for _, r := range rows {
		if r.ID == id {
			return subscriptionOut(r), nil
		}
	}
	return api.Subscription{}, noSuch("no subscription %d", id)
}

func (s apiServer) ListSubscriptions(ctx context.Context, in api.ListSubscriptionsRequestObject) (api.ListSubscriptionsResponseObject, error) {
	rows, err := s.a.s.Subscriptions(reqFrom(ctx).Org.ID, in.Params.Company, today())
	if err != nil {
		return nil, err
	}
	rows, next, err := listPage(rows, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListSubscriptions200JSONResponse{Headers: api.ListSubscriptions200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.Subscription{}, next
	for _, r := range rows {
		out.Body.Items = append(out.Body.Items, subscriptionOut(r))
	}
	return out, nil
}

func (s apiServer) AddSubscription(ctx context.Context, in api.AddSubscriptionRequestObject) (api.AddSubscriptionResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send the subscription: {\"company\": id, \"name\": …, \"start\": …}")
	}
	q, b := reqFrom(ctx), in.Body
	sub, err := s.a.s.AddSubscription(q.Org.ID, SubscriptionIn{Company: b.Company, Name: b.Name, Amount: b.Amount, Per: b.Per, Start: b.Start,
		TermMonths: b.TermMonths, Then: b.Then, NoticeDays: b.NoticeDays}, q.Actor())
	if err != nil {
		return nil, err
	}
	out, err := s.subscription(q.Org.ID, sub.CompanyID, sub.ID)
	if err != nil {
		return nil, err
	}
	return api.AddSubscription201JSONResponse(out), nil
}

func (s apiServer) ChangeSubscription(ctx context.Context, in api.ChangeSubscriptionRequestObject) (api.ChangeSubscriptionResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send what to change")
	}
	q, b := reqFrom(ctx), in.Body
	number := func(n *int) *string {
		if n == nil {
			return nil
		}
		text := strconv.Itoa(*n)
		return &text
	}
	// Dates being cleared go first, since a start is checked against them; those being
	// set go last, since they are checked against the start.
	var cleared, changes, dated []SubscriptionChange
	for _, set := range []struct {
		field string
		value *string
	}{{"name", b.Name}, {"amount", b.Amount}, {"per", b.Per}, {"start", b.Start}, {"term_months", number(b.TermMonths)}, {"then", b.Then},
		{"notice_days", number(b.NoticeDays)}, {"notice_on", b.NoticeOn}, {"ended_on", b.EndedOn}} {
		switch c := (SubscriptionChange{set.field, deref(set.value)}); {
		case set.value == nil:
		case set.field != "notice_on" && set.field != "ended_on":
			changes = append(changes, c)
		case c.Value == "":
			cleared = append(cleared, c)
		default:
			dated = append(dated, c)
		}
	}
	changes = append(append(cleared, changes...), dated...)
	if len(changes) == 0 {
		return nil, errors.New("send what to change")
	}
	if err := s.a.s.ChangeSubscriptionFields(q.Org.ID, in.Id, changes, q.Actor()); err != nil {
		return nil, err
	}
	out, err := s.subscription(q.Org.ID, 0, in.Id)
	if err != nil {
		return nil, err
	}
	return api.ChangeSubscription200JSONResponse(out), nil
}

func (s apiServer) RemoveSubscription(ctx context.Context, in api.RemoveSubscriptionRequestObject) (api.RemoveSubscriptionResponseObject, error) {
	q := reqFrom(ctx)
	if err := s.a.s.RemoveSubscription(q.Org.ID, in.Id, q.Actor()); err != nil {
		return nil, err
	}
	return api.RemoveSubscription204Response{}, nil
}

func (s apiServer) StartRenewal(ctx context.Context, in api.StartRenewalRequestObject) (api.StartRenewalResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("say who the renewal is with: {\"contact\": id}")
	}
	q := reqFrom(ctx)
	d, err := s.a.s.StartRenewal(q.Org.ID, in.Id, in.Body.Contact, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.StartRenewal201JSONResponse{Deal: d.ID, Contact: d.ContactID}, nil
}
