package main

import (
	"context"
	"errors"

	"github.com/shutthegoatup/prospector/api"
)

// dealOut is a deal as the API gives it. A conversation's own stands where its contact
// does, and has a kind before anything is stored for it.
func dealOut(d Deal, with Contact, own bool) api.Deal {
	out := api.Deal{Id: d.ID, ContactId: with.ID, Person: displayName(with), CompanyId: idOf(with.CompanyID), Seq: d.Seq, Kind: d.Kind,
		Stage: d.Stage, NextStep: d.NextStep, NextDate: deref(d.NextDate), LostReason: d.LostReason, Amount: d.Amount, Currency: d.Currency,
		Value: d.Value(), CloseDate: deref(d.CloseDate), Owner: d.Owner, SubscriptionId: d.SubscriptionID}
	if own {
		out.Stage, out.NextStep, out.NextDate, out.LostReason = with.ConversationStage(), with.NextStep, deref(with.NextDate), with.LostReason
		if out.Kind == "" {
			out.Kind = DealNew
		}
	}
	return out
}

func (s apiServer) deals(org int64) ([]api.Deal, error) {
	convs, err := s.a.s.Conversations(org)
	if err != nil {
		return nil, err
	}
	own, err := s.a.s.Deals(org)
	if err != nil {
		return nil, err
	}
	others, err := s.a.s.OtherDeals(org)
	if err != nil {
		return nil, err
	}
	out := []api.Deal{}
	for _, st := range Stages {
		for _, c := range convs[st.Key] {
			out = append(out, dealOut(own[c.ID], c, true))
		}
		for _, o := range others[st.Key] {
			out = append(out, dealOut(o.Deal, o.With, false))
		}
	}
	return out, nil
}

// deal finds one in the list: a conversation's own by its contact, any other by its id.
func (s apiServer) deal(org int64, own bool, id int64) (api.Deal, error) {
	all, err := s.deals(org)
	if err != nil {
		return api.Deal{}, err
	}
	for _, d := range all {
		if (own && d.Seq == 0 && d.ContactId == id) || (!own && d.Seq != 0 && d.Id == id) {
			return d, nil
		}
	}
	return api.Deal{}, noSuch("no deal %d", id)
}

func dealChanges(b *api.DealChange) []Change {
	var out []Change
	for _, set := range []struct {
		field string
		value *string
	}{{"kind", b.Kind}, {"amount", b.Amount}, {"close_date", b.CloseDate}, {"owner", b.Owner}, {"stage", b.Stage},
		{"next_step", b.NextStep}, {"next_date", b.NextDate}, {"lost_reason", b.LostReason}} {
		if set.value != nil {
			out = append(out, Change{set.field, *set.value})
		}
	}
	return out
}

