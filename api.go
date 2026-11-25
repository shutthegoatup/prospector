package main

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (a *App) apiRoutes(mux *http.ServeMux) {
	v1 := "/o/{org}/api/v1/"
	mux.HandleFunc("GET "+v1+"search", a.agent("read", func(w http.ResponseWriter, r *http.Request) {
		hits, more, err := a.s.AgentSearch(reqOf(r).Org.ID, reqOf(r).Agent(), r.URL.Query().Get("q"))
		respond(w, r, SearchOut{hits, more}, err)
	}))
	mux.HandleFunc("GET "+v1+"companies/{ref}", a.agent("read", func(w http.ResponseWriter, r *http.Request) {
		c, err := a.s.AgentCompany(reqOf(r).Org.ID, reqOf(r).Agent(), r.PathValue("ref"))
		if err != nil {
			apiFail(w, r, err)
			return
		}
		writeJSON(w, c)
	}))
	mux.HandleFunc("GET "+v1+"due", a.agent("read", func(w http.ResponseWriter, r *http.Request) {
		items, err := a.s.AgentDue(reqOf(r).Org)
		respond(w, r, items, err)
	}))
	mux.HandleFunc("GET "+v1+"tasks", a.agent("read", func(w http.ResponseWriter, r *http.Request) {
		tasks, more, err := a.s.AgentTasks(reqOf(r).Org.ID, reqOf(r).Agent())
		respond(w, r, tasksOut{tasks, more}, err)
	}))
	mux.HandleFunc("GET "+v1+"tasks/{id}", a.agent("read", func(w http.ResponseWriter, r *http.Request) {
		t, err := a.s.AgentTask(reqOf(r).Org.ID, reqOf(r).Agent(), idParam(r, "id"))
		if err != nil {
			apiFail(w, r, err)
			return
		}
		writeJSON(w, t)
	}))
	mux.HandleFunc("POST "+v1+"notes", a.agent("research:write", func(w http.ResponseWriter, r *http.Request) {
		var in NoteIn
		if decode(w, r, &in) {
			q := reqOf(r)
			out, err := a.s.AgentNote(q.Org.ID, q.Agent(), in)
			respond(w, r, out, err)
		}
	}))
	mux.HandleFunc("POST "+v1+"requests/finish", a.agent("research:write", func(w http.ResponseWriter, r *http.Request) {
		var in FinishIn
		if decode(w, r, &in) {
			q := reqOf(r)
			respond(w, r, map[string]bool{"ok": true}, a.s.AgentFinish(q.Org.ID, q.Agent(), in))
		}
	}))
	mux.HandleFunc("POST "+v1+"companies", a.agent("research:write", func(w http.ResponseWriter, r *http.Request) {
		var in AddCompanyIn
		if decode(w, r, &in) {
			q := reqOf(r)
			out, err := a.s.AgentAddCompany(q.Org.ID, q.Agent(), in)
			respond(w, r, out, err)
		}
	}))
	mux.HandleFunc("POST "+v1+"facts", a.agent("research:write", func(w http.ResponseWriter, r *http.Request) {
		var in AddFactIn
		if decode(w, r, &in) {
			q := reqOf(r)
			out, err := a.s.AgentAddFact(q.Org.ID, q.Agent(), in)
			respond(w, r, out, err)
		}
	}))
	mux.HandleFunc("POST "+v1+"contacts", a.agent("research:write", func(w http.ResponseWriter, r *http.Request) {
		var in AddContactIn
		if decode(w, r, &in) {
			q := reqOf(r)
			out, err := a.s.AgentAddContact(q.Org, q.Agent(), in)
			respond(w, r, out, err)
		}
	}))
	mux.HandleFunc("POST "+v1+"drafts", a.agent("drafts:write", func(w http.ResponseWriter, r *http.Request) {
		var in DraftIn
		if decode(w, r, &in) {
			q := reqOf(r)
			respond(w, r, map[string]bool{"ok": true}, a.s.AgentDraft(q.Org.ID, q.Agent(), in))
		}
	}))
	mux.HandleFunc("POST "+v1+"marks", a.agent("outcomes:write", func(w http.ResponseWriter, r *http.Request) {
		var in MarkIn
		if decode(w, r, &in) {
			q := reqOf(r)
			respond(w, r, map[string]bool{"ok": true}, a.s.AgentMark(q.Org.ID, q.Agent(), in))
		}
	}))
	mux.HandleFunc("POST "+v1+"enquiries", a.agent("enquiries:write", func(w http.ResponseWriter, r *http.Request) {
		var in EnquiryIn
		if decode(w, r, &in) {
			q := reqOf(r)
			in.Trap = ""
			e, err := a.s.AddEnquiry(q.Org.ID, q.Actor(), in)
			respond(w, r, e, err)
		}
	}))
	mux.HandleFunc("POST "+v1+"import", a.agent("import", func(w http.ResponseWriter, r *http.Request) {
		var p ImportPayload
		if decode(w, r, &p) {
			total, err := a.s.Import(reqOf(r).Org.ID, &p)
			respond(w, r, map[string]int64{"contacts": total}, err)
		}
	}))
	mux.HandleFunc("GET "+v1+"snapshot", a.agent("import", func(w http.ResponseWriter, r *http.Request) {
		text, err := a.s.SnapshotText(reqOf(r).Org.ID)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		w.Header().Set("content-type", "text/plain; charset=utf-8")
		w.Write([]byte(text))
	}))

	mcpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return a.mcpServer(reqOf(r))
	}, nil)
	// The SDK ties each session to this id, so one key can't drive another key's session.
	sameKey := auth.RequireBearerToken(func(_ context.Context, _ string, r *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{UserID: strconv.FormatInt(reqOf(r).Key.ID, 10)}, nil
	}, &auth.RequireBearerTokenOptions{AllowMissingExpiration: true})
	mux.Handle("/o/{org}/mcp", a.agent("", streaming(sameKey(mcpHandler))))
}

