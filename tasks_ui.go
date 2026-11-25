package main

import (
	"net/http"
	"strconv"
	"time"
)

func (a *App) taskRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /o/{org}/tasks", a.member(a.tasksPage))
	mux.HandleFunc("GET /o/{org}/tasks/{id}", a.member(a.taskPage))
	mux.HandleFunc("POST /o/{org}/ui/task", a.member(a.uiTask))
	mux.HandleFunc("POST /o/{org}/ui/task/ask", a.member(a.uiTaskAsk))
	mux.HandleFunc("POST /o/{org}/ui/task/close", a.member(a.uiTaskClosed(true)))
	mux.HandleFunc("POST /o/{org}/ui/task/reopen", a.member(a.uiTaskClosed(false)))
	mux.HandleFunc("POST /o/{org}/ui/task/request/done", a.member(a.uiRequestDone(true)))
	mux.HandleFunc("POST /o/{org}/ui/task/request/reopen", a.member(a.uiRequestDone(false)))
	mux.HandleFunc("POST /o/{org}/ui/task/agent/start", a.member(a.uiRunStart))
	mux.HandleFunc("POST /o/{org}/ui/task/agent/stop", a.member(a.uiRunStop))
	mux.HandleFunc("POST /o/{org}/ui/note/delete", a.member(a.uiDeleteNote))
}

func (a *App) tasksPage(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	now := time.Now()
	closed := r.URL.Query().Get("closed") != ""
	rows, pg, err := a.s.Tasks(q.Org.ID, closed, pageParam(r), now)
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	var open, all int64
	logged(a.s.in(q.Org.ID).Model(&Task{}).Where("closed_at is null").Count(&open))
	logged(a.s.in(q.Org.ID).Model(&Task{}).Count(&all))
	a.page(w, r, "tasks", TasksView(q, TasksData{Rows: rows, Page: pg, Closed: closed, Open: int(open), All: int(all), Now: now}))
}

func (a *App) taskPage(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	now := time.Now()
	t, err := a.s.Task(q.Org.ID, idParam(r, "id"), now)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d := TaskPage{TaskView: *t, Prompt: taskPrompt(q.Org, t.Task), Now: now}
	if agents, err := a.s.Agents(q.Org.ID); err == nil {
		for _, c := range agents {
			if c.Ready() {
				d.Agents = append(d.Agents, c)
			}
		}
	}
	d.AgentRuns, _ = a.s.AgentRuns(q.Org.ID, t.ID, 10)
	a.page(w, r, "tasks", TaskDetailView(q, d))
}

func (a *App) uiRunStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Task  int64 `json:"task"`
		Agent int64 `json:"agent"`
	}
	if !decode(w, r, &in) {
		return
	}
	q := reqOf(r)
	run, err := a.StartRun(q.Org.ID, in.Task, in.Agent, q.Actor())
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"message": run.AgentName + " is on it."})
}

func (a *App) uiRunStop(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID int64 `json:"id"`
	}
	if decode(w, r, &in) {
		done(w, a.StopRun(reqOf(r).Org.ID, in.ID))
	}
}

func (a *App) uiTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
		Brief string `json:"brief"`
	}
	if !decode(w, r, &in) {
		return
	}
	q := reqOf(r)
	t, err := a.s.CreateTask(q.Org.ID, in.Title, in.Brief, q.Actor())
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"go": q.Base() + "tasks/" + strconv.FormatInt(t.ID, 10)})
}

func (a *App) uiTaskAsk(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Task int64  `json:"task"`
		Text string `json:"text"`
	}
	if decode(w, r, &in) {
		q := reqOf(r)
		done(w, a.s.AskTask(q.Org.ID, in.Task, in.Text, q.Actor()))
	}
}

func (a *App) uiTaskClosed(closed bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID int64 `json:"id"`
		}
		if decode(w, r, &in) {
			q := reqOf(r)
			done(w, a.s.SetTaskClosed(q.Org.ID, in.ID, closed, q.Actor()))
		}
	}
}

func (a *App) uiRequestDone(isDone bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID int64 `json:"id"`
		}
		if decode(w, r, &in) {
			q := reqOf(r)
			done(w, a.s.SetRequestDone(q.Org.ID, in.ID, isDone, q.Actor()))
		}
	}
}

func (a *App) uiDeleteNote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID int64 `json:"id"`
	}
	if decode(w, r, &in) {
		done(w, a.s.DeleteNote(reqOf(r).Org.ID, in.ID))
	}
}

// taskClosedParam is whether closed tasks are shown, as the page's link carries it.
func taskClosedParam(closed bool) string {
	if closed {
		return "1"
	}
	return ""
}