func (s apiServer) ListDeals(ctx context.Context, in api.ListDealsRequestObject) (api.ListDealsResponseObject, error) {
	all, err := s.deals(reqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	page, next, err := listPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListDeals200JSONResponse{Headers: api.ListDeals200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = append([]api.Deal{}, page...), next
	return out, nil
}

func (s apiServer) StartDeal(ctx context.Context, in api.StartDealRequestObject) (api.StartDealResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("say who it is with: {\"contact\": id}")
	}
	q := reqFrom(ctx)
	kind := in.Body.Kind
	if kind == "" {
		kind = DealNew
	}
	d, err := s.a.s.StartDeal(q.Org.ID, in.Body.Contact, kind, q.Actor())
	if err != nil {
		return nil, err
	}
	out, err := s.deal(q.Org.ID, false, d.ID)
	if err != nil {
		return nil, err
	}
	return api.StartDeal201JSONResponse(out), nil
}

func (s apiServer) ChangeDeal(ctx context.Context, in api.ChangeDealRequestObject) (api.ChangeDealResponseObject, error) {
	if in.Body == nil || len(dealChanges(in.Body)) == 0 {
		return nil, errors.New("send what to change")
	}
	q := reqFrom(ctx)
	if err := s.a.s.ChangeDealFields(q.Org.ID, in.Id, dealChanges(in.Body), q.Actor()); err != nil {
		return nil, err
	}
	out, err := s.deal(q.Org.ID, false, in.Id)
	if err != nil {
		return nil, err
	}
	return api.ChangeDeal200JSONResponse(out), nil
}

func (s apiServer) RemoveDeal(ctx context.Context, in api.RemoveDealRequestObject) (api.RemoveDealResponseObject, error) {
	q := reqFrom(ctx)
	if err := s.a.s.RemoveDeal(q.Org.ID, in.Id, q.Actor()); err != nil {
		return nil, err
	}
	return api.RemoveDeal204Response{}, nil
}

func (s apiServer) ChangeOwnDeal(ctx context.Context, in api.ChangeOwnDealRequestObject) (api.ChangeOwnDealResponseObject, error) {
	if in.Body == nil || len(dealChanges(in.Body)) == 0 {
		return nil, errors.New("send what to change")
	}
	if b := in.Body; b.Stage != nil || b.NextStep != nil || b.NextDate != nil || b.LostReason != nil {
		return nil, errors.New("a conversation's own deal stands where its contact does: its stage, next step and why it was lost are the contact's")
	}
	q := reqFrom(ctx)
	if err := s.a.s.SetDealFields(q.Org.ID, in.Id, dealChanges(in.Body), q.Actor()); err != nil {
		return nil, err
	}
	// Read back by itself, not off the board: someone who replied and then declined has
	// a deal that the board no longer shows.
	var c Contact
	if err := s.a.s.in(q.Org.ID).Where("id = ?", in.Id).First(&c).Error; err != nil {
		return nil, missing(err, "no contact %d", in.Id)
	}
	var d Deal
	if err := s.a.s.in(q.Org.ID).Where("contact_id = ? and seq = 0", in.Id).Limit(1).Find(&d).Error; err != nil {
		return nil, err
	}
	return api.ChangeOwnDeal200JSONResponse(dealOut(d, c, true)), nil
}

func enquiryOut(e Enquiry) api.Enquiry {
	return api.Enquiry{Id: e.ID, Kind: e.Kind, Name: e.Name, Email: e.Email, Company: e.Company, Phone: e.Phone, Message: e.Message, Site: e.Site,
		Page: e.Page, Via: e.Via, Spam: e.Spam, ContactId: idOf(e.ContactID), CompanyId: idOf(e.CompanyID), CreatedAt: e.CreatedAt,
		State: e.State, Owner: e.Owner, Note: e.Note}
}

func (s apiServer) ListEnquiries(ctx context.Context, in api.ListEnquiriesRequestObject) (api.ListEnquiriesResponseObject, error) {
	p := in.Params
	page, err := pageIn(p.Limit, p.Cursor)
	if err != nil {
		return nil, err
	}
	rows, next, err := s.a.s.enquiriesBefore(reqFrom(ctx).Org.ID, EnquiryFilter{Kind: p.Kind, Spam: p.Spam, State: p.State}, page)
	if err != nil {
		return nil, err
	}
	out := api.ListEnquiries200JSONResponse{Headers: api.ListEnquiries200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.Enquiry{}, next
	for _, e := range rows {
		out.Body.Items = append(out.Body.Items, enquiryOut(e))
	}
	return out, nil
}

func (s apiServer) ChangeEnquiry(ctx context.Context, in api.ChangeEnquiryRequestObject) (api.ChangeEnquiryResponseObject, error) {
	var changes []Change
	if b := in.Body; b != nil {
		for _, set := range []struct {
			field string
			value *string
		}{{"state", b.State}, {"owner", b.Owner}, {"note", b.Note}} {
			if set.value != nil {
				changes = append(changes, Change{set.field, *set.value})
			}
		}
	}
	if len(changes) == 0 {
		return nil, errors.New("send what to change: state, owner or note")
	}
	q := reqFrom(ctx)
	if err := s.a.s.SetEnquiryFields(q.Org.ID, in.Id, changes, q.Actor()); err != nil {
		return nil, err
	}
	var e Enquiry
	if err := s.a.s.in(q.Org.ID).Where("id = ?", in.Id).First(&e).Error; err != nil {
		return nil, missing(err, "no enquiry %d", in.Id)
	}
	return api.ChangeEnquiry200JSONResponse(enquiryOut(e)), nil
}

func (s apiServer) DeleteEnquiry(ctx context.Context, in api.DeleteEnquiryRequestObject) (api.DeleteEnquiryResponseObject, error) {
	if err := s.a.s.DeleteEnquiry(reqFrom(ctx).Org.ID, in.Id); err != nil {
		return nil, err
	}
	return api.DeleteEnquiry204Response{}, nil
}

func (s apiServer) ConverseEnquiry(ctx context.Context, in api.ConverseEnquiryRequestObject) (api.ConverseEnquiryResponseObject, error) {
	q := reqFrom(ctx)
	c, err := s.a.s.ConverseEnquiry(q.Org, in.Id, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.ConverseEnquiry201JSONResponse{Contact: c.ID}, nil
}

// idOf is an optional id as the API gives one: 0 for none.
func idOf(id *int64) int64 {
	if id == nil {
		return 0
	}
	return *id
}
