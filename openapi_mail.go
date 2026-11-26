package main

import (
	"context"
	"errors"

	"github.com/shutthegoatup/prospector/api"
)

func suppressionOut(s Suppression) api.Suppression {
	return api.Suppression{Id: s.ID, Address: s.Address, Reason: api.SuppressionReason(s.Reason), By: s.By, CreatedAt: s.CreatedAt, BouncedAt: s.BouncedAt}
}

func (s apiServer) ListSuppressions(ctx context.Context, in api.ListSuppressionsRequestObject) (api.ListSuppressionsResponseObject, error) {
	if in.Params.Limit < 0 {
		return nil, errors.New("limit is a whole number, 1 or more")
	}
	rows, next, err := s.a.s.Suppressions(reqFrom(ctx).Org.ID, PageIn{Limit: in.Params.Limit, Cursor: in.Params.Cursor})
	if err != nil {
		return nil, err
	}
	out := api.ListSuppressions200JSONResponse{Headers: api.ListSuppressions200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.Suppression{}, next
	for _, r := range rows {
		out.Body.Items = append(out.Body.Items, suppressionOut(r))
	}
	return out, nil
}

func (s apiServer) AddSuppression(ctx context.Context, in api.AddSuppressionRequestObject) (api.AddSuppressionResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send the address: {\"address\": …}")
	}
	q := reqFrom(ctx)
	row, err := s.a.s.Suppress(q.Org.ID, in.Body.Address, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.AddSuppression201JSONResponse(suppressionOut(*row)), nil
}

func (s apiServer) RemoveSuppression(ctx context.Context, in api.RemoveSuppressionRequestObject) (api.RemoveSuppressionResponseObject, error) {
	q := reqFrom(ctx)
	if err := s.a.s.Unsuppress(q.Org.ID, in.Id, q.Actor()); err != nil {
		return nil, err
	}
	return api.RemoveSuppression204Response{}, nil
}
