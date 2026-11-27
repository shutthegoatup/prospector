package research

import (
	"context"
	"time"

	"github.com/shutthegoatup/prospector/api"

	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/store"
)

func (s ResearchAPI) ListTasks(ctx context.Context, in api.ListTasksRequestObject) (api.ListTasksResponseObject, error) {
	q := request.ReqFrom(ctx)
	page, err := paging.PageIn(int(in.Params.Limit), string(in.Params.Cursor))
	if err != nil {
		return nil, err
	}
	tasks, next, err := s.Research.TaskBriefsFor(q.Org.ID, request.Worker(q), in.Params.Closed, in.Params.Offering, page)
	if err != nil {
		return nil, err
	}
	open, all, err := s.Research.TaskCounts(q.Org.ID)
	if err != nil {
		return nil, err
	}
	out := api.ListTasks200JSONResponse{Headers: api.ListTasks200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Open, out.Body.All = int(open), int(all)
	if out.Body.Items, err = request.Recast[[]api.TaskBrief](tasks); err != nil {
		return nil, err
	}
	if out.Body.Items == nil {
		out.Body.Items = []api.TaskBrief{}
	}
	out.Body.NextCursor = next
	return out, nil
}

func (s ResearchAPI) GetTask(ctx context.Context, in api.GetTaskRequestObject) (api.GetTaskResponseObject, error) {
	q := request.ReqFrom(ctx)
	page, err := paging.PageIn(int(in.Params.Limit), string(in.Params.Cursor))
	if err != nil {
		return nil, err
	}
	t, err := s.Research.AgentTask(q.Org.ID, request.Worker(q), in.Id, page)
	if err != nil {
		return nil, err
	}
	body, err := request.Recast[api.TaskOut](t)
	if err != nil {
		return nil, err
	}
	return api.GetTask200JSONResponse{Body: body, Headers: api.GetTask200ResponseHeaders{Link: request.NextLink(ctx, t.NextCursor)}}, nil
}

func (s ResearchAPI) AddNote(ctx context.Context, in api.AddNoteRequestObject) (api.AddNoteResponseObject, error) {
	q := request.ReqFrom(ctx)
	who := request.Worker(q)
	who.ReadOnly = false
	note, err := s.Research.AgentNote(q.Org.ID, who, NoteIn{Task: in.Body.Task, Company: in.Body.Company, Kind: in.Body.Kind, Text: in.Body.Text})
	if err != nil {
		return nil, err
	}
	body, err := request.Recast[api.Note](note)
	if err != nil {
		return nil, err
	}
	return api.AddNote200JSONResponse(body), nil
}

func (s ResearchAPI) FinishRequest(ctx context.Context, in api.FinishRequestRequestObject) (api.FinishRequestResponseObject, error) {
	q := request.ReqFrom(ctx)
	who := request.Worker(q)
	who.ReadOnly = false
	if err := s.Research.AgentFinish(q.Org.ID, who, FinishIn{Request: in.Body.Request}); err != nil {
		return nil, err
	}
	return api.FinishRequest200JSONResponse{Ok: true}, nil
}

func (s ResearchAPI) GetTaskPrompt(ctx context.Context, in api.GetTaskPromptRequestObject) (api.GetTaskPromptResponseObject, error) {
	q := request.ReqFrom(ctx)
	t, err := s.Research.TaskByID(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	return api.GetTaskPrompt200JSONResponse{Prompt: TaskPrompt(q.Org, *t)}, nil
}

func (s ResearchAPI) ListTaskNotes(ctx context.Context, in api.ListTaskNotesRequestObject) (api.ListTaskNotesResponseObject, error) {
	q := request.ReqFrom(ctx)
	if _, err := s.Research.TaskByID(q.Org.ID, in.Id); err != nil {
		return nil, err
	}
	page, err := paging.PageIn(in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	notes, next, err := s.Research.TaskNotes(q.Org.ID, in.Id, page)
	if err != nil {
		return nil, err
	}
	who := request.Worker(q)
	out := api.ListTaskNotes200JSONResponse{Headers: api.ListTaskNotes200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.TaskNoteOut{}, next
	for _, n := range notes {
		out.Body.Items = append(out.Body.Items, api.TaskNoteOut{Id: n.ID, Kind: n.Kind, Text: n.Text, Actor: n.Actor, At: n.CreatedAt,
			Company: n.Company, CompanyId: n.CompanyID, ByYou: who.Did(store.Event{User: n.Actor, KeyID: n.KeyID})})
	}
	return out, nil
}

func (s ResearchAPI) ListTaskWork(ctx context.Context, in api.ListTaskWorkRequestObject) (api.ListTaskWorkResponseObject, error) {
	q := request.ReqFrom(ctx)
	now := time.Now()
	t, err := s.Research.Task(q.Org.ID, in.Id, now)
	if err != nil {
		return nil, err
	}
	runs, next, err := paging.ListPage(t.Runs, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListTaskWork200JSONResponse{Headers: api.ListTaskWork200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.TaskWork{}, next
	for _, r := range runs {
		out.Body.Items = append(out.Body.Items, api.TaskWork{Actor: r.Actor, Start: r.Start, End: r.End, Going: r.Going(now),
			Changes: r.Changes, Companies: r.Companies, People: r.People, Drafts: r.Drafts})
	}
	return out, nil
}

func (s ResearchAPI) DeleteNote(ctx context.Context, in api.DeleteNoteRequestObject) (api.DeleteNoteResponseObject, error) {
	if err := s.Research.DeleteNote(request.ReqFrom(ctx).Org.ID, in.Id); err != nil {
		return nil, err
	}
	return api.DeleteNote204Response{}, nil
}

func (s ResearchAPI) ListLeftOut(ctx context.Context, in api.ListLeftOutRequestObject) (api.ListLeftOutResponseObject, error) {
	q := request.ReqFrom(ctx)
	v, err := s.Research.Task(q.Org.ID, in.Id, time.Now())
	if err != nil {
		return nil, err
	}
	left := v.LeftOut()
	page, next, err := paging.ListPage(left, int(in.Params.Limit), string(in.Params.Cursor))
	if err != nil {
		return nil, err
	}
	out := api.ListLeftOut200JSONResponse{Headers: api.ListLeftOut200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.NextCursor, out.Body.Offering = next, v.OfferingID
	out.Body.NeverContact, out.Body.Erased = v.Withheld[WithheldNeverContact], v.Withheld[WithheldErased]
	for _, c := range left {
		switch c.Held {
		case prospecting.HeldRejected:
			out.Body.Rejected++
		case prospecting.HeldCustomer:
			out.Body.Customer++
		case prospecting.HeldOpenDeal:
			out.Body.OpenDeal++
		case prospecting.HeldQualified:
			out.Body.Qualified++
		case HeldNeverContact:
			out.Body.Listed++
		}
	}
	if out.Body.Items, err = request.Recast[[]api.TaskCompany](page); err != nil {
		return nil, err
	}
	if out.Body.Items == nil {
		out.Body.Items = []api.TaskCompany{}
	}
	return out, nil
}
