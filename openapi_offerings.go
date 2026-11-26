package main

import (
	"context"
	"errors"

	"github.com/shutthegoatup/prospector/api"
)

func offeringOut(o Offering) api.Offering {
	return api.Offering{Id: o.ID, Name: o.Name, LookFor: o.LookFor, Pitch: o.Pitch, NoteTemplate: o.NoteTemplate,
		UpdatedBy: o.UpdatedBy, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt}
}

func verdictOut(v VerdictRow) api.Verdict {
	return api.Verdict{OfferingId: v.OfferingID, CompanyId: v.CompanyID, Company: v.Company, Stage: api.VerdictStage(v.Stage),
		Fit: v.Fit, Reason: v.Reason, By: v.By, UpdatedAt: v.UpdatedAt}
}

func (s apiServer) ListOfferings(ctx context.Context, in api.ListOfferingsRequestObject) (api.ListOfferingsResponseObject, error) {
	rows, err := s.a.s.Offerings(reqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	rows, next, err := listPage(rows, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListOfferings200JSONResponse{Headers: api.ListOfferings200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.Offering{}, next
	for _, o := range rows {
		out.Body.Items = append(out.Body.Items, offeringOut(o))
	}
	return out, nil
}

func (s apiServer) AddOffering(ctx context.Context, in api.AddOfferingRequestObject) (api.AddOfferingResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send the offering: {\"name\": …}")
	}
	q, b := reqFrom(ctx), in.Body
	o, err := s.a.s.AddOffering(q.Org.ID, Offering{Name: b.Name, LookFor: b.LookFor, Pitch: b.Pitch, NoteTemplate: b.NoteTemplate}, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.AddOffering201JSONResponse(offeringOut(*o)), nil
}

func (s apiServer) ChangeOffering(ctx context.Context, in api.ChangeOfferingRequestObject) (api.ChangeOfferingResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send what changes, such as {\"name\": …}")
	}
	q, c := reqFrom(ctx), in.Body
	o, err := s.a.s.Offering(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	put(&o.Name, c.Name)
	put(&o.LookFor, c.LookFor)
	put(&o.Pitch, c.Pitch)
	put(&o.NoteTemplate, c.NoteTemplate)
	if o, err = s.a.s.ChangeOffering(q.Org.ID, *o, q.Actor()); err != nil {
		return nil, err
	}
	return api.ChangeOffering200JSONResponse(offeringOut(*o)), nil
}

func (s apiServer) RemoveOffering(ctx context.Context, in api.RemoveOfferingRequestObject) (api.RemoveOfferingResponseObject, error) {
	q := reqFrom(ctx)
	if err := s.a.s.RemoveOffering(q.Org.ID, in.Id, q.Actor()); err != nil {
		return nil, err
	}
	return api.RemoveOffering204Response{}, nil
}

func (s apiServer) ListVerdicts(ctx context.Context, in api.ListVerdictsRequestObject) (api.ListVerdictsResponseObject, error) {
	rows, err := s.a.s.Judged(reqFrom(ctx).Org.ID, in.Id, string(in.Params.Stage))
	if err != nil {
		return nil, err
	}
	rows, next, err := listPage(rows, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListVerdicts200JSONResponse{Headers: api.ListVerdicts200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.Verdict{}, next
	for _, v := range rows {
		out.Body.Items = append(out.Body.Items, verdictOut(v))
	}
	return out, nil
}

func (s apiServer) ListCompanyVerdicts(ctx context.Context, in api.ListCompanyVerdictsRequestObject) (api.ListCompanyVerdictsResponseObject, error) {
	rows, err := s.a.s.Verdicts(reqFrom(ctx).Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	rows, next, err := listPage(rows, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListCompanyVerdicts200JSONResponse{Headers: api.ListCompanyVerdicts200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.Verdict{}, next
	for _, v := range rows {
		out.Body.Items = append(out.Body.Items, verdictOut(v))
	}
	return out, nil
}