func respond(w http.ResponseWriter, r *http.Request, v any, err error) {
	if err != nil {
		apiFail(w, r, err)
		return
	}
	writeJSON(w, v)
}

type ok struct {
	OK bool `json:"ok"`
}

type tasksOut struct {
	Tasks []TaskBrief `json:"tasks"`
	More  bool        `json:"more" jsonschema:"true if there are more open tasks than are listed here"`
}

type taskRef struct {
	ID int64 `json:"id" jsonschema:"the task's number"`
}

type factAdded struct {
	OK      bool `json:"ok"`
	Existed bool `json:"existed" jsonschema:"true if the company already had this fact from this page"`
}

// mcpServer is built once per API key, with only the tools its scopes allow. The
// key is checked again on every request, so revoking it cuts off its sessions, and
// a session answers only to the key that opened it.
func (a *App) mcpServer(q *Req) *mcp.Server {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s, found := a.mcp[q.Key.ID]; found {
		return s
	}
	org, actor, k := q.Org, q.Agent(), q.Key
	// Each session holds its own claims, so two clients sharing a key take different requests.
	sessionOf := func(r *mcp.CallToolRequest) Actor {
		who := actor
		if r != nil && r.Session != nil {
			who.Session = r.Session.ID()
		}
		return who
	}
	// Settings saved while a session is open (waits, the never-contact list) apply to its next call.
	current := func() (*Org, error) { return a.s.OrgByID(org.ID) }
	s := mcp.NewServer(&mcp.Implementation{Name: "prospector", Title: "Prospector: " + org.Name, Version: "1"},
		&mcp.ServerOptions{Instructions: agentInstructions(org, k)})
	if k.Has("read") {
		mcp.AddTool(s, &mcp.Tool{Name: "guidance", Description: "How to research and draft for this organisation: the same text as the server instructions. Read it first if your client doesn't show those."},
			func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, *GuidanceOut, error) {
				return nil, &GuidanceOut{Guidance: agentInstructions(org, k)}, nil
			})
		mcp.AddTool(s, &mcp.Tool{Name: "search", Description: "Find companies and contacts by name, domain, email or notes. Each company says which agent last changed it."},
			func(_ context.Context, req *mcp.CallToolRequest, in SearchIn) (*mcp.CallToolResult, SearchOut, error) {
				hits, more, err := a.s.AgentSearch(org.ID, sessionOf(req), in.Query)
				return nil, SearchOut{hits, more}, err
			})
		mcp.AddTool(s, &mcp.Tool{Name: "get_company", Description: "A company's details, facts with sources, people, and its history: what people and agents did here, newest first. Read the history before researching, so you don't redo work or suggest what a person rejected."},
			func(_ context.Context, req *mcp.CallToolRequest, in struct {
				Company string `json:"company" jsonschema:"company id or exact name"`
			}) (*mcp.CallToolResult, *CompanyDetail, error) {
				c, err := a.s.AgentCompany(org.ID, sessionOf(req), in.Company)
				return nil, c, err
			})
		mcp.AddTool(s, &mcp.Tool{Name: "list_due", Description: "What is due on Today, and whether its text is written yet."},
			func(_ context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []DueItem, error) {
				o, err := current()
				if err != nil {
					return nil, nil, err
				}
				items, err := a.s.AgentDue(o)
				return nil, items, err
			})
		mcp.AddTool(s, &mcp.Tool{Name: "list_tasks", Description: "The open tasks people have written for agents: what each asks for, what is still unfinished, and whether another agent is on it now."},
			func(_ context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, tasksOut, error) {
				tasks, more, err := a.s.AgentTasks(org.ID, sessionOf(req))
				return nil, tasksOut{tasks, more}, err
			})
		mcp.AddTool(s, &mcp.Tool{Name: "get_task", Description: "Everything needed to carry a task on: the brief in the person's words, what has been asked since, the companies and people already found, and the notes other agents left. Read it before working a task; people see that you picked it up. With a key that can add research, it also keeps an open request for you (claimed_request): work that one. If other agents hold every open request, leave the task to them."},
			func(_ context.Context, req *mcp.CallToolRequest, in taskRef) (*mcp.CallToolResult, *TaskOut, error) {
				out, err := a.s.AgentTask(org.ID, sessionOf(req), in.ID)
				return nil, out, err
			})
		s.AddPrompt(&mcp.Prompt{Name: "work_task", Description: "Start on a task a person wrote in Prospector.",
			Arguments: []*mcp.PromptArgument{{Name: "id", Description: "the task's number, from its page or list_tasks", Required: true}}},
			func(_ context.Context, r *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
				id, _ := strconv.ParseInt(strings.TrimSpace(r.Params.Arguments["id"]), 10, 64)
				t, err := a.s.Task(org.ID, id, time.Now())
				if err != nil {
					return nil, err
				}
				return &mcp.GetPromptResult{Description: t.Title, Messages: []*mcp.PromptMessage{
					{Role: "user", Content: &mcp.TextContent{Text: taskPrompt(org, t.Task)}}}}, nil
			})
	}
	if k.Has("research:write") {
		mcp.AddTool(s, &mcp.Tool{Name: "task_note", Description: "Leave a short working note on a task or a company for whoever comes next: what you skipped and why, where you found nothing, or what you would do next. Not a fact, and not for naming people."},
			func(_ context.Context, req *mcp.CallToolRequest, in NoteIn) (*mcp.CallToolResult, ok, error) {
				_, err := a.s.AgentNote(org.ID, sessionOf(req), in)
				return nil, ok{err == nil}, err
			})
		mcp.AddTool(s, &mcp.Tool{Name: "finish_request", Description: "Say that an open request on a task is done. A person can reopen it."},
			func(_ context.Context, req *mcp.CallToolRequest, in FinishIn) (*mcp.CallToolResult, ok, error) {
				err := a.s.AgentFinish(org.ID, sessionOf(req), in)
				return nil, ok{err == nil}, err
			})
		mcp.AddTool(s, &mcp.Tool{Name: "add_company", Description: "Add a company to Research for a person to review, with facts and their sources. If its name or domain is already known, the facts it lacks are added to that company."},
			func(_ context.Context, req *mcp.CallToolRequest, in AddCompanyIn) (*mcp.CallToolResult, *AddCompanyOut, error) {
				out, err := a.s.AgentAddCompany(org.ID, sessionOf(req), in)
				return nil, out, err
			})
		mcp.AddTool(s, &mcp.Tool{Name: "add_fact", Description: "Add one fact about a company, with the page that shows it. A fact the company already has from that page isn't added twice."},
			func(_ context.Context, req *mcp.CallToolRequest, in AddFactIn) (*mcp.CallToolResult, factAdded, error) {
				out, err := a.s.AgentAddFact(org.ID, sessionOf(req), in)
				return nil, factAdded{OK: err == nil, Existed: err == nil && out.Existed}, err
			})
		mcp.AddTool(s, &mcp.Tool{Name: "add_contact", Description: "Suggest a person at a company. They wait in Research until a person picks them. Someone already at the company comes back as existed: an email, phone or profile you send that they lack isn't filled in, it waits for a person to accept and is listed in suggested. Never scrape LinkedIn; use links found by search."},
			func(_ context.Context, req *mcp.CallToolRequest, in AddContactIn) (*mcp.CallToolResult, *AddContactOut, error) {
				o, err := current()
				if err != nil {
					return nil, nil, err
				}
				out, err := a.s.AgentAddContact(o, sessionOf(req), in)
				return nil, out, err
			})
	}
	if k.Has("drafts:write") {
		mcp.AddTool(s, &mcp.Tool{Name: "draft_message", Description: "Write the LinkedIn note (300 characters at most), LinkedIn message, email or follow-up for a contact. It shows as written by an agent; a person edits and sends it. Text a person has edited, or that has already gone out, is refused."},
			func(_ context.Context, req *mcp.CallToolRequest, in DraftIn) (*mcp.CallToolResult, ok, error) {
				err := a.s.AgentDraft(org.ID, sessionOf(req), in)
				return nil, ok{err == nil}, err
			})
	}
	if k.Has("outcomes:write") {
		mcp.AddTool(s, &mcp.Tool{Name: "mark", Description: "Record an outcome for a contact in outreach: LinkedIn request sent, accepted, email or follow-up sent, called, replied, declined or on hold. It shows as yours, with your note."},
			func(_ context.Context, req *mcp.CallToolRequest, in MarkIn) (*mcp.CallToolResult, ok, error) {
				err := a.s.AgentMark(org.ID, sessionOf(req), in)
				return nil, ok{err == nil}, err
			})
	}
	if a.mcp == nil {
		a.mcp = map[int64]*mcp.Server{}
	}
	a.mcp[k.ID] = s
	return s
}

// dropMCPServers makes new sessions pick up changed settings; open sessions keep
// the instructions they were given when they connected.
func (a *App) dropMCPServers() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mcp = nil
}
