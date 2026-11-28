package main

import (
	"context"
	"time"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
)

func enricherOut(s *Store, e Enricher, now time.Time) api.Enricher {
	var fields []string
	if v := prospecting.Vendors[e.Kind]; v != nil {
		fields = v.Fields()
	}
	return api.Enricher{Id: e.ID, Kind: api.EnricherKind(e.Kind), Position: e.Position, MonthlyLimit: e.MonthlyLimit,
		UsedThisMonth: s.EnricherUsed(e.OrgID, e.Kind, now), Fields: nonNil(fields), HasKey: e.APIKey != "",
		UpdatedBy: e.UpdatedBy, UpdatedAt: e.UpdatedAt}
}

func (s apiServer) ListEnrichers(ctx context.Context, in api.ListEnrichersRequestObject) (api.ListEnrichersResponseObject, error) {
	list, err := s.a.s.Enrichers(request.ReqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	list, next, err := paging.ListPage(list, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListEnrichers200JSONResponse{Headers: api.ListEnrichers200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.Enricher{}, next
	now := time.Now()
	for _, e := range list {
		out.Body.Items = append(out.Body.Items, enricherOut(s.a.s, e, now))
	}
	return out, nil
}

func (s apiServer) SaveEnricher(ctx context.Context, in api.SaveEnricherRequestObject) (api.SaveEnricherResponseObject, error) {
	if in.Body == nil {
		return nil, say.New(`send the vendor, such as {"kind": "hunter", "api_key": "…", "monthly_limit": 100}`)
	}
	q := request.ReqFrom(ctx)
	e, err := s.a.s.SaveEnricher(q.Org.ID, prospecting.EnricherIn{Kind: string(in.Body.Kind), APIKey: in.Body.ApiKey,
		Position: in.Body.Position, MonthlyLimit: in.Body.MonthlyLimit}, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.SaveEnricher200JSONResponse(enricherOut(s.a.s, *e, time.Now())), nil
}

func (s apiServer) DeleteEnricher(ctx context.Context, in api.DeleteEnricherRequestObject) (api.DeleteEnricherResponseObject, error) {
	if err := s.a.s.DeleteEnricher(request.ReqFrom(ctx).Org.ID, in.Id); err != nil {
		return nil, err
	}
	return api.DeleteEnricher204Response{}, nil
}

func (s apiServer) EnrichContact(ctx context.Context, in api.EnrichContactRequestObject) (api.EnrichContactResponseObject, error) {
	if in.Body == nil {
		return nil, say.New(`send the fields to look for, such as {"fields": ["email"]}`)
	}
	q := request.ReqFrom(ctx)
	// Read afresh, so a never-contact entry added a moment ago holds.
	o, err := s.a.s.OrgBySlug(q.Org.Slug)
	if err != nil {
		return nil, err
	}
	who := Actor{Name: q.Actor()}
	if q.Key != nil {
		who.KeyID = q.Key.ID
	}
	fields := make([]string, len(in.Body.Fields))
	for i, f := range in.Body.Fields {
		fields[i] = string(f)
	}
	results, err := s.a.Enrich(ctx, o, who, EnrichIn{ContactID: in.Id, Fields: fields, Task: in.Body.Task})
	if err != nil {
		return nil, err
	}
	out := api.EnrichContact200JSONResponse{Results: []api.EnrichResult{}}
	for _, r := range results {
		out.Results = append(out.Results, api.EnrichResult{Field: r.Field, Vendor: r.Vendor, Outcome: api.EnrichResultOutcome(r.Outcome), Detail: r.Detail})
	}
	return out, nil
}
