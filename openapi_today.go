package main

import (
	"context"
	"errors"
	"time"

	"github.com/shutthegoatup/prospector/api"
)

// voiceOf is who calls and signs for whoever asks. A key is nobody's, so its scripts
// keep the placeholder and its emails go unsigned.
func voiceOf(q *Req) Voice {
	if q.Member == nil || q.User == nil {
		return Voice{Org: q.Org.Name, Pitch: q.Org.Settings.withDefaults().Pitch}
	}
	return q.Voice()
}

func cardMeta(c Card) string {
	switch c.Step {
	case "call":
		return callMeta(c)
	case "accepted":
		return acceptedMeta(c)
	case "next":
		return nextMeta(c.Contact)
	case "email", "followup":
		return emailMeta(c)
	}
	return ""
}

func dueCardOut(c Card) api.DueCard {
	out := api.DueCard{Id: c.ID, Step: api.DueCardStep(c.Step), Name: c.Name, Title: c.Title, Company: deref(c.Maker), Fit: deref(c.Fit),
		DraftBy: c.DraftBy, Held: c.Held, State: api.DueCardState(c.State), Next: c.Next, People: []api.DuePerson{}, Badges: []api.DueBadge{},
		Meta: cardMeta(c), Text: c.Text, Copy: c.Copy, OpenUrl: profileHref(c.Contact), OpenLabel: openLabel(c.Contact),
		Number: c.Number, Tel: c.Tel, About: c.About, Script: []api.ScriptLine{}, Email: deref(c.Email), Phone: deref(c.Phone),
		Linkedin: deref(c.Linkedin), Tz: deref(c.Tz), CallNotes: deref(c.CallNotes), Flags: deref(c.Flags), Sources: nonNil(c.SourceList),
		Stage: c.Stage, NextStep: c.NextStep, NextDate: deref(c.NextDate)}
	if c.CompanyID != nil {
		out.CompanyId = *c.CompanyID
	}
	for _, p := range c.People {
		out.People = append(out.People, api.DuePerson{Name: p.Name, Title: p.Title})
	}
	for _, b := range c.Badges {
		out.Badges = append(out.Badges, api.DueBadge{Text: b.Text, State: api.DueBadgeState(b.State)})
	}
	for _, l := range c.Script {
		out.Script = append(out.Script, api.ScriptLine{Cue: l.Cue, Say: l.Say})
	}
	return out
}

func (s apiServer) ListToday(ctx context.Context, in api.ListTodayRequestObject) (api.ListTodayResponseObject, error) {
	q := reqFrom(ctx)
	now := time.Now()
	cards, counts := Today(s.a.dueByStep(q, now), voiceOf(q), now, s.a.s.noMailDomains())
	filter := string(in.Params.Step)
	if filter != "" && !in.Params.Step.Valid() {
		return nil, errors.New("step is call, accepted, email, followup, linkedin or next")
	}
	var shown []Card
	later := 0
	for _, c := range cards {
		if c.Step == "call" && c.State != "ok" {
			later++
		}
		if filter == "" || filter == c.Step {
			shown = append(shown, c)
		}
	}
	page, next, err := listPage(shown, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(page))
	for i, c := range page {
		ids[i] = c.ID
	}
	marks := s.a.s.AgentMarks(q.Org.ID, ids)
	out := api.ListToday200JSONResponse{Headers: api.ListToday200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.DueCard{}, next
	out.Body.Counts, out.Body.Total, out.Body.Shown, out.Body.LaterCalls = counts, len(cards), len(shown), later
	for _, c := range page {
		c.AgentMarks = marks[c.ID]
		out.Body.Items = append(out.Body.Items, dueCardOut(c))
	}
	return out, nil
}

func (s apiServer) GetTodaySummary(ctx context.Context, _ api.GetTodaySummaryRequestObject) (api.GetTodaySummaryResponseObject, error) {
	var d TodayData
	if err := s.a.todayElsewhere(reqFrom(ctx).Org.ID, time.Now(), &d); err != nil {
		return nil, err
	}
	return api.GetTodaySummary200JSONResponse{Enquiries: d.Enquiries, Deals: d.Deals, Subscriptions: d.Subs, OnboardingSteps: d.Steps, Review: d.Review,
		Week: api.Week{Touches: d.Week.Touches, Replies: d.Week.Replies, Meetings: d.Week.Meetings, Researched: d.Week.Researched}}, nil
}

func (s apiServer) MarkOutcome(ctx context.Context, in api.MarkOutcomeRequestObject) (api.MarkOutcomeResponseObject, error) {
	q := reqFrom(ctx)
	if err := s.a.s.AgentMark(q.Org.ID, worker(q), MarkIn{ContactID: in.Body.ContactId, Step: string(in.Body.Step), Date: in.Body.Date, Note: in.Body.Note}); err != nil {
		return nil, err
	}
	return api.MarkOutcome200JSONResponse{Ok: true}, nil
}
