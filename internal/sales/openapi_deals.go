package sales

import (
	"context"

	"github.com/shutthegoatup/prospector/api"

	"github.com/shutthegoatup/prospector/internal/onboarding"
	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// dealOut is a deal as the API gives it. A conversation's own stands where its contact
// does, and has a kind before anything is stored for it.
func dealOut(d store.Deal, with store.Contact, own bool) api.Deal {
	out := api.Deal{Id: d.ID, ContactId: with.ID, Person: store.DisplayName(with), CompanyId: store.IDOf(with.CompanyID), Seq: d.Seq, Kind: d.Kind,
		Stage: d.Stage, NextStep: d.NextStep, NextDate: store.Deref(d.NextDate), LostReason: d.LostReason, Amount: d.Amount, Currency: d.Currency,
		Value: d.Value(), CloseDate: store.Deref(d.CloseDate), Owner: d.Owner, SubscriptionId: d.SubscriptionID}
	if own {
		out.Stage, out.NextStep, out.NextDate, out.LostReason = with.ConversationStage(), with.NextStep, store.Deref(with.NextDate), with.LostReason
		if out.Kind == "" {
			out.Kind = DealNew
		}
	}
	return out
}

// boardDeal is a deal on the board with the person it is with.
type boardDeal struct {
	deal store.Deal
	with store.Contact
	own  bool
}

// board is every deal on the Conversations board, by stage in the board's order.
func (s SalesAPI) board(org int64) ([]boardDeal, error) {
	convs, err := s.People.Conversations(org)
	if err != nil {
		return nil, err
	}
	own, err := s.Pipeline.Deals(org)
	if err != nil {
		return nil, err
	}
	others, err := s.Pipeline.OtherDeals(org)
	if err != nil {
		return nil, err
	}
	var out []boardDeal
	for _, st := range store.Stages {
		for _, c := range convs[st.Key] {
			out = append(out, boardDeal{own[c.ID], c, true})
		}
		for _, o := range others[st.Key] {
			out = append(out, boardDeal{o.Deal, o.With, false})
		}
	}
	return out, nil
}

func (s SalesAPI) deals(org int64) ([]api.Deal, error) {
	rows, err := s.board(org)
	if err != nil {
		return nil, err
	}
	out := []api.Deal{}
	for _, r := range rows {
		out = append(out, dealOut(r.deal, r.with, r.own))
	}
	return out, nil
}

// deal finds one in the list: a conversation's own by its contact, any other by its id.
func (s SalesAPI) deal(org int64, own bool, id int64) (api.Deal, error) {
	all, err := s.deals(org)
	if err != nil {
		return api.Deal{}, err
	}
	for _, d := range all {
		if (own && d.Seq == 0 && d.ContactId == id) || (!own && d.Seq != 0 && d.Id == id) {
			return d, nil
		}
	}
	return api.Deal{}, store.NoSuch("no deal %d", id)
}

func dealChanges(b *api.DealChange) store.Changes {
	return store.Changes{}.Set("kind", b.Kind).Set("amount", b.Amount).Set("close_date", b.CloseDate).Set("owner", b.Owner).
		Set("stage", b.Stage).Set("next_step", b.NextStep).Set("next_date", b.NextDate).Set("lost_reason", b.LostReason)
}

func (s SalesAPI) ListDeals(ctx context.Context, in api.ListDealsRequestObject) (api.ListDealsResponseObject, error) {
	all, err := s.deals(request.ReqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	page, next, err := paging.ListPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListDeals200JSONResponse{Headers: api.ListDeals200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = append([]api.Deal{}, page...), next
	return out, nil
}

func (s SalesAPI) StartDeal(ctx context.Context, in api.StartDealRequestObject) (api.StartDealResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("say who it is with: {\"contact\": id}")
	}
	q := request.ReqFrom(ctx)
	kind := in.Body.Kind
	if kind == "" {
		kind = DealNew
	}
	d, err := s.Pipeline.StartDeal(q.Org.ID, in.Body.Contact, kind, q.Actor())
	if err != nil {
		return nil, err
	}
	out, err := s.deal(q.Org.ID, false, d.ID)
	if err != nil {
		return nil, err
	}
	return api.StartDeal201JSONResponse(out), nil
}

func (s SalesAPI) ChangeDeal(ctx context.Context, in api.ChangeDealRequestObject) (api.ChangeDealResponseObject, error) {
	if in.Body == nil || len(dealChanges(in.Body)) == 0 {
		return nil, say.New("send what to change")
	}
	q := request.ReqFrom(ctx)
	if err := s.Pipeline.ChangeDealFields(q.Org.ID, in.Id, dealChanges(in.Body), q.Actor()); err != nil {
		return nil, err
	}
	out, err := s.deal(q.Org.ID, false, in.Id)
	if err != nil {
		return nil, err
	}
	return api.ChangeDeal200JSONResponse(out), nil
}

func (s SalesAPI) RemoveDeal(ctx context.Context, in api.RemoveDealRequestObject) (api.RemoveDealResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.Pipeline.RemoveDeal(q.Org.ID, in.Id, q.Actor()); err != nil {
		return nil, err
	}
	return api.RemoveDeal204Response{}, nil
}

func (s SalesAPI) ChangeOwnDeal(ctx context.Context, in api.ChangeOwnDealRequestObject) (api.ChangeOwnDealResponseObject, error) {
	if in.Body == nil || len(dealChanges(in.Body)) == 0 {
		return nil, say.New("send what to change")
	}
	if b := in.Body; b.Stage != nil || b.NextStep != nil || b.NextDate != nil || b.LostReason != nil {
		return nil, say.New("a conversation's own deal stands where its contact does: its stage, next step and why it was lost are the contact's")
	}
	q := request.ReqFrom(ctx)
	if err := s.Pipeline.SetDealFields(q.Org.ID, in.Id, dealChanges(in.Body), q.Actor()); err != nil {
		return nil, err
	}
	// Read back by itself, not off the board: someone who replied and then declined has
	// a deal that the board no longer shows.
	c, err := s.People.ContactByID(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	d, err := s.Pipeline.OwnDeal(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	return api.ChangeOwnDeal200JSONResponse(dealOut(*d, *c, true)), nil
}

// SalesAPI is the sales product's operations: the pipeline, subscriptions and the
// conversations board, which apiServer embeds.
type SalesAPI struct {
	Pipeline      *DealStore
	Subscriptions *SubscriptionStore
	People        salesPeople
	Onboarding    *onboarding.OnboardingStore
}

// salesPeople is what sales reads of the people its deals are with.
type salesPeople interface {
	ContactByID(org, id int64) (*store.Contact, error)
	Conversations(org int64) (map[string][]store.Contact, error)
}
