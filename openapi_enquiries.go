package main

import (
	"context"
	"errors"

	"github.com/shutthegoatup/prospector/api"
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
		return nil, errors.New("send the reply: {\"text\": …}")
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
		return nil, errors.New("send what they said: {\"text\": …}")
	}
	q := reqFrom(ctx)
	m, err := s.a.s.HearEnquiry(q.Org.ID, in.Id, in.Body.Text, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.RecordEnquiryAnswer201JSONResponse(messageOut(*m)), nil
}

func (s apiServer) ListMembers(ctx context.Context, in api.ListMembersRequestObject) (api.ListMembersResponseObject, error) {
	members, err := s.a.s.Members(reqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	members, next, err := listPage(members, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListMembers200JSONResponse{Headers: api.ListMembers200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.MemberName{}, next
	for _, m := range members {
		out.Body.Items = append(out.Body.Items, api.MemberName{Name: m.Name})
	}
	return out, nil
}
