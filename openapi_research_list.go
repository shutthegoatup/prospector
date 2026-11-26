package main

import (
	"context"

	"github.com/shutthegoatup/prospector/api"
)

func (s apiServer) ListResearch(ctx context.Context, in api.ListResearchRequestObject) (api.ListResearchResponseObject, error) {
	q := reqFrom(ctx)
	page, err := pageIn(int(in.Params.Limit), string(in.Params.Cursor))
	if err != nil {
		return nil, err
	}
	filter := string(in.Params.Filter)
	if filter == "" {
		filter = "review"
	}
	if in.Params.Task != 0 {
		if _, err := s.a.s.taskRow(q.Org.ID, in.Params.Task); err != nil {
			return nil, missing(err, "there's no task %d here", in.Params.Task)
		}
	}
	rows, ranked, next, err := s.a.s.researchList(q.Org.ID, q.Org.Settings, filter, in.Params.Task, page)
	if err != nil {
		return nil, err
	}
	out := api.ListResearch200JSONResponse{Headers: api.ListResearch200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.Ranked, out.Body.NextCursor = []api.ResearchCompany{}, ranked, next
	for _, r := range rows {
		out.Body.Items = append(out.Body.Items, researchOut(q.Org.Settings, r))
	}
	return out, nil
}

func researchOut(set OrgSettings, r ResearchRow) api.ResearchCompany {
	found := assetsFound(set, r.Facts)
	fit := fitOf(found)
	c := api.ResearchCompany{Id: r.ID, Name: r.Name, Domain: r.Domain, Country: r.Country, Segment: r.Segment,
		Stage: api.ResearchCompanyStage(r.Stage), Grade: r.Fit, FoundBy: r.FoundBy, CreatedAt: r.CreatedAt,
		Criteria: []api.Criterion{}, OtherFacts: len(otherFacts(set, r.Facts)), People: len(r.People),
		Fit: api.Fit{Of: fit.Of, Met: fit.Met, NotFound: fit.NotFound, Unchecked: fit.Unchecked, MustsMissed: fit.MustsMissed, Text: fit.Text()}}
	for _, a := range found {
		cr := api.Criterion{Field: a.Field, Must: a.Must, State: api.CriterionState(a.State)}
		if f := a.Fact; f != nil {
			at := f.CreatedAt
			cr.Value, cr.SourceUrl, cr.Inferred, cr.By, cr.At = f.Value, f.SourceURL, f.Inferred, f.Actor, &at
		}
		c.Criteria = append(c.Criteria, cr)
	}
	return c
}
