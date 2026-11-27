package main

import (
	"context"

	"github.com/shutthegoatup/prospector/api"
)

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
	onboardings, err := s.Onboardings(org, c.ID)
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
		Stage: api.CompanyOverviewStage(c.Stage), FoundFor: c.FoundFor, FoundBy: c.FoundBy, CreatedAt: c.CreatedAt, Pipeline: api.Pipeline(pipeline(*c, people)),
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
