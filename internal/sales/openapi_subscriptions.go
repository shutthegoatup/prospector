package sales

import (
	"context"
	"strconv"

	"github.com/shutthegoatup/prospector/api"

	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

func SubscriptionOut(r SubscriptionRow, day string) api.Subscription {
	out := api.Subscription{Id: r.ID, CompanyId: r.CompanyID, Company: r.Company, Name: r.Name, Amount: r.Amount, Currency: r.Currency, Per: r.Per,
		Start: r.Start, TermMonths: r.TermMonths, Then: r.Then, NoticeDays: r.NoticeDays, NoticeOn: store.Deref(r.NoticeOn), EndedOn: store.Deref(r.EndedOn),
		Status: api.SubscriptionStatus(r.Status), TermEnd: r.TermEnd, CancelBy: r.CancelBy, CancelEnds: r.Out, Ends: r.Ends, Renewing: r.Renewing,
		Figure: r.Figure(), Soon: r.Soon(day), People: []api.SubscriptionPerson{}, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	for _, p := range r.People {
		out.People = append(out.People, api.SubscriptionPerson{Id: p.ID, Name: store.DisplayName(p)})
	}
	return out
}

// subscription is one as the API gives it, with where it stands today.
func (s SalesAPI) subscription(org, company, id int64) (api.Subscription, error) {
	day := store.Today()
	rows, err := s.Subscriptions.Subscriptions(org, company, day)
	if err != nil {
		return api.Subscription{}, err
	}
	for _, r := range rows {
		if r.ID == id {
			return SubscriptionOut(r, day), nil
		}
	}
	return api.Subscription{}, store.NoSuch("no subscription %d", id)
}

func (s SalesAPI) ListSubscriptions(ctx context.Context, in api.ListSubscriptionsRequestObject) (api.ListSubscriptionsResponseObject, error) {
	day := store.Today()
	rows, err := s.Subscriptions.Subscriptions(request.ReqFrom(ctx).Org.ID, in.Params.Company, day)
	if err != nil {
		return nil, err
	}
	worth := Contracted(rows)
	rows, next, err := paging.ListPage(rows, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListSubscriptions200JSONResponse{Headers: api.ListSubscriptions200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor, out.Body.Contracted = []api.Subscription{}, next, worth
	for _, r := range rows {
		out.Body.Items = append(out.Body.Items, SubscriptionOut(r, day))
	}
	return out, nil
}

func (s SalesAPI) AddSubscription(ctx context.Context, in api.AddSubscriptionRequestObject) (api.AddSubscriptionResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send the subscription: {\"company\": id, \"name\": …, \"start\": …}")
	}
	q, b := request.ReqFrom(ctx), in.Body
	sub, err := s.Subscriptions.AddSubscription(q.Org.ID, SubscriptionIn{Company: b.Company, Name: b.Name, Amount: b.Amount, Per: b.Per, Start: b.Start,
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

func (s SalesAPI) ChangeSubscription(ctx context.Context, in api.ChangeSubscriptionRequestObject) (api.ChangeSubscriptionResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send what to change")
	}
	q, b := request.ReqFrom(ctx), in.Body
	number := func(n *int) *string {
		if n == nil {
			return nil
		}
		text := strconv.Itoa(*n)
		return &text
	}
	// Dates being cleared go first, since a start is checked against them; those being
	// set go last, since they are checked against the start.
	var cleared, changes, dated []store.Change
	for _, set := range []struct {
		field string
		value *string
	}{{"name", b.Name}, {"amount", b.Amount}, {"per", b.Per}, {"start", b.Start}, {"term_months", number(b.TermMonths)}, {"then", b.Then},
		{"notice_days", number(b.NoticeDays)}, {"notice_on", b.NoticeOn}, {"ended_on", b.EndedOn}} {
		switch c := (store.Change{Field: set.field, Value: store.Deref(set.value)}); {
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
		return nil, say.New("send what to change")
	}
	if err := s.Subscriptions.ChangeSubscriptionFields(q.Org.ID, in.Id, changes, q.Actor()); err != nil {
		return nil, err
	}
	out, err := s.subscription(q.Org.ID, 0, in.Id)
	if err != nil {
		return nil, err
	}
	return api.ChangeSubscription200JSONResponse(out), nil
}

func (s SalesAPI) RemoveSubscription(ctx context.Context, in api.RemoveSubscriptionRequestObject) (api.RemoveSubscriptionResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.Subscriptions.RemoveSubscription(q.Org.ID, in.Id, q.Actor()); err != nil {
		return nil, err
	}
	return api.RemoveSubscription204Response{}, nil
}

func (s SalesAPI) StartRenewal(ctx context.Context, in api.StartRenewalRequestObject) (api.StartRenewalResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("say who the renewal is with: {\"contact\": id}")
	}
	q := request.ReqFrom(ctx)
	d, err := s.Subscriptions.StartRenewal(q.Org.ID, in.Id, in.Body.Contact, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.StartRenewal201JSONResponse{Deal: d.ID, Contact: d.ContactID}, nil
}
