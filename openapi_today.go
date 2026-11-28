package main

import (
	"context"
	"time"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/say"
)

// voiceOf is who calls and signs for whoever asks. A key is nobody's, so its scripts
// keep the placeholder and its emails go unsigned.
func voiceOf(q *request.Req) request.Voice {
	if q.Member == nil || q.User == nil {
		return request.Voice{Org: q.Org.Name, Pitch: q.Org.Settings.WithDefaults().Pitch}
	}
	return q.Voice()
}

// todayVoice is voiceOf with each of the organisation's offerings' pitches, which a
// call card uses for a company found for one of them.
func (a *App) todayVoice(q *request.Req) (request.Voice, error) {
	voice := voiceOf(q)
	offerings, err := a.s.Offerings(q.Org.ID)
	if err != nil {
		return voice, err
	}
	voice.Pitches = map[int64]string{}
	for _, o := range offerings {
		voice.Pitches[o.ID] = o.Pitch
	}
	return voice, nil
}

func cardMeta(c Card) say.Sayer {
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
	return say.Nothing
}

// dueCardOut is a card as the API sends it, its words in lang.
func dueCardOut(c Card, lang string) api.DueCard {
	out := api.DueCard{Id: c.ID, Step: api.DueCardStep(c.Step), Name: c.Name, Title: c.Title, Company: deref(c.Maker), Fit: deref(c.Fit),
		DraftBy: c.DraftBy, Held: c.Held, State: api.DueCardState(c.State), Next: c.Next, People: []api.DuePerson{}, Badges: []api.DueBadge{},
		Meta: cardMeta(c).In(lang), Text: c.Text, Copy: c.Copy, OpenUrl: profileHref(c.Contact), OpenLabel: openLabel(c.Contact).In(lang),
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
		out.Badges = append(out.Badges, api.DueBadge{Text: b.Text.In(lang), State: api.DueBadgeState(b.State)})
	}
	for _, l := range c.Script {
		out.Script = append(out.Script, api.ScriptLine{Cue: l.Cue.In(lang), Say: l.Say.In(lang)})
	}
	return out
}

func (s apiServer) ListToday(ctx context.Context, in api.ListTodayRequestObject) (api.ListTodayResponseObject, error) {
	q := request.ReqFrom(ctx)
	now := time.Now()
	voice, err := s.a.todayVoice(q)
	if err != nil {
		return nil, err
	}
	cards, counts := Today(s.a.dueByStep(q, now), voice, now, s.a.s.NoMailDomains())
	filter := string(in.Params.Step)
	if filter != "" && !in.Params.Step.Valid() {
		return nil, say.New("step is call, accepted, email, followup, linkedin or next")
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
	page, next, err := paging.ListPage(shown, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(page))
	for i, c := range page {
		ids[i] = c.ID
	}
	marks := s.a.s.AgentMarks(q.Org.ID, ids)
	out := api.ListToday200JSONResponse{Headers: api.ListToday200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.DueCard{}, next
	out.Body.Counts, out.Body.Total, out.Body.Shown, out.Body.LaterCalls = counts, len(cards), len(shown), later
	for _, c := range page {
		c.AgentMarks = marks[c.ID]
		out.Body.Items = append(out.Body.Items, dueCardOut(c, request.LanguageOf(requestFrom(ctx))))
	}
	return out, nil
}

func (s apiServer) GetTodaySummary(ctx context.Context, _ api.GetTodaySummaryRequestObject) (api.GetTodaySummaryResponseObject, error) {
	var d TodayData
	if err := s.a.todayElsewhere(request.ReqFrom(ctx).Org.ID, time.Now(), &d); err != nil {
		return nil, err
	}
	return api.GetTodaySummary200JSONResponse{Enquiries: d.Enquiries, Deals: d.Deals, Subscriptions: d.Subs, OnboardingSteps: d.Steps, Review: d.Review,
		Week: api.Week{Touches: d.Week.Touches, Replies: d.Week.Replies, Meetings: d.Week.Meetings, Researched: d.Week.Researched}}, nil
}

func (s apiServer) MarkOutcome(ctx context.Context, in api.MarkOutcomeRequestObject) (api.MarkOutcomeResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.a.s.AgentMark(q.Org.ID, request.Worker(q), research.MarkIn{ContactID: in.Body.ContactId, Step: string(in.Body.Step), Date: in.Body.Date, Note: in.Body.Note}); err != nil {
		return nil, err
	}
	return api.MarkOutcome200JSONResponse{Ok: true}, nil
}

// dealStages are a deal's stages as the status line names them.
var dealStages = map[string]*say.Text{
	"replied": say.New("replied"), "meeting": say.New("meeting"), "proposal": say.New("proposal"), "won": say.New("won"), "lost": say.New("lost"),
}

func nextMeta(c Contact) say.Sayer {
	var st say.Sayer = dealStages["replied"]
	if s, ok := dealStages[c.Stage]; ok {
		st = s
	} else if c.Stage != "" {
		st = say.New(c.Stage)
	}
	if c.NextStep == "" {
		return say.Errorf("In conversation · %s · replied %s · no next step yet", st, deref(c.Replied))
	}
	return say.Errorf("In conversation · %s · replied %s · due %s", st, deref(c.Replied), deref(c.NextDate))
}

func callMeta(c Card) say.Sayer {
	var s say.Sayer = say.New("Not on LinkedIn yet")
	if c.LinkedinAdded != nil {
		s = say.Errorf("Added on LinkedIn %s%s", *c.LinkedinAdded, markedBy(c, "linkedin"))
	}
	if c.EmailSent != nil {
		s = say.Errorf("%s · emailed %s%s", s, *c.EmailSent, markedBy(c, "email"))
	}
	return s
}

// markedBy is who marked a step done, when it wasn't a member, in brackets after it.
func markedBy(c Card, step string) say.Sayer {
	if a := c.AgentMarks[step]; a != "" {
		return say.Errorf(" (%s)", actorLabel(a))
	}
	return say.Nothing
}

func acceptedMeta(c Card) say.Sayer {
	if deref(c.Message) == "" {
		return say.Errorf("Accepted %s%s · LinkedIn message (none written yet, so this is the email; edit it down)", deref(c.Accepted), markedBy(c, "accepted"))
	}
	return say.Errorf("Accepted %s%s · LinkedIn message", deref(c.Accepted), markedBy(c, "accepted"))
}

func emailMeta(c Card) say.Sayer {
	if c.Step == "email" {
		return say.Errorf("Subject: %s · added %s%s", deref(c.Subject), deref(c.LinkedinAdded), markedBy(c, "linkedin"))
	}
	return say.Errorf("added %s%s", deref(c.LinkedinAdded), markedBy(c, "linkedin"))
}
