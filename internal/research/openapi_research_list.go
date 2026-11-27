package research

import (
	"context"
	"errors"
	"strings"

	"github.com/shutthegoatup/prospector/api"

	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

func (s ResearchAPI) ListResearch(ctx context.Context, in api.ListResearchRequestObject) (api.ListResearchResponseObject, error) {
	q := request.ReqFrom(ctx)
	page, err := paging.PageIn(int(in.Params.Limit), string(in.Params.Cursor))
	if err != nil {
		return nil, err
	}
	filter := string(in.Params.Filter)
	if filter == "" {
		filter = "review"
	}
	offering := in.Params.Offering
	if in.Params.Task != 0 {
		t, err := s.Research.TaskByID(q.Org.ID, in.Params.Task)
		if err != nil {
			return nil, store.Missing(err, "there's no task %d here", in.Params.Task)
		}
		if offering != 0 && offering != t.OfferingID {
			return nil, say.Errorf("task %d finds companies for another offering", t.ID)
		}
		offering = t.OfferingID
		// A closed task can outlive its offering, whose finds are then the organisation's own.
		if offering != 0 {
			if _, err := s.Research.Offering(q.Org.ID, offering); errors.As(err, new(store.NotFound)) {
				offering = 0
			}
		}
	}
	set := q.Org.Settings
	if offering != 0 {
		o, err := s.Research.Offering(q.Org.ID, offering)
		if err != nil {
			return nil, err
		}
		set.LookFor = o.LookFor
	}
	rows, ranked, next, err := s.Finding.ResearchList(q.Org.ID, offering, set, filter, in.Params.Task, page)
	if err != nil {
		return nil, err
	}
	if offering != 0 {
		if err := s.Research.WithVerdicts(q.Org.ID, offering, rows); err != nil {
			return nil, err
		}
	}
	out := api.ListResearch200JSONResponse{Headers: api.ListResearch200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.Ranked, out.Body.NextCursor = []api.ResearchCompany{}, ranked, next
	out.Body.OfferingId = offering
	out.Body.Counts = s.Finding.ResearchCountsFor(q.Org.ID, offering, in.Params.Task)
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	roles, err := s.Research.RolesAt(q.Org.ID, offering, ids)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		c := researchOut(set, r)
		for _, role := range roles[r.ID] {
			c.Roles = append(c.Roles, roleOut(role))
		}
		out.Body.Items = append(out.Body.Items, c)
	}
	return out, nil
}

var lookupOut = map[string]api.LookupOutcome{
	prospecting.LookupFound: api.LookupOutcomeFound, LookupNone: api.LookupOutcomeNone, LookupNotKept: api.LookupOutcomeNotKept,
}

func LookingFor(set store.OrgSettings) []api.LookFor {
	out := []api.LookFor{}
	assets, _ := set.Assets()
	for _, a := range assets {
		out = append(out, api.LookFor{Field: a.Field, Hint: a.Hint, Must: a.Must})
	}
	return out
}

func FactAnswerOut(answer string) api.ResearchFactAnswer {
	if answer == AnswerYes {
		return api.ResearchFactAnswerYes
	}
	return api.ResearchFactAnswer(answer)
}

func researchOut(set store.OrgSettings, r prospecting.ResearchRow) api.ResearchCompany {
	found := prospecting.AssetsFound(set, r.Facts)
	fit := prospecting.FitOf(found)
	c := api.ResearchCompany{Id: r.ID, Name: r.Name, Domain: r.Domain, Country: r.Country, Segment: r.Segment,
		Stage: api.ResearchCompanyStage(r.Stage), Grade: r.Fit, Why: r.Why, FoundBy: r.FoundBy, CreatedAt: r.CreatedAt,
		Criteria: []api.Criterion{}, Facts: []api.ResearchFact{}, People: []api.ResearchPerson{},
		Fit: api.Fit{Of: fit.Of, Met: fit.Met, NotFound: fit.NotFound, Unchecked: fit.Unchecked, MustsMissed: fit.MustsMissed, Text: fit.Text()}}
	for _, f := range OtherFacts(set, r.Facts) {
		c.Facts = append(c.Facts, api.ResearchFact{Id: f.ID, Field: f.Field, Value: f.Value, SourceUrl: f.SourceURL,
			Answer: FactAnswerOut(f.Answer), Inferred: f.Inferred, By: f.Actor, At: f.CreatedAt})
	}
	for _, p := range r.People {
		person := api.ResearchPerson{Id: p.ID, Name: store.DisplayName(p), Title: p.Title, Status: p.Status, HasEmail: store.Deref(p.Email) != "", HasPhone: store.Deref(p.Phone) != "",
			EmailStatus: p.EmailStatus, Linkedin: store.Deref(p.Linkedin), SourceUrl: FirstLine(store.Deref(p.Sources)),
			Pending: []api.PendingDetail{}, Lookups: []api.Lookup{},
			Unlookable: set.Unlookable(p, strings.ToLower(strings.TrimSpace(r.Domain)))}
		for _, d := range r.Pending[p.ID] {
			person.Pending = append(person.Pending, PendingOut(d))
		}
		for _, l := range r.Lookups[p.ID] {
			person.Lookups = append(person.Lookups, api.Lookup{Field: l.Field, Vendor: api.EnricherKind(l.Vendor),
				Outcome: lookupOut[l.Outcome], At: l.At, AgainAt: l.At.Add(prospecting.EnrichAgain)})
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

func PendingOut(d prospecting.PendingDetail) api.PendingDetail {
	return api.PendingDetail{Id: d.ID, Field: d.Field, Value: d.Value, Current: d.Current, SourceUrl: d.SourceURL, By: d.By, At: d.At, EmailStatus: d.EmailStatus, Vendor: d.Vendor}
}
