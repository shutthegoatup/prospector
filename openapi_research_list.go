package main

import (
	"context"
	"errors"
	"fmt"

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
	offering := in.Params.Offering
	if in.Params.Task != 0 {
		t, err := s.a.s.taskRow(q.Org.ID, in.Params.Task)
		if err != nil {
			return nil, missing(err, "there's no task %d here", in.Params.Task)
		}
		if offering != 0 && offering != t.OfferingID {
			return nil, fmt.Errorf("task %d finds companies for another offering", t.ID)
		}
		offering = t.OfferingID
		// A closed task can outlive its offering, whose finds are then the organisation's own.
		if offering != 0 {
			if _, err := s.a.s.Offering(q.Org.ID, offering); errors.As(err, new(notFound)) {
				offering = 0
			}
		}
	}
	set := q.Org.Settings
	if offering != 0 {
		o, err := s.a.s.Offering(q.Org.ID, offering)
		if err != nil {
			return nil, err
		}
		set.LookFor = o.LookFor
	}
	rows, ranked, next, err := s.a.s.researchList(q.Org.ID, offering, set, filter, in.Params.Task, page)
	if err != nil {
		return nil, err
	}
	if offering != 0 {
		if err := s.a.s.withVerdicts(q.Org.ID, offering, rows); err != nil {
			return nil, err
		}
	}
	out := api.ListResearch200JSONResponse{Headers: api.ListResearch200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.Ranked, out.Body.NextCursor = []api.ResearchCompany{}, ranked, next
	out.Body.Counts = s.a.s.researchCountsFor(q.Org.ID, offering, in.Params.Task)
	for _, r := range rows {
		out.Body.Items = append(out.Body.Items, researchOut(set, r))
	}
	return out, nil
}

var lookupOut = map[string]api.LookupOutcome{
	lookupFound: api.LookupOutcomeFound, lookupNone: api.LookupOutcomeNone, lookupNotKept: api.LookupOutcomeNotKept,
}

func lookingFor(set OrgSettings) []api.LookFor {
	out := []api.LookFor{}
	assets, _ := set.Assets()
	for _, a := range assets {
		out = append(out, api.LookFor{Field: a.Field, Hint: a.Hint, Must: a.Must})
	}
	return out
}

func factAnswerOut(answer string) api.ResearchFactAnswer {
	if answer == answerYes {
		return api.ResearchFactAnswerYes
	}
	return api.ResearchFactAnswer(answer)
}

func researchOut(set OrgSettings, r ResearchRow) api.ResearchCompany {
	found := assetsFound(set, r.Facts)
	fit := fitOf(found)
	c := api.ResearchCompany{Id: r.ID, Name: r.Name, Domain: r.Domain, Country: r.Country, Segment: r.Segment,
		Stage: api.ResearchCompanyStage(r.Stage), Grade: r.Fit, Why: r.Why, FoundBy: r.FoundBy, CreatedAt: r.CreatedAt,
		Criteria: []api.Criterion{}, Facts: []api.ResearchFact{}, People: []api.ResearchPerson{},
		Fit: api.Fit{Of: fit.Of, Met: fit.Met, NotFound: fit.NotFound, Unchecked: fit.Unchecked, MustsMissed: fit.MustsMissed, Text: fit.Text()}}
	for _, f := range otherFacts(set, r.Facts) {
		c.Facts = append(c.Facts, api.ResearchFact{Id: f.ID, Field: f.Field, Value: f.Value, SourceUrl: f.SourceURL,
			Answer: factAnswerOut(f.Answer), Inferred: f.Inferred, By: f.Actor, At: f.CreatedAt})
	}
	for _, p := range r.People {
		person := api.ResearchPerson{Id: p.ID, Name: displayName(p), Title: p.Title, Status: p.Status, HasEmail: deref(p.Email) != "",
			EmailStatus: p.EmailStatus, Linkedin: deref(p.Linkedin), SourceUrl: firstLine(deref(p.Sources)),
			Pending: []api.PendingDetail{}, Lookups: []api.Lookup{}}
		for _, d := range r.Pending[p.ID] {
			person.Pending = append(person.Pending, pendingOut(d))
		}
		for _, l := range r.Lookups[p.ID] {
			person.Lookups = append(person.Lookups, api.Lookup{Field: l.Field, Vendor: api.EnricherKind(l.Vendor),
				Outcome: lookupOut[l.Outcome], At: l.At, AgainAt: l.At.Add(enrichAgain)})
		}
		c.People = append(c.People, person)
	}
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
