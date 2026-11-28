package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// The Agents page's reads and the keys an owner makes for outside agents.

// keyless are operations no key makes, whatever its scopes: a key that could make or
// widen keys could give itself any scope.
var keyless = map[string]bool{"CreateKey": true, "ChangeKey": true, "RevokeKey": true, "CreateOrg": true}

func (s apiServer) GetConnect(ctx context.Context, _ api.GetConnectRequestObject) (api.GetConnectResponseObject, error) {
	q := request.ReqFrom(ctx)
	base := s.a.baseURL(ctx.Value(requestKey{}).(*http.Request)) + q.Base()
	out := api.GetConnect200JSONResponse{BaseUrl: base, McpUrl: base + "mcp", CanKeepKeys: s.a.s.Keyring != nil,
		Scopes: []api.Scope{}, Tools: []api.AgentToolScope{}, Kinds: []api.AgentKindLabel{}}
	lang := request.LanguageFrom(ctx)
	for _, sc := range identity.Scopes {
		out.Scopes = append(out.Scopes, api.Scope{Name: sc.Name, What: sc.What.In(lang)})
	}
	for _, t := range mcpTools {
		out.Tools = append(out.Tools, api.AgentToolScope{Name: t.Name, Scope: t.Scope, What: t.What.In(lang)})
	}
	for _, k := range AgentKinds {
		out.Kinds = append(out.Kinds, api.AgentKindLabel{Kind: api.AgentKind(k.Key), Label: k.Label})
	}
	return out, nil
}

func keyOut(k store.APIKey) api.APIKey {
	return api.APIKey{Id: k.ID, Name: k.Name, Prefix: k.Prefix, Scopes: nonNil(strings.Fields(k.Scopes)), CreatedBy: k.CreatedBy,
		CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt, Builtin: k.Prefix == store.BuiltinPrefix}
}

