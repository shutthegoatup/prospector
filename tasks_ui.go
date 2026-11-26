package main

import (
	"net/http"
	"time"
)

func (a *App) taskRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /o/{org}/tasks", a.member(a.tasksPage))
	mux.HandleFunc("GET /o/{org}/tasks/{id}", a.member(a.taskPage))
	mux.HandleFunc("POST /o/{org}/ui/note/delete", a.works(areaSales, a.uiDeleteNote))
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
	a.page(w, r, "research", TasksView(q, TasksData{Rows: rows, Page: pg, Closed: closed, Open: int(open), All: int(all), Now: now}))
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
	d.Query = r.URL.Query()
	notes, notesPage, err := a.s.TaskNotesPage(q.Org.ID, t.ID, panelPage(r, "notes"))
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	d.Notes, d.NotesPage = notes, notesPage
	runs, err := a.s.AgentRuns(q.Org.ID, t.ID, 0)
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	// Runs still going are all shown, with what stops them; those that ended come a page at a time.
	var going, ended []AgentRun
	for _, run := range runs {
		if run.Going() {
			going = append(going, run)
		} else {
			ended = append(ended, run)
		}
	}
	ended, d.EndedPage = pageSlice(ended, panelPage(r, "runs"), taskRunsPageSize)
	d.AgentRuns = append(going, ended...)
	d.Runs, d.WorkPage = pageSlice(d.Runs, panelPage(r, "work"), taskRunsPageSize)
	a.page(w, r, "research", TaskDetailView(q, d))
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
