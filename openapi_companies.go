package main

import (
	"context"
	"slices"
	"strings"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
)

func (s apiServer) ListCompanies(ctx context.Context, in api.ListCompaniesRequestObject) (api.ListCompaniesResponseObject, error) {
	q, p := reqFrom(ctx), in.Params
	if p.Stage != "" && !slices.Contains(pipelineStages, string(p.Stage)) {
		return nil, say.Errorf("no stage %q: one of %v", p.Stage, pipelineStages)
	}
	page, err := pageIn(int(p.Limit), string(p.Cursor))
	if err != nil {
		return nil, err
	}
	rows, total, next, err := s.a.s.CompaniesList(q.Org.ID, p.Q, p.Segment, string(p.Stage), page)
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
	all, err := s.a.s.Segments(reqFrom(ctx).Org.ID)
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
	at, err := page.At(1)
	if err != nil {
		return nil, err
	}
	total, err := s.a.s.HistoryCount(org, in.Id)
	if err != nil {
		return nil, err
	}
	items, err := s.a.s.HistoryAt(org, in.Id, int(at[0]), page.Size())
	if err != nil {
		return nil, err
	}
	next := ""
	if end := int(at[0]) + page.Size(); end < total {
		next = paging.Cursor(int64(end))
	}
	out := api.ListCompanyHistory200JSONResponse{Headers: api.ListCompanyHistory200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor, out.Body.Total = []api.HistoryEntry{}, next, total
	for _, h := range items {
		out.Body.Items = append(out.Body.Items, api.HistoryEntry{At: h.At, Actor: h.Actor, Action: h.Action, Value: h.Value, Person: h.Person, Text: historyText(h).In(request.LanguageFrom(ctx))})
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
	at, err := page.At(1)
	if err != nil {
		return nil, err
	}
	notes, total, err := s.a.s.CompanyNotes(org, in.Id, int(at[0]), page.Size())
	if err != nil {
		return nil, err
	}
	next := ""
	if end := at[0] + int64(page.Size()); end < total {
		next = paging.Cursor(end)
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

// historyVerbs say what each kind of history entry was, as the entry's line begins.
var historyVerbs = map[string]*say.Text{
	"mark:linkedin": say.New("added on LinkedIn"), "mark:email": say.New("sent the email"), "mark:followup": say.New("sent the follow-up"),
	"mark:call": say.New("called"), "mark:replied": say.New("marked replied"), "mark:declined": say.New("marked not interested"),
	"mark:hold": say.New("put on hold"), "mark:accepted": say.New("marked accepted"), "add:company": say.New("added the company"),
	"add:facts": say.New("added facts"), "add:fact": say.New("added a fact"), "add:contact": say.New("suggested a contact"),
	"add:person": say.New("added"), "pick": say.New("picked for outreach"), "company:qualified": say.New("qualified the company"),
	"company:rejected": say.New("rejected the company"), "company:review": say.New("sent back to review"),
	"verdict:qualified": say.New("qualified it for an offering"), "verdict:rejected": say.New("junked it for an offering"), "verdict:review": say.New("sent it back to review for an offering"), "add:role": say.New("added a role"), "role:pursued": say.New("pursued a role"), "role:junked": say.New("junked a role"), "role:review": say.New("put a role back for review"),
	"offering:added": say.New("added an offering"), "offering:changed": say.New("changed an offering"), "offering:removed": say.New("removed an offering"),
	"erase": say.New("erased a person on request"), "enquiry": say.New("sent an enquiry"),
	"suggest:email": say.New("suggested an email"), "suggest:phone": say.New("suggested a phone number"), "suggest:linkedin": say.New("suggested a LinkedIn profile"),
	"dismiss:email": say.New("dismissed a suggested email"), "dismiss:phone": say.New("dismissed a suggested phone number"), "dismiss:linkedin": say.New("dismissed a suggested LinkedIn profile"),
	"export": say.New("exported what is held on them"), "task:match": say.New("matched to a task"), "task:held": say.New("found again by a search"),
	"note:skipped": say.New("left a note: skipped"), "note:dead_end": say.New("left a note: found nothing"), "note:next": say.New("left a note: what to do next"),
	"task:new": say.New("wrote a task"), "task:ask": say.New("asked for more on a task"), "task:pickup": say.New("picked up a task"),
	"task:done": say.New("finished a request"), "task:reopen": say.New("reopened a request"), "task:close": say.New("closed a task"), "task:open": say.New("reopened a task"),
	"deal:start": say.New("started another deal"), "deal:remove": say.New("removed a deal"),
	"sub:new": say.New("recorded a subscription"), "sub:remove": say.New("removed a subscription"),
	"onboard:start": say.New("started an onboarding"), "onboard:remove": say.New("removed an onboarding"),
	"onboard:invite": say.New("emailed a link to their onboarding page"), "onboard:revoke": say.New("withdrew access to their onboarding page"),
	"onboard:done": say.New("finished an onboarding step"), "onboard:undone": say.New("reopened an onboarding step"),
	"onboard:due":   say.New("moved an onboarding step's date"),
	"enquiry:reply": say.New("emailed a reply to their enquiry"), "enquiry:heard": say.New("recorded their answer to an enquiry"),
}

// historyText is a history entry's line: what was done, to whom, and what with.
func historyText(h HistoryItem) say.Sayer {
	var verb say.Sayer
	if v, ok := historyVerbs[h.Action]; ok {
		verb = v
	} else if f, ok := strings.CutPrefix(h.Action, "set:"); ok {
		verb = say.Errorf("set %s", strings.ReplaceAll(f, "_", " "))
	} else if f, ok := strings.CutPrefix(h.Action, "enquiry:"); ok {
		verb = say.Errorf("set an enquiry's %s", f)
	} else if f, ok := strings.CutPrefix(h.Action, "deal:"); ok {
		verb = say.Errorf("set the deal's %s", strings.ReplaceAll(f, "_", " "))
	} else if f, ok := strings.CutPrefix(h.Action, "sub:"); ok {
		verb = say.Errorf("changed a subscription's %s", strings.ReplaceAll(f, "_", " "))
	} else if k, ok := strings.CutPrefix(h.Action, "draft:"); ok {
		verb = say.Errorf("drafted the %s", k)
	} else {
		verb = say.New(h.Action)
	}
	s := verb
	if h.Person != "" {
		s = say.Errorf("%s · %s", s, h.Person)
	}
	if _, note, found := strings.Cut(h.Value, " · "); found && strings.HasPrefix(h.Action, "mark:") {
		s = say.Errorf("%s: %s", s, note)
	}
	if h.Value != "" && !strings.HasPrefix(h.Action, "mark:") && h.Action != "add:contact" && len(h.Value) < 80 {
		s = say.Errorf("%s: %s", s, h.Value)
	}
	return s
}
