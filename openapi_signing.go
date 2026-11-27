package main

import (
	"bytes"
	"context"
	"errors"
	"mime"
	"net/url"
	"strings"

	"github.com/shutthegoatup/prospector/api"
)

// canSend is why this server can't send an organisation's agreements for signature:
// they go through the organisation's own mail server, with links to this server's
// public https address.
func (a *App) canSend(o *Org) string {
	via, err := a.campaignMail(o.ID)
	if err != nil {
		return err.Error()
	}
	if via == nil {
		return "agreements go out through your own mail server: an owner sets one up in Settings"
	}
	if u, err := url.Parse(a.base); a.base == "" || err != nil || u.Scheme != "https" {
		return "a signer's link needs this server's public https address (PROSPECTOR_URL)"
	}
	return ""
}

func whoFrom(ctx context.Context) Who {
	r := requestFrom(ctx)
	return Who{IP: clientIP(r), Browser: browserOf(r)}
}

func (s apiServer) SendAgreement(ctx context.Context, in api.SendAgreementRequestObject) (api.SendAgreementResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send {} to send it with the usual subject and 14 days")
	}
	q := reqFrom(ctx)
	if why := s.a.canSend(q.Org); why != "" {
		return nil, errors.New(why)
	}
	ag, err := s.a.s.SendAgreement(q.Org, in.Id, SendIn{Subject: in.Body.Subject, Message: in.Body.Message, Days: in.Body.Days}, q.User, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.SendAgreement200JSONResponse(agreementOut(*ag)), nil
}

func (s apiServer) VoidAgreement(ctx context.Context, in api.VoidAgreementRequestObject) (api.VoidAgreementResponseObject, error) {
	reason := ""
	if in.Body != nil {
		reason = in.Body.Reason
	}
	q := reqFrom(ctx)
	ag, err := s.a.s.VoidAgreement(q.Org.ID, in.Id, reason, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.VoidAgreement200JSONResponse(agreementOut(*ag)), nil
}

func (s apiServer) ResendAgreementLink(ctx context.Context, in api.ResendAgreementLinkRequestObject) (api.ResendAgreementLinkResponseObject, error) {
	q := reqFrom(ctx)
	if why := s.a.canSend(q.Org); why != "" {
		return nil, errors.New(why)
	}
	if err := s.a.s.ResendLink(q.Org.ID, in.Id, in.Recipient, q.Actor()); err != nil {
		return nil, err
	}
	return api.ResendAgreementLink204Response{}, nil
}

func signInOf(typed string, consent bool, values []struct {
	Field int64  `json:"field"`
	Value string `json:"value"`
}) SignIn {
	out := SignIn{Typed: typed, Consent: consent, Values: map[int64]string{}}
	for _, v := range values {
		out.Values[v.Field] = v.Value
	}
	return out
}

func (s apiServer) SignAgreementAsMember(ctx context.Context, in api.SignAgreementAsMemberRequestObject) (api.SignAgreementAsMemberResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send {\"typed_name\": …, \"consent\": true}")
	}
	q, err := someone(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.a.s.MemberSign(q.Org, in.Id, q.User, signInOf(in.Body.TypedName, in.Body.Consent, in.Body.Values), whoFrom(ctx)); err != nil {
		return nil, err
	}
	ag, err := s.a.s.Agreement(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	return api.SignAgreementAsMember200JSONResponse(agreementOut(*ag)), nil
}

func (s apiServer) DeclineAgreementAsMember(ctx context.Context, in api.DeclineAgreementAsMemberRequestObject) (api.DeclineAgreementAsMemberResponseObject, error) {
	q, err := someone(ctx)
	if err != nil {
		return nil, err
	}
	reason := ""
	if in.Body != nil {
		reason = in.Body.Reason
	}
	if _, err := s.a.s.MemberDecline(q.Org, in.Id, q.User, reason, whoFrom(ctx)); err != nil {
		return nil, err
	}
	ag, err := s.a.s.Agreement(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	return api.DeclineAgreementAsMember200JSONResponse(agreementOut(*ag)), nil
}

func (s apiServer) GetSealedAgreement(ctx context.Context, in api.GetSealedAgreementRequestObject) (api.GetSealedAgreementResponseObject, error) {
	ag, data, err := s.a.s.SealedDocument(reqFrom(ctx).Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	return api.GetSealedAgreement200ApplicationpdfResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data)),
		Headers: api.GetSealedAgreement200ResponseHeaders{ContentDisposition: sealedName(ag)}}, nil
}

// sealedName is how a sealed copy downloads: the document's name, marked signed.
func sealedName(ag *Agreement) string {
	name := strings.TrimSuffix(ag.FileName, ".pdf")
	name = strings.TrimSuffix(name, ".PDF")
	return mime.FormatMediaType("attachment", map[string]string{"filename": fileName(name + " (signed).pdf")})
}

func (s apiServer) ListAgreementEvents(ctx context.Context, in api.ListAgreementEventsRequestObject) (api.ListAgreementEventsResponseObject, error) {
	q := reqFrom(ctx)
	if _, err := s.a.s.Agreement(q.Org.ID, in.Id); err != nil {
		return nil, err
	}
	all, err := s.a.s.AgreementEvents(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	page, next, err := listPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListAgreementEvents200JSONResponse{Headers: api.ListAgreementEvents200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.AgreementEvent{}, next
	for _, e := range page {
		out.Body.Items = append(out.Body.Items, api.AgreementEvent{Id: e.ID, At: e.At, Kind: e.Kind, Actor: e.Actor, RecipientId: e.RecipientID, Detail: e.Detail, Hash: e.Hash})
	}
	return out, nil
}
