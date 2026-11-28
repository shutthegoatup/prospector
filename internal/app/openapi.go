package app

import (
	"context"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/marketing"
	"github.com/shutthegoatup/prospector/internal/onboarding"
	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/privacy"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/sales"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/signing"
	"github.com/shutthegoatup/prospector/internal/store"
	"github.com/shutthegoatup/prospector/internal/tickets"
)

// The API in api/openapi.yaml is the one way to do anything: the web app calls it with
// its session, agents and scripts with a key. api.gen.go is generated from the spec.

// apiServer implements the spec's operations.
type apiServer struct {
	a *App
	signing.SigningAPI
	sales.SalesAPI
	tickets.TicketsAPI
	onboarding.OnboardingAPI
	marketing.MarketingAPI
	crm.CRMAPI
	privacy.PrivacyAPI
	research.ResearchAPI
}

// newAPIServer hands each product's operations the part of the store they use.
func newAPIServer(a *App) apiServer {
	return apiServer{a: a,
		ResearchAPI:   research.ResearchAPI{Research: &a.s.ResearchStore, Finding: &a.s.ProspectingStore, Records: &a.s.CRMStore, Runs: a},
		PrivacyAPI:    privacy.PrivacyAPI{Store: &a.s.PrivacyStore},
		CRMAPI:        crm.CRMAPI{Records: &a.s.CRMStore},
		SigningAPI:    signing.SigningAPI{Store: &a.s.AgreementStore},
		TicketsAPI:    tickets.TicketsAPI{Enquiries: &a.s.EnquiryStore, Mail: a},
		OnboardingAPI: onboarding.OnboardingAPI{Steps: &a.s.OnboardingStore, Customers: &a.s.CustomerStore, App: a},
		MarketingAPI:  marketing.MarketingAPI{Campaigns: &a.s.OutreachStore, Suppression: &a.s.SuppressionStore, App: a},
		SalesAPI: sales.SalesAPI{Pipeline: &a.s.DealStore, Subscriptions: &a.s.SubscriptionStore,
			People: &a.s.CRMStore, Onboarding: &a.s.OnboardingStore},
	}
}

var _ api.StrictServerInterface = apiServer{}

// routeAPI puts the spec's operations on the mux, each behind who-is-asking and the
// scope its operation names.
func (a *App) routeAPI(mux *http.ServeMux) {
	spec, err := api.GetSpec()
	if err != nil {
		log.Fatalf("the API spec: %v", err)
	}
	scopes, account, keysOnly, agentOps := map[string][]string{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			if s := opScopes(op); len(s) > 0 {
				scopes[op.OperationID] = s
				if !sessionAllowed(op) {
					keysOnly[method+" "+path], agentOps[op.OperationID] = true, true
				}
			}
			if slices.Contains(op.Tags, "account") {
				account[method+" "+path] = true
			}
		}
	}
	doc, err := api.SpecJSON()
	if err != nil {
		log.Fatalf("the API spec: %v", err)
	}
	mux.HandleFunc("GET /api/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(doc)
	})

	strict := api.NewStrictHandlerWithOptions(newAPIServer(a), []api.StrictMiddlewareFunc{a.apiScope(scopes), func(f api.StrictHandlerFunc, _ string) api.StrictHandlerFunc { return listURL(f) }, uploads, answerAfterBody(agentOps), withRequest, a.withSite}, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			// The agents' operations say it as the routes they came from did.
			if keysOnly[r.Pattern] {
				request.WriteProblem(w, r, http.StatusBadRequest, say.Errorf("bad request body: %s", err.Error()))
				return
			}
			request.WriteProblem(w, r, http.StatusBadRequest, say.New(err.Error()))
		},
		ResponseErrorHandlerFunc: apiFail,
	})
	var router api.ServeMux = mux
	if watchAPI != nil {
		router = watchAPI(mux)
	}
	api.HandlerWithOptions(strict, api.StdHTTPServerOptions{
		BaseRouter:  router,
		Middlewares: []api.MiddlewareFunc{a.apiCaller(account, keysOnly), noEmptyLink},
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			request.WriteProblem(w, r, http.StatusBadRequest, say.New(err.Error()))
		},
	})
}

// answerAfterBody starts an agent operation's answer clock once its body is in, as
// the routes it came from did, so a slow upload doesn't use up the time to answer.
func answerAfterBody(ops map[string]bool) api.StrictMiddlewareFunc {
	return func(f api.StrictHandlerFunc, op string) api.StrictHandlerFunc {
		if !ops[op] {
			return f
		}
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, in any) (any, error) {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(answerLimit))
			return f(ctx, w, r, in)
		}
	}
}

// agentBodyLimit is the most an agent's write may send.
const agentBodyLimit = 64 << 20

// watchAPI, when set, wraps the router the operations go on, so the tests can hold
// the spec to what the server really answers.
var watchAPI func(*http.ServeMux) api.ServeMux

