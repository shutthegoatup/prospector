package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/shutthegoatup/prospector/api"
)

// The API in api/openapi.yaml is the one way to do anything: the web app calls it with
// its session, agents and scripts with a key. api.gen.go is generated from the spec.

// apiServer implements the spec's operations.
type apiServer struct{ a *App }

var _ api.StrictServerInterface = apiServer{}

// routeAPI puts the spec's operations on the mux, each behind who-is-asking and the
// scope its operation names.
func (a *App) routeAPI(mux *http.ServeMux) {
	spec, err := api.GetSwagger()
	if err != nil {
		log.Fatalf("the API spec: %v", err)
	}
	scopes := map[string][]string{}
	for _, item := range spec.Paths.Map() {
		for _, op := range item.Operations() {
			if s := opScopes(op); len(s) > 0 {
				scopes[op.OperationID] = s
			}
		}
	}
	doc, err := api.SpecJSON()
	if err != nil {
		log.Fatalf("the API spec: %v", err)
	}
	mux.HandleFunc("GET /api/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(doc)
	})

	strict := api.NewStrictHandlerWithOptions(apiServer{a}, []api.StrictMiddlewareFunc{a.apiScope(scopes), func(f api.StrictHandlerFunc, _ string) api.StrictHandlerFunc { return listURL(f) }, uploads}, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeProblem(w, http.StatusBadRequest, err.Error())
		},
		ResponseErrorHandlerFunc: apiFail,
	})
	api.HandlerWithOptions(strict, api.StdHTTPServerOptions{
		BaseRouter:  mux,
		Middlewares: []api.MiddlewareFunc{a.apiCaller, noEmptyLink},
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeProblem(w, http.StatusBadRequest, err.Error())
		},
	})
}

// apiCaller works out who is asking: a key, or a signed-in member of the organisation.
func (a *App) apiCaller(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hasKey(r) {
			a.agent("", next.ServeHTTP)(w, r)
			return
		}
		if _, u := a.session(r); u == nil {
			challenge(w, http.StatusUnauthorized, "", "", "sign in, or send an API key: Authorization: Bearer pk_…")
			return
		}
		a.member(next.ServeHTTP)(w, r)
	})
}

// apiScope holds an operation to its scope: a key needs it granted; a member has everything
// but configuring, which needs an owner.
func (a *App) apiScope(scopes map[string][]string) api.StrictMiddlewareFunc {
	return func(f api.StrictHandlerFunc, op string) api.StrictHandlerFunc {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, in any) (any, error) {
			q, need := reqFrom(ctx), scopes[op]
			switch {
			case q.Key != nil && len(need) > 0 && !slices.ContainsFunc(need, q.Key.Has):
				challenge(w, http.StatusForbidden, "insufficient_scope", need[0], "this key lacks the "+strings.Join(need, " or ")+" scope")
				return nil, nil
			case q.Key == nil && slices.ContainsFunc(need, ownersOnly) && !q.Owner():
				writeProblem(w, http.StatusForbidden, "only an owner of this organisation can change that")
				return nil, nil
			case q.Key == nil && !q.Member.Works(opArea[op]):
				writeProblem(w, http.StatusForbidden, areaRefusal(opArea[op]))
				return nil, nil
			}
			return f(ctx, w, r, in)
		}
	}
}

// opScopes are the scopes a key may make an operation with, any one of them.
func opScopes(op *openapi3.Operation) []string {
	var out []string
	if op.Security != nil {
		for _, req := range *op.Security {
			if s := req["bearer"]; len(s) > 0 {
				out = append(out, s[0])
			}
		}
	}
	return out
}

func ownersOnly(scope string) bool { return scope == "agents:configure" || scope == "enquiries:delete" }

func reqFrom(ctx context.Context) *Req { return ctx.Value(reqKey{}).(*Req) }

func agentOut(c Agent) api.Agent {
	tools := []api.Tool{}
	for _, t := range c.ToolList() {
		tools = append(tools, api.Tool{Name: t.Name, Description: t.Description})
	}
	return api.Agent{Id: c.ID, Name: c.Name, Kind: api.AgentKind(c.Kind), BaseUrl: c.BaseURL, Model: c.Model, ToolsUrl: c.ToolsURL, SearchUrl: c.SearchURL,
		Models: nonNil(c.ModelList()), Tools: tools, AllowedTools: nonNil(c.AllowedTools()), Fallback: c.FallbackID, FallbackMinutes: c.FallbackMinutes,
		Problem: c.Problem, CheckedAt: c.CheckedAt, HasKey: c.APIKey != "", Ready: c.Ready(), UpdatedBy: c.UpdatedBy, UpdatedAt: c.UpdatedAt}
}

