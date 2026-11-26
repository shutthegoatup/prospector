package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/shutthegoatup/prospector/api"
)

// worker is who reads or works a task through the API: a key as the agent it is, or a
// person signed in, who claims nothing by reading.
func worker(q *Req) Actor {
	if q.Key != nil {
		return q.Agent()
	}
	return Actor{Name: q.Actor(), ReadOnly: true}
}

// recast carries what the store answers into the spec's type of the same JSON, so the
// API says exactly what these routes always have; a field the spec lacks is caught by
// the test that compares the two.
func recast[T any](v any) (T, error) {
	var out T
	raw, err := json.Marshal(v)
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	return out, err
}

func (s apiServer) ListTasks(ctx context.Context, in api.ListTasksRequestObject) (api.ListTasksResponseObject, error) {
	q := reqFrom(ctx)
	page, err := pageIn(int(in.Params.Limit), string(in.Params.Cursor))
	if err != nil {
		return nil, err
	}
	tasks, next, err := s.a.s.taskBriefsFor(q.Org.ID, worker(q), in.Params.Closed, in.Params.Offering, page)
	if err != nil {
		return nil, err
	}
	var open, all int64
	if err := s.a.s.in(q.Org.ID).Model(&Task{}).Where("closed_at is null").Count(&open).Error; err != nil {
		return nil, err
	}
	if err := s.a.s.in(q.Org.ID).Model(&Task{}).Count(&all).Error; err != nil {
		return nil, err
	}
	out := api.ListTasks200JSONResponse{Headers: api.ListTasks200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Open, out.Body.All = int(open), int(all)
	if out.Body.Items, err = recast[[]api.TaskBrief](tasks); err != nil {
		return nil, err
	}
	if out.Body.Items == nil {
		out.Body.Items = []api.TaskBrief{}
	}
	out.Body.NextCursor = next
	return out, nil
}

func (s apiServer) GetTask(ctx context.Context, in api.GetTaskRequestObject) (api.GetTaskResponseObject, error) {
	q := reqFrom(ctx)
	page, err := pageIn(int(in.Params.Limit), string(in.Params.Cursor))
	if err != nil {
		return nil, err
	}
	t, err := s.a.s.AgentTask(q.Org.ID, worker(q), in.Id, page)
	if err != nil {
		return nil, err
	}
	body, err := recast[api.TaskOut](t)
	if err != nil {
		return nil, err
	}
	return api.GetTask200JSONResponse{Body: body, Headers: api.GetTask200ResponseHeaders{Link: nextLink(ctx, t.NextCursor)}}, nil
}

func (s apiServer) AddNote(ctx context.Context, in api.AddNoteRequestObject) (api.AddNoteResponseObject, error) {
	q := reqFrom(ctx)
	who := worker(q)
	who.ReadOnly = false
	note, err := s.a.s.AgentNote(q.Org.ID, who, NoteIn{Task: in.Body.Task, Company: in.Body.Company, Kind: in.Body.Kind, Text: in.Body.Text})
	if err != nil {
		return nil, err
	}
	body, err := recast[api.Note](note)
	if err != nil {
		return nil, err
	}
	return api.AddNote200JSONResponse(body), nil
}

func (s apiServer) FinishRequest(ctx context.Context, in api.FinishRequestRequestObject) (api.FinishRequestResponseObject, error) {
	q := reqFrom(ctx)
	who := worker(q)
	who.ReadOnly = false
	if err := s.a.s.AgentFinish(q.Org.ID, who, FinishIn{Request: in.Body.Request}); err != nil {
		return nil, err
	}
	return api.FinishRequest200JSONResponse{Ok: true}, nil
}

func (s apiServer) GetTaskPrompt(ctx context.Context, in api.GetTaskPromptRequestObject) (api.GetTaskPromptResponseObject, error) {
	q := reqFrom(ctx)
	t, err := s.a.s.taskRow(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	return api.GetTaskPrompt200JSONResponse{Prompt: taskPrompt(q.Org, *t)}, nil
}

func (s apiServer) ListTaskNotes(ctx context.Context, in api.ListTaskNotesRequestObject) (api.ListTaskNotesResponseObject, error) {
	q := reqFrom(ctx)
	if _, err := s.a.s.taskRow(q.Org.ID, in.Id); err != nil {
		return nil, err
	}
	page, err := pageIn(in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	notes, next, err := s.a.s.TaskNotes(q.Org.ID, in.Id, page)
	if err != nil {
		return nil, err
	}
	who := worker(q)
	out := api.ListTaskNotes200JSONResponse{Headers: api.ListTaskNotes200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.TaskNoteOut{}, next
	for _, n := range notes {
		out.Body.Items = append(out.Body.Items, api.TaskNoteOut{Id: n.ID, Kind: n.Kind, Text: n.Text, Actor: n.Actor, At: n.CreatedAt,
			Company: n.Company, CompanyId: n.CompanyID, ByYou: who.did(Event{User: n.Actor, KeyID: n.KeyID})})
	}
	return out, nil
}

func (s apiServer) ListTaskWork(ctx context.Context, in api.ListTaskWorkRequestObject) (api.ListTaskWorkResponseObject, error) {
	q := reqFrom(ctx)
	now := time.Now()
	t, err := s.a.s.Task(q.Org.ID, in.Id, now)
	if err != nil {
		return nil, err
	}
	runs, next, err := listPage(t.Runs, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListTaskWork200JSONResponse{Headers: api.ListTaskWork200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.TaskWork{}, next
	for _, r := range runs {
		out.Body.Items = append(out.Body.Items, api.TaskWork{Actor: r.Actor, Start: r.Start, End: r.End, Going: r.Going(now),
			Changes: r.Changes, Companies: r.Companies, People: r.People, Drafts: r.Drafts})
	}
	return out, nil
}

func (s apiServer) DeleteNote(ctx context.Context, in api.DeleteNoteRequestObject) (api.DeleteNoteResponseObject, error) {
	if err := s.a.s.DeleteNote(reqFrom(ctx).Org.ID, in.Id); err != nil {
		return nil, err
	}
	return api.DeleteNote204Response{}, nil
}

func (s apiServer) ListLeftOut(ctx context.Context, in api.ListLeftOutRequestObject) (api.ListLeftOutResponseObject, error) {
	q := reqFrom(ctx)
	t, err := s.a.s.taskRow(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	views, err := s.a.s.taskViews(s.a.s.db, q.Org.ID, []Task{*t}, time.Now())
	if err != nil {
		return nil, err
	}
	v := views[0]
	left := v.LeftOut()
	page, next, err := listPage(left, int(in.Params.Limit), string(in.Params.Cursor))
	if err != nil {
		return nil, err
	}
	out := api.ListLeftOut200JSONResponse{Headers: api.ListLeftOut200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.NextCursor, out.Body.Offering = next, t.OfferingID
	out.Body.NeverContact, out.Body.Erased = v.Withheld[withheldNeverContact], v.Withheld[withheldErased]
	for _, c := range left {
		switch c.Held {
		case heldRejected:
			out.Body.Rejected++
		case heldCustomer:
			out.Body.Customer++
		case heldOpenDeal:
			out.Body.OpenDeal++
		case heldQualified:
			out.Body.Qualified++
		}
	}
	if out.Body.Items, err = recast[[]api.TaskCompany](page); err != nil {
		return nil, err
	}
	if out.Body.Items == nil {
		out.Body.Items = []api.TaskCompany{}
	}
	return out, nil
}
