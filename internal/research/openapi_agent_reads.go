package research

import (
	"context"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/request"
)

// An agent's reads answer what the MCP tools of the same names do, carried into the
// spec's types by their JSON.

func (s ResearchAPI) AgentSearch(ctx context.Context, in api.AgentSearchRequestObject) (api.AgentSearchResponseObject, error) {
	q := request.ReqFrom(ctx)
	page, err := paging.PageIn(int(in.Params.Limit), string(in.Params.Cursor))
	if err != nil {
		return nil, err
	}
	hits, next, err := s.Research.AgentSearch(q.Org.ID, q.Agent(), in.Params.Q, page)
	if err != nil {
		return nil, err
	}
	out := api.AgentSearch200JSONResponse{Headers: api.AgentSearch200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	if out.Body.Hits, err = request.Recast[[]api.AgentSearchHit](hits); err != nil {
		return nil, err
	}
	out.Body.Hits = append([]api.AgentSearchHit{}, out.Body.Hits...)
	out.Body.NextCursor = next
	return out, nil
}

func (s ResearchAPI) AgentCompany(ctx context.Context, in api.AgentCompanyRequestObject) (api.AgentCompanyResponseObject, error) {
	q := request.ReqFrom(ctx)
	page, err := paging.PageIn(int(in.Params.Limit), string(in.Params.Cursor))
	if err != nil {
		return nil, err
	}
	c, err := s.Research.AgentCompany(q.Org.ID, q.Agent(), in.Id, page)
	if err != nil {
		return nil, err
	}
	body, err := request.Recast[api.AgentCompanyDetail](c)
	if err != nil {
		return nil, err
	}
	return api.AgentCompany200JSONResponse{Body: body, Headers: api.AgentCompany200ResponseHeaders{Link: request.NextLink(ctx, c.NextCursor)}}, nil
}

func (s ResearchAPI) AgentDue(ctx context.Context, in api.AgentDueRequestObject) (api.AgentDueResponseObject, error) {
	q := request.ReqFrom(ctx)
	page, err := paging.PageIn(int(in.Params.Limit), string(in.Params.Cursor))
	if err != nil {
		return nil, err
	}
	due, err := s.Research.AgentDue(q.Org, page)
	if err != nil {
		return nil, err
	}
	out := api.AgentDue200JSONResponse{Headers: api.AgentDue200ResponseHeaders{Link: request.NextLink(ctx, due.NextCursor)}}
	if out.Body.Items, err = request.Recast[[]api.AgentDueItem](due.Items); err != nil {
		return nil, err
	}
	out.Body.Items = append([]api.AgentDueItem{}, out.Body.Items...)
	out.Body.NextCursor = due.NextCursor
	return out, nil
}
