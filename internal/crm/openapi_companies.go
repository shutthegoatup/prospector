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
	page, err := paging.PageIn(int(p.Limit), string(p.Cursor))
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
	page, err := paging.PageIn(int(in.Params.Limit), string(in.Params.Cursor))
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
		out.Body.Items = append(out.Body.Items, api.HistoryEntry{At: h.At, Actor: h.Actor, Action: h.Action, Value: h.Value, Person: h.Person, Text: HistoryText(h)})
	}
	return out, nil
}

func (s CRMAPI) ListCompanyNotes(ctx context.Context, in api.ListCompanyNotesRequestObject) (api.ListCompanyNotesResponseObject, error) {
	org := request.ReqFrom(ctx).Org.ID
	if err := s.companyHeld(org, in.Id); err != nil {
		return nil, err
	}
	page, err := paging.PageIn(int(in.Params.Limit), string(in.Params.Cursor))
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

func HistoryText(h HistoryItem) string {
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

// CRMAPI is the CRM's operations: the companies list, segments, and a company's
// history and notes, which apiServer embeds.
type CRMAPI struct {
	Records *CRMStore
}
