package main

import (
	"context"
	"encoding/json"

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
	tasks, next, err := s.a.s.AgentTasks(q.Org.ID, worker(q), page)
	if err != nil {
		return nil, err
	}
	out := api.ListTasks200JSONResponse{Headers: api.ListTasks200ResponseHeaders{Link: nextLink(ctx, next)}}
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
