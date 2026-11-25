package main

import (
	"context"

	"github.com/shutthegoatup/prospector/api"
)

func taskOut(t Task) api.Task {
	return api.Task{Id: t.ID, Title: t.Title, CreatedBy: t.CreatedBy, CreatedAt: t.CreatedAt, ClosedAt: t.ClosedAt}
}

func requestOut(r TaskRequest) api.TaskRequest {
	return api.TaskRequest{Id: r.ID, TaskId: r.TaskID, Text: r.Text, AskedBy: r.AskedBy, AskedAt: r.AskedAt, DoneAt: r.DoneAt, DoneBy: r.DoneBy}
}

func (s apiServer) CreateTask(ctx context.Context, in api.CreateTaskRequestObject) (api.CreateTaskResponseObject, error) {
	q := reqFrom(ctx)
	t, err := s.a.s.CreateTask(q.Org.ID, in.Body.Title, in.Body.Brief, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.CreateTask201JSONResponse(taskOut(*t)), nil
}

func (s apiServer) ChangeTask(ctx context.Context, in api.ChangeTaskRequestObject) (api.ChangeTaskResponseObject, error) {
	q := reqFrom(ctx)
	if err := s.a.s.SetTaskClosed(q.Org.ID, in.Id, in.Body.Closed, q.Actor()); err != nil {
		return nil, err
	}
	t, err := s.a.s.taskRow(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	return api.ChangeTask200JSONResponse(taskOut(*t)), nil
}

func (s apiServer) AskTask(ctx context.Context, in api.AskTaskRequestObject) (api.AskTaskResponseObject, error) {
	q := reqFrom(ctx)
	r, err := s.a.s.askTask(q.Org.ID, in.Id, in.Body.Text, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.AskTask201JSONResponse(requestOut(*r)), nil
}

func (s apiServer) ChangeTaskRequest(ctx context.Context, in api.ChangeTaskRequestRequestObject) (api.ChangeTaskRequestResponseObject, error) {
	q := reqFrom(ctx)
	if err := s.a.s.SetRequestDone(q.Org.ID, in.Id, in.Body.Done, q.Actor()); err != nil {
		return nil, err
	}
	r, err := s.a.s.requestRow(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	return api.ChangeTaskRequest200JSONResponse(requestOut(*r)), nil
}