func (s apiServer) ListKeys(ctx context.Context, in api.ListKeysRequestObject) (api.ListKeysResponseObject, error) {
	keys, err := s.a.s.Keys(request.ReqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	keys, next, err := paging.ListPage(keys, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListKeys200JSONResponse{Headers: api.ListKeys200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.APIKey{}, next
	for _, k := range keys {
		out.Body.Items = append(out.Body.Items, keyOut(k))
	}
	return out, nil
}

func (s apiServer) keyByID(org, id int64) (api.APIKey, error) {
	keys, err := s.a.s.Keys(org)
	if err != nil {
		return api.APIKey{}, err
	}
	for _, k := range keys {
		if k.ID == id {
			return keyOut(k), nil
		}
	}
	return api.APIKey{}, store.Missing(nil, "no key %d in this organisation", id)
}

func (s apiServer) CreateKey(ctx context.Context, in api.CreateKeyRequestObject) (api.CreateKeyResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send the key's name and scopes")
	}
	q := request.ReqFrom(ctx)
	secret, k, err := s.a.s.CreateKey(q.Org.ID, in.Body.Name, in.Body.Scopes, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.CreateKey201JSONResponse{Key: keyOut(*k), Secret: secret, Message: "Copy this key now; it isn't shown again: " + secret}, nil
}

func (s apiServer) ChangeKey(ctx context.Context, in api.ChangeKeyRequestObject) (api.ChangeKeyResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send the key's scopes")
	}
	org := request.ReqFrom(ctx).Org.ID
	if err := s.a.s.SetKeyScopes(org, in.Id, in.Body.Scopes); err != nil {
		return nil, err
	}
	k, err := s.keyByID(org, in.Id)
	if err != nil {
		return nil, err
	}
	return api.ChangeKey200JSONResponse{Key: k, Message: "Saved ✓ Agents connected over MCP have to reconnect."}, nil
}

func (s apiServer) RevokeKey(ctx context.Context, in api.RevokeKeyRequestObject) (api.RevokeKeyResponseObject, error) {
	if err := s.a.s.RevokeKey(request.ReqFrom(ctx).Org.ID, in.Id); err != nil {
		return nil, err
	}
	return api.RevokeKey204Response{}, nil
}

func (s apiServer) ListAgentScores(ctx context.Context, in api.ListAgentScoresRequestObject) (api.ListAgentScoresResponseObject, error) {
	scores, next, err := paging.ListPage(s.a.s.AgentScores(request.ReqFrom(ctx).Org.ID), in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListAgentScores200JSONResponse{Headers: api.ListAgentScores200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.AgentScore{}, next
	for _, sc := range scores {
		out.Body.Items = append(out.Body.Items, api.AgentScore{Agent: sc.Agent, Companies: sc.Companies, Qualified: sc.Qualified, Rejected: sc.Rejected,
			Contacts: sc.Contacts, Picked: sc.Picked, Replied: sc.Replied, Text: scoreText(sc).In(request.LanguageFrom(ctx))})
	}
	return out, nil
}

// ListAgentWork pages by time, as the page does: a cursor is the moment the next page's
// runs ended before.
func (s apiServer) ListAgentWork(ctx context.Context, in api.ListAgentWorkRequestObject) (api.ListAgentWorkResponseObject, error) {
	page, err := paging.PageIn(in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	at, err := page.At(1)
	if err != nil {
		return nil, err
	}
	var before time.Time
	if at[0] > 0 {
		before = time.Unix(0, at[0])
	}
	runs, titles, older := s.a.s.Runs(request.ReqFrom(ctx).Org.ID, before, page.Size())
	var next string
	if !older.IsZero() {
		next = paging.Cursor(older.UnixNano())
	}
	now := time.Now()
	out := api.ListAgentWork200JSONResponse{Headers: api.ListAgentWork200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.AgentWork{}, next
	for _, r := range runs {
		w := api.AgentWork{Actor: r.Actor, Start: r.Start, End: r.End, Going: r.Going(now), Changes: r.Changes, Companies: r.Companies,
			People: r.People, Drafts: r.Drafts, Tasks: []api.TaskTitle{}, Text: runText(r, now).In(request.LanguageFrom(ctx))}
		for _, id := range r.Tasks {
			w.Tasks = append(w.Tasks, api.TaskTitle{Id: id, Title: titles[id]})
		}
		out.Body.Items = append(out.Body.Items, w)
	}
	return out, nil
}

func (s apiServer) ListAgentActivity(ctx context.Context, in api.ListAgentActivityRequestObject) (api.ListAgentActivityResponseObject, error) {
	page, err := paging.PageIn(in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	at, err := page.At(2)
	if err != nil {
		return nil, err
	}
	events, nextAt, nextID, total := s.a.s.AgentEventsBefore(request.ReqFrom(ctx).Org.ID, at[0], at[1], page.Size())
	var next string
	if nextAt > 0 {
		next = paging.Cursor(nextAt, nextID)
	}
	out := api.ListAgentActivity200JSONResponse{Headers: api.ListAgentActivity200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor, out.Body.Total = []api.AgentEvent{}, next, total
	for _, e := range events {
		out.Body.Items = append(out.Body.Items, api.AgentEvent{At: e.At, Actor: e.Actor, Action: e.Action, Value: e.Value,
			Text: crm.HistoryText(crm.HistoryItem{Action: e.Action, Value: e.Value}).In(request.LanguageFrom(ctx))})
	}
	return out, nil
}

// refuseKeys answers an operation no key may make.
func refuseKeys(w http.ResponseWriter, r *http.Request) {
	request.WriteProblem(w, r, http.StatusForbidden, say.New("no key can make, change or revoke a key: an owner does that, signed in"))
}

var mcpTools = []struct {
	Name, Scope string
	What        *say.Text
}{
	{"guidance", "read", say.New("how to research and draft for this organisation")},
	{"search", "read", say.New("companies and people by name, domain, email or notes, and which agent was last there")},
	{"get_company", "read", say.New("details, facts with sources, people, and who did what")},
	{"list_due", "read", say.New("what's on Today, and whether its text is written")},
	{"list_tasks", "read", say.New("open tasks people wrote, and whether an agent is on each")},
	{"get_task", "read", say.New("a task's brief, requests, what is found and agents' notes; keeps an open request for a key that can add research")},
	{"task_note", "research:write", say.New("what was skipped or not found, for whoever comes next")},
	{"finish_request", "research:write", say.New("say a request on a task is done")},
	{"add_company", "research:write", say.New("lands in Research to review, with facts")},
	{"add_fact", "research:write", say.New("field, value and source URL")},
	{"add_role", "research:write", say.New("a role open at a company, for an offering with roles; one junked before is refused")},
	{"add_contact", "research:write", say.New("a suggested person, who waits until someone picks them; new details for someone already here wait to be accepted")},
	{"enrich_contact", "enrich:spend", say.New("ask the organisation's paid vendors for a detail someone lacks; what is found waits to be accepted")},
	{"draft_message", "drafts:write", say.New("note, message, email or follow-up, marked as the agent's")},
	{"mark", "outcomes:write", say.New("record an outcome, shown as the agent's with its note")},
}

func scoreText(sc research.AgentScore) say.Sayer {
	var parts []say.Sayer
	if sc.Companies > 0 {
		parts = append(parts, say.Errorf("Companies: %d suggested, %d qualified (%s), %d rejected", sc.Companies, sc.Qualified, percent(sc.Qualified, sc.Companies), sc.Rejected))
	}
	if sc.Contacts > 0 {
		parts = append(parts, say.Errorf("Contacts: %d suggested, %d picked (%s), %d replied", sc.Contacts, sc.Picked, percent(sc.Picked, sc.Contacts), sc.Replied))
	}
	return joinSaid(parts)
}

// joinSaid is parts one after another, a dot between each.
func joinSaid(parts []say.Sayer) say.Sayer {
	if len(parts) == 0 {
		return say.Nothing
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out = say.Errorf("%s · %s", out, p)
	}
	return out
}

func percent(n, of int) string {
	return fmt.Sprintf("%d%%", n*100/of)
}

func runText(r research.Run, now time.Time) say.Sayer {
	span := at(r.Start)
	if mins := int(r.End.Sub(r.Start).Minutes()); mins > 0 {
		span = say.Errorf("%s, %d min", span, mins)
	}
	if r.Going(now) {
		span = say.Errorf("%s · last change %s", span, ago(r.End, now))
	}
	if r.Changes == 0 {
		return say.Errorf("%s · picked up, nothing added yet", span)
	}
	done := say.Errorf("%s at %s", say.Errorf("%d changes", r.Changes), say.Errorf("%d companies", r.Companies))
	if r.People > 0 {
		done = say.Errorf("%s, %s", done, say.Errorf("%d people suggested", r.People))
	}
	if r.Drafts > 0 {
		done = say.Errorf("%s, %s", done, say.Errorf("%d drafts", r.Drafts))
	}
	return say.Errorf("%s · %s", span, done)
}

func ago(t, now time.Time) say.Sayer {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return say.New("just now")
	case d < time.Hour:
		return say.Errorf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return say.Errorf("%d hours ago", int(d.Hours()))
	}
	return onDate(t)
}
