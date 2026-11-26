package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

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

var pipelineStages = []string{"research", "outreach", "replied", "meeting", "proposal", "won", "lost", "rejected"}

func historyText(h HistoryItem) string {
	verb := map[string]string{
		"mark:linkedin": "added on LinkedIn", "mark:email": "sent the email", "mark:followup": "sent the follow-up",
		"mark:call": "called", "mark:replied": "marked replied", "mark:declined": "marked not interested",
		"mark:hold": "put on hold", "mark:accepted": "marked accepted", "add:company": "added the company",
		"add:facts": "added facts", "add:fact": "added a fact", "add:contact": "suggested a contact",
		"add:person": "added", "pick": "picked for outreach", "company:qualified": "qualified the company",
		"company:rejected": "rejected the company", "company:review": "sent back to review",
		"verdict:qualified": "qualified it for an offering", "verdict:rejected": "junked it for an offering", "verdict:review": "sent it back to review for an offering", "add:role": "added a role", "role:pursued": "pursued a role", "role:junked": "junked a role", "role:review": "put a role back for review",
		"offering:added": "added an offering", "offering:changed": "changed an offering", "offering:removed": "removed an offering",
		"erase": "erased a person on request", "enquiry": "sent an enquiry",
		"suggest:email": "suggested an email", "suggest:phone": "suggested a phone number", "suggest:linkedin": "suggested a LinkedIn profile",
		"dismiss:email": "dismissed a suggested email", "dismiss:phone": "dismissed a suggested phone number", "dismiss:linkedin": "dismissed a suggested LinkedIn profile",
		"export": "exported what is held on them", "task:match": "matched to a task", "task:held": "found again by a search",
		"note:skipped": "left a note: skipped", "note:dead_end": "left a note: found nothing", "note:next": "left a note: what to do next",
		"task:new": "wrote a task", "task:ask": "asked for more on a task", "task:pickup": "picked up a task",
		"task:done": "finished a request", "task:reopen": "reopened a request", "task:close": "closed a task", "task:open": "reopened a task",
		"deal:start": "started another deal", "deal:remove": "removed a deal",
		"sub:new": "recorded a subscription", "sub:remove": "removed a subscription",
		"onboard:start": "started an onboarding", "onboard:remove": "removed an onboarding",
		"onboard:invite": "emailed a link to their onboarding page", "onboard:revoke": "withdrew access to their onboarding page",
		"onboard:done": "finished an onboarding step", "onboard:undone": "reopened an onboarding step",
		"onboard:due":   "moved an onboarding step's date",
		"enquiry:reply": "emailed a reply to their enquiry", "enquiry:heard": "recorded their answer to an enquiry",
	}[h.Action]
	if verb == "" {
		if f, ok := strings.CutPrefix(h.Action, "set:"); ok {
			verb = "set " + strings.ReplaceAll(f, "_", " ")
		} else if f, ok := strings.CutPrefix(h.Action, "enquiry:"); ok {
			verb = "set an enquiry's " + f
		} else if f, ok := strings.CutPrefix(h.Action, "deal:"); ok {
			verb = "set the deal's " + strings.ReplaceAll(f, "_", " ")
		} else if f, ok := strings.CutPrefix(h.Action, "sub:"); ok {
			verb = "changed a subscription's " + strings.ReplaceAll(f, "_", " ")
		} else if k, ok := strings.CutPrefix(h.Action, "draft:"); ok {
			verb = "drafted the " + k
		} else {
			verb = h.Action
		}
	}
	s := verb
	if h.Person != "" {
		s += " · " + h.Person
	}
	if _, note, found := strings.Cut(h.Value, " · "); found && strings.HasPrefix(h.Action, "mark:") {
		s += ": " + note
	}
	if h.Value != "" && !strings.HasPrefix(h.Action, "mark:") && h.Action != "add:contact" && len(h.Value) < 80 {
		s += ": " + h.Value
	}
	return s
}
