package research

import (
	"context"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/prospecting"
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

// An agent's writes take what the MCP tools of the same names do, carried into the
// store's types by their JSON, and answer as those routes always did.

func (s ResearchAPI) AgentAddCompany(ctx context.Context, in api.AgentAddCompanyRequestObject) (api.AgentAddCompanyResponseObject, error) {
	q := request.ReqFrom(ctx)
	body, err := request.Recast[AddCompanyIn](in.Body)
	if err != nil {
		return nil, err
	}
	out, err := s.Research.AgentAddCompany(q.Org.ID, q.Agent(), body)
	if err != nil {
		return nil, err
	}
	answer, err := request.Recast[api.AgentAddCompanyOut](out)
	return api.AgentAddCompany200JSONResponse(answer), err
}

func (s ResearchAPI) AgentAddFact(ctx context.Context, in api.AgentAddFactRequestObject) (api.AgentAddFactResponseObject, error) {
	q := request.ReqFrom(ctx)
	body, err := request.Recast[AddFactIn](in.Body)
	if err != nil {
		return nil, err
	}
	out, err := s.Research.AgentAddFact(q.Org.ID, q.Agent(), body)
	if err != nil {
		return nil, err
	}
	answer, err := request.Recast[api.AgentAddFactOut](out)
	return api.AgentAddFact200JSONResponse(answer), err
}

func (s ResearchAPI) AgentAddContact(ctx context.Context, in api.AgentAddContactRequestObject) (api.AgentAddContactResponseObject, error) {
	q := request.ReqFrom(ctx)
	body, err := request.Recast[prospecting.AddContactIn](in.Body)
	if err != nil {
		return nil, err
	}
	out, err := s.Research.AgentAddContact(q.Org, q.Agent(), body)
	if err != nil {
		return nil, err
	}
	answer, err := request.Recast[api.AgentAddContactOut](out)
	return api.AgentAddContact200JSONResponse(answer), err
}

func (s ResearchAPI) AgentDraft(ctx context.Context, in api.AgentDraftRequestObject) (api.AgentDraftResponseObject, error) {
	q := request.ReqFrom(ctx)
	body, err := request.Recast[DraftIn](in.Body)
	if err != nil {
		return nil, err
	}
	if err := s.Research.AgentDraft(q.Org.ID, q.Agent(), body); err != nil {
		return nil, err
	}
	return api.AgentDraft200JSONResponse{Ok: true}, nil
}
