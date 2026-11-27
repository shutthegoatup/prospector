package research

import (
	"context"

	"github.com/shutthegoatup/prospector/api"

	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

func taskOut(t store.Task) api.Task {
	return api.Task{Id: t.ID, Title: t.Title, CreatedBy: t.CreatedBy, CreatedAt: t.CreatedAt, ClosedAt: t.ClosedAt, OfferingId: t.OfferingID, Want: t.Want}
}

func requestOut(r store.TaskRequest) api.TaskRequest {
	return api.TaskRequest{Id: r.ID, TaskId: r.TaskID, Text: r.Text, AskedBy: r.AskedBy, AskedAt: r.AskedAt, DoneAt: r.DoneAt, DoneBy: r.DoneBy}
}

func (s ResearchAPI) CreateTask(ctx context.Context, in api.CreateTaskRequestObject) (api.CreateTaskResponseObject, error) {
	q := request.ReqFrom(ctx)
	if len(in.Body.Agents) > 0 && q.Key != nil && !q.Key.Has("agents:run") {
		return nil, request.Refused{Text: say.New("starting agents on the task needs the agents:run scope as well")}
	}
	t, err := s.Research.CreateTaskWanting(q.Org.ID, in.Body.Offering, in.Body.Want, in.Body.Title, in.Body.Brief, q.Actor())
	if err != nil {
		return nil, err
	}
	out := taskOut(*t)
	started := api.TaskStarted{Id: out.Id, Title: out.Title, CreatedBy: out.CreatedBy, CreatedAt: out.CreatedAt, ClosedAt: out.ClosedAt, OfferingId: out.OfferingId, Want: out.Want,
		Runs: []api.Run{}, NotStarted: []string{}}
	for _, agent := range in.Body.Agents {
		run, err := s.Runs.StartRun(q.Org.ID, t.ID, agent, q.Actor())
		if err != nil {
			started.NotStarted = append(started.NotStarted, err.Error())
			continue
		}
		started.Runs = append(started.Runs, RunOut(*run))
	}
	return api.CreateTask201JSONResponse(started), nil
}

func (s ResearchAPI) ChangeTask(ctx context.Context, in api.ChangeTaskRequestObject) (api.ChangeTaskResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.Research.SetTaskClosed(q.Org.ID, in.Id, in.Body.Closed, q.Actor()); err != nil {
		return nil, err
	}
	t, err := s.Research.TaskByID(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	return api.ChangeTask200JSONResponse(taskOut(*t)), nil
}

func (s ResearchAPI) AskTask(ctx context.Context, in api.AskTaskRequestObject) (api.AskTaskResponseObject, error) {
	q := request.ReqFrom(ctx)
	r, err := s.Research.AskTask(q.Org.ID, in.Id, in.Body.Text, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.AskTask201JSONResponse(requestOut(*r)), nil
}

func (s ResearchAPI) ChangeTaskRequest(ctx context.Context, in api.ChangeTaskRequestRequestObject) (api.ChangeTaskRequestResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.Research.SetRequestDone(q.Org.ID, in.Id, in.Body.Done, q.Actor()); err != nil {
		return nil, err
	}
	r, err := s.Research.RequestByID(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	return api.ChangeTaskRequest200JSONResponse(requestOut(*r)), nil
}

// ResearchAPI is research's operations: tasks and what agents bring back to them, the
// offerings and roles companies are judged against, and the Research queue, which
// apiServer embeds.
type ResearchAPI struct {
	Research *ResearchStore
	Finding  *prospecting.ProspectingStore
	Records  *crm.CRMStore
	Runs     researchRuns
}

// researchRuns is what research asks of the app: starting an agent on a task.
type researchRuns interface {
	StartRun(org, task, agent int64, by string) (*store.AgentRun, error)
}

func RunOut(r store.AgentRun) api.Run {
	return api.Run{Id: r.ID, Task: r.TaskID, Agent: r.AgentID, AgentName: r.AgentName, Request: r.RequestID, StartedBy: r.StartedBy, Model: r.Model,
		Status: api.RunStatus(r.Status), StopAsked: r.StopAsked, StartedAt: r.StartedAt, EndedAt: r.EndedAt, WaitUntil: r.WaitUntil, Turns: r.Turns,
		Episodes: r.Episodes, ToolCalls: r.ToolCalls, Added: r.Added, Resumes: r.Resumes, TokensIn: r.TokensIn, TokensOut: r.TokensOut, StopReason: r.StopReason}
}
