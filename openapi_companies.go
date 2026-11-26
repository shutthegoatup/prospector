package main

import (
	"context"
	"fmt"
	"slices"

	"github.com/shutthegoatup/prospector/api"
)

func (s apiServer) ListCompanies(ctx context.Context, in api.ListCompaniesRequestObject) (api.ListCompaniesResponseObject, error) {
	q, p := reqFrom(ctx), in.Params
	if p.Stage != "" && !slices.Contains(pipelineStages, string(p.Stage)) {
		return nil, fmt.Errorf("no stage %q: one of %v", p.Stage, pipelineStages)
	}
	page, err := pageIn(int(p.Limit), string(p.Cursor))
	if err != nil {
		return nil, err
	}
	rows, total, next, err := s.a.s.companiesList(q.Org.ID, p.Q, p.Segment, string(p.Stage), page)
	if err != nil {
		return nil, err
	}
	out := api.ListCompanies200JSONResponse{Headers: api.ListCompanies200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor, out.Body.Total = []api.CompanyRow{}, next, total
	for _, r := range rows {
		out.Body.Items = append(out.Body.Items, api.CompanyRow{Id: r.ID, Name: r.Name, Country: r.Country, Segment: r.Segment,
			Grade: r.Fit, People: r.People, Pipeline: api.Pipeline(r.Pipeline)})
	}
	return out, nil
}

func (s apiServer) ListSegments(ctx context.Context, in api.ListSegmentsRequestObject) (api.ListSegmentsResponseObject, error) {
	all, err := s.a.s.segments(reqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	page, next, err := listPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListSegments200JSONResponse{Headers: api.ListSegments200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = append([]string{}, page...), next
	return out, nil
}

func pendingOut(d PendingDetail) api.PendingDetail {
	return api.PendingDetail{Id: d.ID, Field: d.Field, Value: d.Value, Current: d.Current, SourceUrl: d.SourceURL, By: d.By, At: d.At, EmailStatus: d.EmailStatus, Vendor: d.Vendor}
}

func factOut(f Fact) api.ResearchFact {
	return api.ResearchFact{Id: f.ID, Field: f.Field, Value: f.Value, SourceUrl: f.SourceURL, Answer: factAnswerOut(f.Answer), Inferred: f.Inferred, By: f.Actor, At: f.CreatedAt}
}

func criteriaOut(found []AssetFound) []api.Criterion {
	out := []api.Criterion{}
	for _, a := range found {
		cr := api.Criterion{Field: a.Field, Must: a.Must, State: api.CriterionState(a.State)}
		if f := a.Fact; f != nil {
			at := f.CreatedAt
			cr.Value, cr.SourceUrl, cr.Inferred, cr.By, cr.At = f.Value, f.SourceURL, f.Inferred, f.Actor, &at
		}
		out = append(out, cr)
	}
	return out
}

// GetCompanyOverview reads what the company page reads, with the same calls.
func (s apiServer) GetCompanyOverview(ctx context.Context, in api.GetCompanyOverviewRequestObject) (api.GetCompanyOverviewResponseObject, error) {
	q, st := reqFrom(ctx), s.a.s
	org := q.Org.ID
	c, err := st.CompanyByID(org, in.Id)
	if err != nil {
		return nil, missing(err, "no company %d", in.Id)
	}
	facts, err := st.Facts(org, c.ID)
	if err != nil {
		return nil, err
	}
	people, err := st.People(org, c.ID)
	if err != nil {
		return nil, err
	}
	pending, err := st.Pending(org, people)
	if err != nil {
		return nil, err
	}
	touches, err := st.Touches(org, c.ID)
	if err != nil {
		return nil, err
	}
	onboardings, err := s.onboardings(org, c.ID)
	if err != nil {
		return nil, err
	}
	day := today()
	subs, err := st.Subscriptions(org, c.ID, day)
	if err != nil {
		return nil, err
	}
	agreements, err := st.Agreements(org, c.ID)
	if err != nil {
		return nil, err
	}
	planned := 0
	if plan, err := q.Org.Settings.Onboarding(); err == nil {
		planned = len(plan)
	}
	out := api.CompanyOverview{Id: c.ID, Name: c.Name, Domain: c.Domain, Country: c.Country, Segment: c.Segment, Grade: c.Fit, Why: c.Why,
		Stage: api.CompanyOverviewStage(c.Stage), FoundBy: c.FoundBy, CreatedAt: c.CreatedAt, Pipeline: api.Pipeline(pipeline(*c, people)),
		Touches: touches, LookingFor: criteriaOut(assetsFound(q.Org.Settings, facts)), Facts: []api.ResearchFact{}, People: []api.CompanyPerson{},
		Onboardings: onboardings, PlannedSteps: planned, Subscriptions: []api.Subscription{}, Agreements: []api.Agreement{}}
	for _, f := range facts {
		out.Facts = append(out.Facts, factOut(f))
	}
	for _, p := range people {
		person := api.CompanyPerson{Id: p.ID, Name: displayName(p), Title: p.Title, Linkedin: deref(p.Linkedin), Stage: contactStage(p), Pending: []api.PendingDetail{}}
		if len(alternates(deref(p.Person))) > 1 {
			person.Several = deref(p.Person)
		}
		for _, d := range pending[p.ID] {
			person.Pending = append(person.Pending, pendingOut(d))
		}
		out.People = append(out.People, person)
	}
	for _, r := range subs {
		out.Subscriptions = append(out.Subscriptions, subscriptionOut(r, day))
	}
	for _, a := range agreements {
		out.Agreements = append(out.Agreements, agreementOut(a))
	}
	return api.GetCompanyOverview200JSONResponse(out), nil
}

// companyHeld checks a company is this organisation's before one of its lists is read,
// so a list of another's reads as missing rather than empty.
func (s apiServer) companyHeld(org, id int64) error {
	if _, err := s.a.s.CompanyByID(org, id); err != nil {
		return missing(err, "no company %d", id)
	}
	return nil
}

func (s apiServer) ListCompanyHistory(ctx context.Context, in api.ListCompanyHistoryRequestObject) (api.ListCompanyHistoryResponseObject, error) {
	org := reqFrom(ctx).Org.ID
	if err := s.companyHeld(org, in.Id); err != nil {
		return nil, err
	}
	page, err := pageIn(int(in.Params.Limit), string(in.Params.Cursor))
	if err != nil {
		return nil, err
	}
	at, err := page.at(1)
	if err != nil {
		return nil, err
	}
	total, err := s.a.s.historyCount(org, in.Id)
	if err != nil {
		return nil, err
	}
	items, err := s.a.s.historyAt(org, in.Id, int(at[0]), page.size())
	if err != nil {
		return nil, err
	}
	next := ""
	if end := int(at[0]) + page.size(); end < total {
		next = cursorOf(int64(end))
	}
	out := api.ListCompanyHistory200JSONResponse{Headers: api.ListCompanyHistory200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor, out.Body.Total = []api.HistoryEntry{}, next, total
	for _, h := range items {
		out.Body.Items = append(out.Body.Items, api.HistoryEntry{At: h.At, Actor: h.Actor, Action: h.Action, Value: h.Value, Person: h.Person, Text: historyText(h)})
	}
	return out, nil
}

func (s apiServer) ListCompanyNotes(ctx context.Context, in api.ListCompanyNotesRequestObject) (api.ListCompanyNotesResponseObject, error) {
	org := reqFrom(ctx).Org.ID
	if err := s.companyHeld(org, in.Id); err != nil {
		return nil, err
	}
	page, err := pageIn(int(in.Params.Limit), string(in.Params.Cursor))
	if err != nil {
		return nil, err
	}
	at, err := page.at(1)
	if err != nil {
		return nil, err
	}
	var total int64
	if err := s.a.s.in(org).Model(&TaskNote{}).Where("company_id = ?", in.Id).Count(&total).Error; err != nil {
		return nil, err
	}
	var notes []TaskNote
	if err := s.a.s.in(org).Where("company_id = ?", in.Id).Order("id desc").Limit(page.size()).Offset(int(at[0])).Find(&notes).Error; err != nil {
		return nil, err
	}
	next := ""
	if end := at[0] + int64(page.size()); end < total {
		next = cursorOf(end)
	}
	out := api.ListCompanyNotes200JSONResponse{Headers: api.ListCompanyNotes200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor, out.Body.Total = []api.Note{}, next, int(total)
	for _, n := range notes {
		body, err := recast[api.Note](n)
		if err != nil {
			return nil, err
		}
		out.Body.Items = append(out.Body.Items, body)
	}
	return out, nil
}

func (s apiServer) ExportPerson(ctx context.Context, in api.ExportPersonRequestObject) (api.ExportPersonResponseObject, error) {
	q := reqFrom(ctx)
	held, err := s.a.s.ExportPerson(q.Org.ID, in.Id, q.Actor())
	if err != nil {
		return nil, err
	}
	out, err := recast[map[string]any](held)
	if err != nil {
		return nil, err
	}
	return api.ExportPerson200JSONResponse(out), nil
}

func (s apiServer) ErasePerson(ctx context.Context, in api.ErasePersonRequestObject) (api.ErasePersonResponseObject, error) {
	q := reqFrom(ctx)
	if err := s.a.s.ErasePerson(q.Org.ID, in.Id, q.Actor()); err != nil {
		return nil, err
	}
	return api.ErasePerson204Response{}, nil
}
