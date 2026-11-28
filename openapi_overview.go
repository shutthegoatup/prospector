package main

import (
	"context"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/privacy"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/sales"
	"github.com/shutthegoatup/prospector/internal/signing"
)

// GetCompanyOverview reads what the company page reads, with the same calls.
func (s apiServer) GetCompanyOverview(ctx context.Context, in api.GetCompanyOverviewRequestObject) (api.GetCompanyOverviewResponseObject, error) {
	q, st := request.ReqFrom(ctx), s.a.s
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
		Stage: api.CompanyOverviewStage(c.Stage), FoundFor: c.FoundFor, FoundBy: c.FoundBy, CreatedAt: c.CreatedAt, Pipeline: api.Pipeline(prospecting.Pipeline(*c, people)),
		Touches: touches, LookingFor: criteriaOut(prospecting.AssetsFound(q.Org.Settings, facts)), Facts: []api.ResearchFact{}, People: []api.CompanyPerson{},
		Onboardings: onboardings, PlannedSteps: planned, Subscriptions: []api.Subscription{}, Agreements: []api.Agreement{}}
	for _, f := range facts {
		out.Facts = append(out.Facts, factOut(f))
	}
	for _, p := range people {
		person := api.CompanyPerson{Id: p.ID, Name: displayName(p), Title: p.Title, Linkedin: deref(p.Linkedin), Stage: research.ContactStage(p), Pending: []api.PendingDetail{}}
		if len(privacy.Alternates(deref(p.Person))) > 1 {
			person.Several = deref(p.Person)
		}
		for _, d := range pending[p.ID] {
			person.Pending = append(person.Pending, research.PendingOut(d))
		}
		out.People = append(out.People, person)
	}
	for _, r := range subs {
		out.Subscriptions = append(out.Subscriptions, sales.SubscriptionOut(r, day))
	}
	for _, a := range agreements {
		out.Agreements = append(out.Agreements, signing.AgreementOut(a))
	}
	return api.GetCompanyOverview200JSONResponse(out), nil
}

func factOut(f Fact) api.ResearchFact {
	return api.ResearchFact{Id: f.ID, Field: f.Field, Value: f.Value, SourceUrl: f.SourceURL, Answer: research.FactAnswerOut(f.Answer), Inferred: f.Inferred, By: f.Actor, At: f.CreatedAt}
}

func criteriaOut(found []prospecting.AssetFound) []api.Criterion {
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
