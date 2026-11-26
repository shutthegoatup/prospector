package main

import (
	"context"
	"errors"

	"github.com/shutthegoatup/prospector/api"
)

func cohortOut(c Cohort) api.Cohort {
	out := api.Cohort{Id: c.ID, Name: c.Name, Position: c.Position, Colour: c.Colour, WhoTheyAre: c.WhoTheyAre, CatchAll: c.CatchAll,
		Rules: []api.CohortRule{}, Companies: c.Companies, ByHand: c.ByHand, UpdatedBy: c.UpdatedBy, UpdatedAt: c.UpdatedAt}
	for _, r := range c.Rules {
		out.Rules = append(out.Rules, api.CohortRule{Field: api.CohortRuleField(r.Field), Op: api.CohortRuleOp(r.Op), Value: r.Value})
	}
	return out
}

func rulesIn(rules []api.CohortRule) []CohortRule {
	out := []CohortRule{}
	for _, r := range rules {
		out = append(out, CohortRule{Field: string(r.Field), Op: string(r.Op), Value: r.Value})
	}
	return out
}

func cohortChangeIn(c api.CohortChange) CohortChange {
	out := CohortChange{Name: c.Name, Colour: c.Colour, WhoTheyAre: c.WhoTheyAre, CatchAll: c.CatchAll}
	if c.Rules != nil {
		rules := rulesIn(*c.Rules)
		out.Rules = &rules
	}
	return out
}

// cohortRefs names each cohort as a company carries it.
func (s apiServer) cohortRefs(org int64) (map[int64]api.CohortRef, error) {
	cohorts, err := loadCohorts(s.a.s.db, org)
	if err != nil {
		return nil, err
	}
	out := map[int64]api.CohortRef{}
	for _, c := range cohorts {
		out[c.ID] = api.CohortRef{Id: c.ID, Name: c.Name, Colour: c.Colour}
	}
	return out, nil
}

func cohortRef(refs map[int64]api.CohortRef, id int64) api.CohortRef {
	if r, ok := refs[id]; ok {
		return r
	}
	return api.CohortRef{}
}

func (s apiServer) ListCohorts(ctx context.Context, in api.ListCohortsRequestObject) (api.ListCohortsResponseObject, error) {
	rows, err := s.a.s.Cohorts(reqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	rows, next, err := listPage(rows, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListCohorts200JSONResponse{Headers: api.ListCohorts200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.Cohort{}, next
	for _, c := range rows {
		out.Body.Items = append(out.Body.Items, cohortOut(c))
	}
	return out, nil
}

func (s apiServer) CreateCohort(ctx context.Context, in api.CreateCohortRequestObject) (api.CreateCohortResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send the cohort: {\"name\": …}")
	}
	q, b := reqFrom(ctx), in.Body
	rules := rulesIn(b.Rules)
	change := CohortChange{Name: &b.Name, Colour: &b.Colour, WhoTheyAre: &b.WhoTheyAre, Rules: &rules}
	if b.CatchAll {
		change.CatchAll = &b.CatchAll
	}
	c, err := s.a.s.AddCohort(q.Org.ID, change, q.Actor())
	if err != nil {
		return nil, err
	}
	s.a.dropMCPServers()
	return api.CreateCohort201JSONResponse(cohortOut(*c)), nil
}

func (s apiServer) UpdateCohort(ctx context.Context, in api.UpdateCohortRequestObject) (api.UpdateCohortResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send what changes, such as {\"name\": …}")
	}
	q := reqFrom(ctx)
	c, err := s.a.s.ChangeCohort(q.Org.ID, in.Id, cohortChangeIn(*in.Body), q.Actor())
	if err != nil {
		return nil, err
	}
	s.a.dropMCPServers()
	return api.UpdateCohort200JSONResponse(cohortOut(*c)), nil
}

func (s apiServer) MoveCohort(ctx context.Context, in api.MoveCohortRequestObject) (api.MoveCohortResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send where it goes: {\"position\": 0}")
	}
	q := reqFrom(ctx)
	if err := s.a.s.MoveCohort(q.Org.ID, in.Id, in.Body.Position, q.Actor()); err != nil {
		return nil, err
	}
	s.a.dropMCPServers()
	return api.MoveCohort204Response{}, nil
}

func (s apiServer) DeleteCohort(ctx context.Context, in api.DeleteCohortRequestObject) (api.DeleteCohortResponseObject, error) {
	q := reqFrom(ctx)
	if err := s.a.s.RemoveCohort(q.Org.ID, in.Id, q.Actor()); err != nil {
		return nil, err
	}
	s.a.dropMCPServers()
	return api.DeleteCohort204Response{}, nil
}

func (s apiServer) PreviewCohort(ctx context.Context, in api.PreviewCohortRequestObject) (api.PreviewCohortResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send the cohort as it would be: {\"cohort\": 0, \"change\": {\"name\": …}}")
	}
	q := reqFrom(ctx)
	p, err := s.a.s.PreviewCohort(q.Org.ID, in.Body.Cohort, cohortChangeIn(in.Body.Change))
	if err != nil {
		return nil, err
	}
	refs, err := s.cohortRefs(q.Org.ID)
	if err != nil {
		return nil, err
	}
	// The cohort previewed is named as it would be, and a new one has no id yet.
	draft := api.CohortRef{Id: p.Cohort.ID, Name: p.Cohort.Name, Colour: p.Cohort.Colour}
	ref := func(id int64, name string) api.CohortRef {
		if id == p.Cohort.ID && name == p.Cohort.Name {
			return draft
		}
		return cohortRef(refs, id)
	}
	moves := func(rows []CohortMoveRow) []api.CohortMove {
		out := []api.CohortMove{}
		for _, m := range rows {
			out = append(out, api.CohortMove{CompanyId: m.CompanyID, Company: m.Company, From: ref(m.From, m.FromName), To: ref(m.To, m.ToName)})
		}
		return out
	}
	return api.PreviewCohort200JSONResponse{MovingIn: moves(p.In), MovingOut: moves(p.Out), KeptByHand: moves(p.Kept), OtherCurrencyDeals: p.OtherCurrency}, nil
}

func (s apiServer) SetCompanyCohort(ctx context.Context, in api.SetCompanyCohortRequestObject) (api.SetCompanyCohortResponseObject, error) {
	if in.Body == nil || (in.Body.Cohort == 0) == !in.Body.ByRule {
		return nil, errors.New("send {\"cohort\": id} to put it there by hand, or {\"by_rule\": true}")
	}
	q := reqFrom(ctx)
	var err error
	if in.Body.ByRule {
		err = s.a.s.CompanyByRule(q.Org.ID, in.Id, q.Actor())
	} else {
		err = s.a.s.SetCompanyCohort(q.Org.ID, in.Id, in.Body.Cohort, q.Actor())
	}
	if err != nil {
		return nil, err
	}
	return api.SetCompanyCohort204Response{}, nil
}