// apiCaller works out who is asking: a key, or a signed-in member of the organisation.
// The account operations, named by route, are asked by whoever is there; those whose
// security names only a key are for keys alone.
func (a *App) apiCaller(account, keysOnly map[string]bool) api.MiddlewareFunc {
	return func(next http.Handler) http.Handler { return a.caller(account, keysOnly, next) }
}

func (a *App) caller(account, keysOnly map[string]bool, next http.Handler) http.Handler {
	member := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hasKey(r) && r.PathValue("org") == "" {
			request.WriteProblem(w, r, http.StatusForbidden, say.New("a key belongs to one organisation and can't start another: sign in to do that"))
			return
		}
		if hasKey(r) || keysOnly[r.Pattern] {
			// What a key sends is held to what the routes it came from always took.
			if keysOnly[r.Pattern] {
				r.Body = http.MaxBytesReader(w, r.Body, agentBodyLimit)
			}
			a.agent("", next.ServeHTTP)(w, r)
			return
		}
		if _, u := a.session(r); u == nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="prospector"`)
			request.WriteTyped(w, r, http.StatusUnauthorized, api.ProblemTypeSignedOut, say.New("sign in, or send an API key: Authorization: Bearer pk_…"))
			return
		}
		a.member(next.ServeHTTP)(w, r)
	})
	anyone := a.accountCaller(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if account[r.Pattern] {
			anyone.ServeHTTP(w, r)
			return
		}
		member.ServeHTTP(w, r)
	})
}

// apiScope holds an operation to its scope: a key needs it granted; a member has everything
// but configuring, which needs an owner.
func (a *App) apiScope(scopes map[string][]string) api.StrictMiddlewareFunc {
	return func(f api.StrictHandlerFunc, op string) api.StrictHandlerFunc {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, in any) (any, error) {
			q, need := request.ReqFrom(ctx), scopes[op]
			switch {
			// One that names no key scope is for people signed in, never a key.
			case q.Key != nil && (keyless[op] || len(need) == 0):
				refuseKeys(w, r)
				return nil, nil
			case q.Key != nil && !slices.ContainsFunc(need, q.Key.Has):
				challenge(w, r, http.StatusForbidden, "insufficient_scope", need[0], say.Errorf("this key lacks the %s scope", strings.Join(need, " or ")))
				return nil, nil
			case q.Key == nil && slices.ContainsFunc(need, ownersOnly) && !q.Owner():
				request.WriteProblem(w, r, http.StatusForbidden, say.New("only an owner of this organisation can change that"))
				return nil, nil
			case q.Key == nil && q.Org != nil && !q.Member.Works(opArea[op]):
				request.WriteProblem(w, r, http.StatusForbidden, areaRefusal(opArea[op]))
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

// sessionAllowed is whether a person signed in may make op, besides a key.
func sessionAllowed(op *openapi3.Operation) bool {
	if op.Security == nil {
		return true
	}
	for _, req := range *op.Security {
		if _, ok := req["session"]; ok {
			return true
		}
	}
	return false
}

func ownersOnly(scope string) bool {
	switch scope {
	case "agents:configure", "enquiries:delete", "settings:write", "people:privacy", "team:write", "mail:configure", "sso:configure", "org:export":
		return true
	}
	return false
}

// agentOut is an agent with its card worked out as the Agents page shows it; all is
// every agent of the organisation, the ones it could fall back to among them.
func agentOut(c store.Agent, all []store.Agent, lang string) api.Agent {
	v := agentView(c, all)
	st := v.State()
	tools := []api.Tool{}
	for _, t := range c.ToolList() {
		tools = append(tools, api.Tool{Name: t.Name, Description: t.Description})
	}
	return api.Agent{Id: c.ID, Name: c.Name, Kind: api.AgentKind(c.Kind), BaseUrl: c.BaseURL, Model: c.Model, ToolsUrl: c.ToolsURL, SearchUrl: c.SearchURL,
		Models: nonNil(c.ModelList()), Tools: tools, AllowedTools: nonNil(c.AllowedTools()), Fallback: c.FallbackID, FallbackMinutes: c.FallbackMinutes,
		ThinkingOff: c.ThinkingOff, Problem: c.Problem, CheckedAt: c.CheckedAt, HasKey: c.APIKey != "", Ready: c.Ready(), UpdatedBy: c.UpdatedBy, UpdatedAt: c.UpdatedAt,
		State: api.AgentState{Rail: api.AgentStateRail(st.Rail), Line: st.Line.In(lang), Todo: st.Todo.In(lang)}, Searches: c.Searches(), SearchText: v.SearchText().In(lang),
		ToolsText: v.ToolsText().In(lang), FallbackText: v.FallbackText().In(lang)}
}

// oneAgentOut is agentOut for an agent read on its own.
func (s apiServer) oneAgentOut(ctx context.Context, org int64, c store.Agent) (api.Agent, error) {
	all, err := s.a.s.Agents(org)
	return agentOut(c, all, request.LanguageFrom(ctx)), err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func settingsIn(id int64, b *api.AgentSettings) (AgentSettingsIn, error) {
	if b == nil {
		return AgentSettingsIn{}, say.New("send the agent's settings")
	}
	return AgentSettingsIn{ID: id, Name: b.Name, Kind: b.Kind, BaseURL: b.BaseUrl, APIKey: b.ApiKey, Model: b.Model, ToolsURL: b.ToolsUrl, SearchURL: b.SearchUrl,
		Tools: nonNil(b.Tools), Fallback: b.Fallback, FallbackMinutes: b.FallbackMinutes, ThinkingOff: b.ThinkingOff}, nil
}

// savedMessage says how a save went, for a person.
func savedMessage(c *store.Agent) string {
	switch {
	case c.Problem != "":
		return c.Name + " saved, but " + c.Problem
	case !c.Ready():
		return c.Name + " saved. Pick a model to finish."
	}
	return c.Name + " saved."
}

func (s apiServer) ListAgents(ctx context.Context, in api.ListAgentsRequestObject) (api.ListAgentsResponseObject, error) {
	all, err := s.a.s.Agents(request.ReqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	list, next, err := paging.ListPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListAgents200JSONResponse{Headers: api.ListAgents200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.Agent{}, next
	for _, c := range list {
		out.Body.Items = append(out.Body.Items, agentOut(c, all, request.LanguageFrom(ctx)))
	}
	return out, nil
}

func (s apiServer) GetAgent(ctx context.Context, in api.GetAgentRequestObject) (api.GetAgentResponseObject, error) {
	org := request.ReqFrom(ctx).Org.ID
	c, err := s.a.s.AgentByID(org, in.Id)
	if err != nil {
		return nil, err
	}
	out, err := s.oneAgentOut(ctx, org, *c)
	return api.GetAgent200JSONResponse(out), err
}

func (s apiServer) CreateAgent(ctx context.Context, in api.CreateAgentRequestObject) (api.CreateAgentResponseObject, error) {
	settings, err := settingsIn(0, in.Body)
	if err != nil {
		return nil, err
	}
	q := request.ReqFrom(ctx)
	c, err := s.a.saveAgent(ctx, q.Org.ID, settings, q.Actor())
	if err != nil {
		return nil, err
	}
	out, err := s.oneAgentOut(ctx, q.Org.ID, *c)
	return api.CreateAgent201JSONResponse{Agent: out, Message: savedMessage(c)}, err
}

func (s apiServer) UpdateAgent(ctx context.Context, in api.UpdateAgentRequestObject) (api.UpdateAgentResponseObject, error) {
	settings, err := settingsIn(in.Id, in.Body)
	if err != nil {
		return nil, err
	}
	q := request.ReqFrom(ctx)
	c, err := s.a.saveAgent(ctx, q.Org.ID, settings, q.Actor())
	if err != nil {
		return nil, err
	}
	out, err := s.oneAgentOut(ctx, q.Org.ID, *c)
	return api.UpdateAgent200JSONResponse{Agent: out, Message: savedMessage(c)}, err
}

func (s apiServer) DeleteAgent(ctx context.Context, in api.DeleteAgentRequestObject) (api.DeleteAgentResponseObject, error) {
	if err := s.a.deleteAgent(request.ReqFrom(ctx).Org.ID, in.Id); err != nil {
		return nil, err
	}
	return api.DeleteAgent204Response{}, nil
}

func (s apiServer) ListRuns(ctx context.Context, in api.ListRunsRequestObject) (api.ListRunsResponseObject, error) {
	org := request.ReqFrom(ctx).Org.ID
	if _, err := s.a.s.TaskByID(org, in.Task); err != nil {
		return nil, err
	}
	runs, err := s.a.s.AgentRuns(org, in.Task, 0)
	if err != nil {
		return nil, err
	}
	runs, next, err := paging.ListPage(runs, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListRuns200JSONResponse{Headers: api.ListRuns200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.Run{}, next
	for _, r := range runs {
		out.Body.Items = append(out.Body.Items, research.RunOut(r))
	}
	return out, nil
}

func (s apiServer) StartRun(ctx context.Context, in api.StartRunRequestObject) (api.StartRunResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("say which agent: {\"agent\": id}")
	}
	q := request.ReqFrom(ctx)
	run, err := s.a.StartRun(q.Org.ID, in.Task, in.Body.Agent, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.StartRun201JSONResponse(research.RunOut(*run)), nil
}

func (s apiServer) StopRun(ctx context.Context, in api.StopRunRequestObject) (api.StopRunResponseObject, error) {
	if err := s.a.StopRun(request.ReqFrom(ctx).Org.ID, in.Id); err != nil {
		return nil, err
	}
	return api.StopRun204Response{}, nil
}
