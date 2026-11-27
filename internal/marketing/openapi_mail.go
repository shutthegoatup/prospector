package marketing

import (
	"context"

	"github.com/shutthegoatup/prospector/api"

	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

func suppressionOut(s store.Suppression) api.Suppression {
	return api.Suppression{Id: s.ID, Address: s.Address, Reason: api.SuppressionReason(s.Reason), By: s.By, CreatedAt: s.CreatedAt, BouncedAt: s.BouncedAt}
}

func (s MarketingAPI) ListSuppressions(ctx context.Context, in api.ListSuppressionsRequestObject) (api.ListSuppressionsResponseObject, error) {
	if in.Params.Limit < 0 {
		return nil, say.New("limit is a whole number, 1 or more")
	}
	rows, next, err := s.Suppression.Suppressions(request.ReqFrom(ctx).Org.ID, paging.Page{Limit: in.Params.Limit, Cursor: in.Params.Cursor})
	if err != nil {
		return nil, err
	}
	out := api.ListSuppressions200JSONResponse{Headers: api.ListSuppressions200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.Suppression{}, next
	for _, r := range rows {
		out.Body.Items = append(out.Body.Items, suppressionOut(r))
	}
	return out, nil
}

func (s MarketingAPI) AddSuppression(ctx context.Context, in api.AddSuppressionRequestObject) (api.AddSuppressionResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send the address: {\"address\": …}")
	}
	q := request.ReqFrom(ctx)
	row, err := s.Suppression.Suppress(q.Org.ID, in.Body.Address, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.AddSuppression201JSONResponse(suppressionOut(*row)), nil
}

func (s MarketingAPI) RemoveSuppression(ctx context.Context, in api.RemoveSuppressionRequestObject) (api.RemoveSuppressionResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.Suppression.Unsuppress(q.Org.ID, in.Id, q.Actor()); err != nil {
		return nil, err
	}
	return api.RemoveSuppression204Response{}, nil
}
