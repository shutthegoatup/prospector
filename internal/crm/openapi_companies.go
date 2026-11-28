package crm

import (
	"context"
	"slices"
	"strings"

	"github.com/shutthegoatup/prospector/api"

	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

func (s CRMAPI) ListCompanies(ctx context.Context, in api.ListCompaniesRequestObject) (api.ListCompaniesResponseObject, error) {
	q, p := request.ReqFrom(ctx), in.Params
	if p.Stage != "" && !slices.Contains(PipelineStages, string(p.Stage)) {
		return nil, say.Errorf("no stage %q: one of %v", p.Stage, PipelineStages)
	}
	page, err := paging.PageIn(p.Limit, p.Cursor)
	if err != nil {
		return nil, err
	}
	rows, total, next, err := s.Records.CompaniesList(q.Org.ID, p.Q, p.Segment, string(p.Stage), page)
	if err != nil {
		return nil, err
	}
	out := api.ListCompanies200JSONResponse{Headers: api.ListCompanies200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor, out.Body.Total = []api.CompanyRow{}, next, total
	for _, r := range rows {
		out.Body.Items = append(out.Body.Items, api.CompanyRow{Id: r.ID, Name: r.Name, Country: r.Country, Segment: r.Segment,
			Grade: r.Fit, People: r.People, Pipeline: api.Pipeline(r.Pipeline)})
	}
	return out, nil
}

func (s CRMAPI) ListSegments(ctx context.Context, in api.ListSegmentsRequestObject) (api.ListSegmentsResponseObject, error) {
	all, err := s.Records.Segments(request.ReqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	page, next, err := paging.ListPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListSegments200JSONResponse{Headers: api.ListSegments200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = append([]string{}, page...), next
	return out, nil
}

// companyHeld checks a company is this organisation's before one of its lists is read,
// so a list of another's reads as missing rather than empty.
func (s CRMAPI) companyHeld(org, id int64) error {
	if _, err := s.Records.CompanyByID(org, id); err != nil {
		return store.Missing(err, "no company %d", id)
	}
	return nil
}

func (s CRMAPI) ListCompanyHistory(ctx context.Context, in api.ListCompanyHistoryRequestObject) (api.ListCompanyHistoryResponseObject, error) {
	org := request.ReqFrom(ctx).Org.ID
	if err := s.companyHeld(org, in.Id); err != nil {
		return nil, err
	}
	page, err := paging.PageIn(in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	at, err := page.At(1)
	if err != nil {
		return nil, err
	}
	total, err := s.Records.HistoryCount(org, in.Id)
	if err != nil {
		return nil, err
	}
	items, err := s.Records.HistoryAt(org, in.Id, int(at[0]), page.Size())
	if err != nil {
		return nil, err
	}
	next := ""
	if end := int(at[0]) + page.Size(); end < total {
		next = paging.Cursor(int64(end))
	}
	out := api.ListCompanyHistory200JSONResponse{Headers: api.ListCompanyHistory200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor, out.Body.Total = []api.HistoryEntry{}, next, total
	for _, h := range items {
		out.Body.Items = append(out.Body.Items, api.HistoryEntry{At: h.At, Actor: h.Actor, Action: h.Action, Value: h.Value, Person: h.Person, Text: HistoryText(h).In(request.LanguageFrom(ctx))})
	}
	return out, nil
}

func (s CRMAPI) ListCompanyNotes(ctx context.Context, in api.ListCompanyNotesRequestObject) (api.ListCompanyNotesResponseObject, error) {
	org := request.ReqFrom(ctx).Org.ID
	if err := s.companyHeld(org, in.Id); err != nil {
		return nil, err
	}
	page, err := paging.PageIn(in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	at, err := page.At(1)
	if err != nil {
		return nil, err
	}
	notes, total, err := s.Records.CompanyNotes(org, in.Id, int(at[0]), page.Size())
	if err != nil {
		return nil, err
	}
	next := ""
	if end := at[0] + int64(page.Size()); end < total {
		next = paging.Cursor(end)
	}
	out := api.ListCompanyNotes200JSONResponse{Headers: api.ListCompanyNotes200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor, out.Body.Total = []api.Note{}, next, int(total)
	for _, n := range notes {
		body, err := request.Recast[api.Note](n)
		if err != nil {
			return nil, err
		}
		out.Body.Items = append(out.Body.Items, body)
	}
	return out, nil
}

var PipelineStages = []string{"research", "outreach", "replied", "meeting", "proposal", "won", "lost", "rejected"}

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

// HistoryText is a history entry's line: what was done, to whom, and what with.
func HistoryText(h HistoryItem) say.Sayer {
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

// CRMAPI is the CRM's operations: the companies list, segments, and a company's
// history and notes, which apiServer embeds.
type CRMAPI struct {
	Records *CRMStore
}