func runOut(r AgentRun) api.Run {
	return api.Run{Id: r.ID, Task: r.TaskID, Agent: r.AgentID, AgentName: r.AgentName, Request: r.RequestID, StartedBy: r.StartedBy, Model: r.Model,
		Status: api.RunStatus(r.Status), StopAsked: r.StopAsked, StartedAt: r.StartedAt, EndedAt: r.EndedAt, WaitUntil: r.WaitUntil, Turns: r.Turns,
		Episodes: r.Episodes, ToolCalls: r.ToolCalls, Added: r.Added, Resumes: r.Resumes, TokensIn: r.TokensIn, TokensOut: r.TokensOut, StopReason: r.StopReason}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func settingsIn(id int64, b *api.AgentSettings) (AgentSettingsIn, error) {
	if b == nil {
		return AgentSettingsIn{}, errors.New("send the agent's settings")
	}
	return AgentSettingsIn{ID: id, Name: b.Name, Kind: b.Kind, BaseURL: b.BaseUrl, APIKey: b.ApiKey, Model: b.Model, ToolsURL: b.ToolsUrl, SearchURL: b.SearchUrl,
		Tools: nonNil(b.Tools), Fallback: b.Fallback, FallbackMinutes: b.FallbackMinutes}, nil
}

// savedMessage says how a save went, for a person.
func savedMessage(c *Agent) string {
	switch {
	case c.Problem != "":
		return c.Name + " saved, but " + c.Problem
	case !c.Ready():
		return c.Name + " saved. Pick a model to finish."
	}
	return c.Name + " saved."
}

func (s apiServer) ListAgents(ctx context.Context, in api.ListAgentsRequestObject) (api.ListAgentsResponseObject, error) {
	list, err := s.a.s.Agents(reqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	list, next, err := listPage(list, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListAgents200JSONResponse{Headers: api.ListAgents200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.Agent{}, next
	for _, c := range list {
		out.Body.Items = append(out.Body.Items, agentOut(c))
	}
	return out, nil
}

func (s apiServer) GetAgent(ctx context.Context, in api.GetAgentRequestObject) (api.GetAgentResponseObject, error) {
	c, err := s.a.s.AgentByID(reqFrom(ctx).Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	return api.GetAgent200JSONResponse(agentOut(*c)), nil
}

func (s apiServer) CreateAgent(ctx context.Context, in api.CreateAgentRequestObject) (api.CreateAgentResponseObject, error) {
	settings, err := settingsIn(0, in.Body)
	if err != nil {
		return nil, err
	}
	q := reqFrom(ctx)
	c, err := s.a.saveAgent(ctx, q.Org.ID, settings, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.CreateAgent201JSONResponse{Agent: agentOut(*c), Message: savedMessage(c)}, nil
}

func (s apiServer) UpdateAgent(ctx context.Context, in api.UpdateAgentRequestObject) (api.UpdateAgentResponseObject, error) {
	settings, err := settingsIn(in.Id, in.Body)
	if err != nil {
		return nil, err
	}
	q := reqFrom(ctx)
	c, err := s.a.saveAgent(ctx, q.Org.ID, settings, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.UpdateAgent200JSONResponse{Agent: agentOut(*c), Message: savedMessage(c)}, nil
}

func (s apiServer) DeleteAgent(ctx context.Context, in api.DeleteAgentRequestObject) (api.DeleteAgentResponseObject, error) {
	if err := s.a.deleteAgent(reqFrom(ctx).Org.ID, in.Id); err != nil {
		return nil, err
	}
	return api.DeleteAgent204Response{}, nil
}

func (s apiServer) ListRuns(ctx context.Context, in api.ListRunsRequestObject) (api.ListRunsResponseObject, error) {
	org := reqFrom(ctx).Org.ID
	if err := s.a.s.db.Where("org_id = ? and id = ?", org, in.Task).First(&Task{}).Error; err != nil {
		return nil, err
	}
	runs, err := s.a.s.AgentRuns(org, in.Task, 0)
	if err != nil {
		return nil, err
	}
	runs, next, err := listPage(runs, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListRuns200JSONResponse{Headers: api.ListRuns200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.Run{}, next
	for _, r := range runs {
		out.Body.Items = append(out.Body.Items, runOut(r))
	}
	return out, nil
}

func (s apiServer) StartRun(ctx context.Context, in api.StartRunRequestObject) (api.StartRunResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("say which agent: {\"agent\": id}")
	}
	q := reqFrom(ctx)
	run, err := s.a.StartRun(q.Org.ID, in.Task, in.Body.Agent, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.StartRun201JSONResponse(runOut(*run)), nil
}

func (s apiServer) StopRun(ctx context.Context, in api.StopRunRequestObject) (api.StopRunResponseObject, error) {
	if err := s.a.StopRun(reqFrom(ctx).Org.ID, in.Id); err != nil {
		return nil, err
	}
	return api.StopRun204Response{}, nil
}
