package main

import (
	"context"
	"strings"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/say"
)

func messageOut(m EnquiryMessage) api.EnquiryMessage {
	return api.EnquiryMessage{Id: m.ID, EnquiryId: m.EnquiryID, Ours: m.Ours, Text: m.Text, By: m.By, SentAt: m.SentAt, CreatedAt: m.CreatedAt}
}

func (s apiServer) ListEnquiryMessages(ctx context.Context, in api.ListEnquiryMessagesRequestObject) (api.ListEnquiryMessagesResponseObject, error) {
	thread, err := s.a.s.EnquiryThread(reqFrom(ctx).Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	thread, next, err := listPage(thread, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListEnquiryMessages200JSONResponse{Headers: api.ListEnquiryMessages200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.EnquiryMessage{}, next
	for _, m := range thread {
		out.Body.Items = append(out.Body.Items, messageOut(m))
	}
	return out, nil
}

func (s apiServer) ReplyToEnquiry(ctx context.Context, in api.ReplyToEnquiryRequestObject) (api.ReplyToEnquiryResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send the reply: {\"text\": …}")
	}
	q := reqFrom(ctx)
	// Their answer goes to the member who wrote. A key has no mailbox of its own.
	answers := ""
	if q.Key == nil && q.User != nil {
		answers = q.User.Email
	}
	m, err := s.a.replyEnquiry(ctx, q.Org, in.Id, in.Body.Text, q.Actor(), answers)
	if err != nil {
		return nil, err
	}
	return api.ReplyToEnquiry201JSONResponse(messageOut(*m)), nil
}

func (s apiServer) RecordEnquiryAnswer(ctx context.Context, in api.RecordEnquiryAnswerRequestObject) (api.RecordEnquiryAnswerResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send what they said: {\"text\": …}")
	}
	q := reqFrom(ctx)
	m, err := s.a.s.HearEnquiry(q.Org.ID, in.Id, in.Body.Text, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.RecordEnquiryAnswer201JSONResponse(messageOut(*m)), nil
}

func (s apiServer) CountEnquiries(ctx context.Context, _ api.CountEnquiriesRequestObject) (api.CountEnquiriesResponseObject, error) {
	out := api.CountEnquiries200JSONResponse{Kinds: map[string]int{}, States: map[string]int{}}
	for key, n := range s.a.s.EnquiryCounts(reqFrom(ctx).Org.ID) {
		state, isState := strings.CutPrefix(key, "state:")
		switch {
		case key == "":
			out.All = n
		case key == "spam":
			out.Spam = n
		case isState:
			out.States[state] = n
		default:
			out.Kinds[key] = n
		}
	}
	return out, nil
}

func enquiryOut(e Enquiry, around enquiriesAround) api.Enquiry {
	out := api.Enquiry{Id: e.ID, Kind: e.Kind, Name: e.Name, Email: e.Email, Company: e.Company, Phone: e.Phone, Message: e.Message, Site: e.Site,
		Page: e.Page, Via: e.Via, Spam: e.Spam, ContactId: idOf(e.ContactID), CompanyId: idOf(e.CompanyID), CreatedAt: e.CreatedAt,
		State: e.State, Owner: e.Owner, Note: e.Note, CompanyName: around.Companies[idOf(e.CompanyID)],
		InConversation: e.ContactID != nil && around.Talking[*e.ContactID], Thread: []api.EnquiryMessage{}}
	for _, m := range around.Threads[e.ID] {
		out.Thread = append(out.Thread, messageOut(m))
	}
	return out
}

func (s apiServer) ListEnquiries(ctx context.Context, in api.ListEnquiriesRequestObject) (api.ListEnquiriesResponseObject, error) {
	p := in.Params
	page, err := pageIn(p.Limit, p.Cursor)
	if err != nil {
		return nil, err
	}
	org := reqFrom(ctx).Org.ID
	rows, next, err := s.a.s.EnquiriesBefore(org, EnquiryFilter{Kind: p.Kind, Spam: p.Spam, State: p.State}, page)
	if err != nil {
		return nil, err
	}
	around, err := s.a.s.AroundEnquiries(org, rows)
	if err != nil {
		return nil, err
	}
	via, _ := s.a.mailFor(org)
	out := api.ListEnquiries200JSONResponse{Headers: api.ListEnquiries200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor, out.Body.CanReply = []api.Enquiry{}, next, via != nil
	for _, e := range rows {
		out.Body.Items = append(out.Body.Items, enquiryOut(e, around))
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
				changes = append(changes, Change{Field: set.field, Value: *set.value})
			}
		}
	}
	if len(changes) == 0 {
		return nil, say.New("send what to change: state, owner or note")
	}
	q := reqFrom(ctx)
	if err := s.a.s.SetEnquiryFields(q.Org.ID, in.Id, changes, q.Actor()); err != nil {
		return nil, err
	}
	e, err := s.a.s.EnquiryByID(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	around, err := s.a.s.AroundEnquiries(q.Org.ID, []Enquiry{*e})
	if err != nil {
		return nil, err
	}
	return api.ChangeEnquiry200JSONResponse(enquiryOut(*e, around)), nil
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
