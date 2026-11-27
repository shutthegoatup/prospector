package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/shutthegoatup/prospector/internal/say"
)

const (
	maxListed    = 300
	maxToolText  = 500
	maxListBytes = 4 << 20
)

// A function name every OpenAI-compatible endpoint accepts.
var toolName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func encodeList(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

// endpointError is an endpoint's refusal, kept apart so a run can wait out a limit.
type endpointError struct {
	Status     int
	Message    string
	RetryAfter time.Time
}

func (e *endpointError) Error() string {
	msg := fmt.Sprintf("the endpoint answered %d", e.Status)
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if !e.RetryAfter.IsZero() {
		msg += ", until " + e.RetryAfter.Format("15:04 MST")
	}
	return msg
}

// refusal reads an error reply in the shapes OpenAI-compatible endpoints use, whose
// code is sometimes a number and sometimes a string.
func refusal(res *http.Response, now time.Time) error {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	e := &endpointError{Status: res.StatusCode}
	var shaped struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &shaped) == nil && shaped.Error.Message != "" {
		e.Message = shaped.Error.Message
	}
	if r := []rune(strings.Join(strings.Fields(e.Message), " ")); len(r) > 200 {
		e.Message = string(r[:200]) + "…"
	} else {
		e.Message = string(r)
	}
	if after := res.Header.Get("Retry-After"); after != "" {
		if secs, err := strconv.Atoi(after); err == nil {
			e.RetryAfter = now.Add(time.Duration(secs) * time.Second)
		} else if at, err := http.ParseTime(after); err == nil {
			e.RetryAfter = at
		}
	}
	return e
}

// agentClient reaches what an organisation's owner typed in. Unless the operator
// allows private addresses, each connection is checked as it is made, and a
// redirect, being a second destination, isn't followed. Contexts bound each call.
func agentClient() *http.Client {
	return publicClient(os.Getenv("PROSPECTOR_AGENT_ALLOW_PRIVATE") == "1")
}

func publicClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !allowPrivate {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || !publicAddr(ap.Addr()) {
				return say.Errorf("%s isn't a public address", address)
			}
			return nil
		}
	}
	return &http.Client{
		Transport:     &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: 10 * time.Second, IdleConnTimeout: 90 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (a *App) agentHTTP() *http.Client {
	if a.agentClient != nil {
		return a.agentClient
	}
	return agentClient()
}

// endpointURL accepts an https address with nothing but a path.
func endpointURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", say.Errorf("%q isn't an address like https://llm.example.com/v1", raw)
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && os.Getenv("PROSPECTOR_AGENT_ALLOW_PRIVATE") == "1") { //nolint:staticcheck // plain http only when allowed
		return "", say.New("the address must start with https://")
	}
	return raw, nil
}

type bearer struct {
	key  string
	next http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	if b.key != "" {
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+b.key)
	}
	return b.next.RoundTrip(r)
}

// listModels asks the endpoint which models it offers (GET /models).
func (a *App) listModels(ctx context.Context, base, key string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := a.agentHTTP().Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach %s: %w", base, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, refusal(res, time.Now())
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxListBytes)).Decode(&list); err != nil {
		return nil, say.Errorf("%s/models didn't answer with a model list", base)
	}
	var out []string
	for _, m := range list.Data {
		if id := strings.TrimSpace(m.ID); id != "" && len(id) <= 200 && len(out) < maxListed {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// toolSession connects to an MCP server. The caller closes it.
func (a *App) toolSession(ctx context.Context, endpoint, key string) (*mcp.ClientSession, error) {
	client := a.agentHTTP()
	transport := &mcp.StreamableClientTransport{Endpoint: endpoint,
		HTTPClient: &http.Client{Transport: bearer{key, client.Transport}, CheckRedirect: client.CheckRedirect}}
	return mcp.NewClient(&mcp.Implementation{Name: "prospector", Version: "1"}, nil).Connect(ctx, transport, nil)
}

// listTools reads what the MCP server offers, keeping only tools a model can be
// given by name, each described briefly.
func (a *App) listTools(ctx context.Context, endpoint, key string) ([]AgentTool, error) {
	session, err := a.toolSession(ctx, endpoint, key)
	if err != nil {
		return nil, fmt.Errorf("couldn't connect to the tools at %s: %w", endpoint, err)
	}
	defer func() { _ = session.Close() }()
	var out []AgentTool
	for t, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("listing the tools at %s: %w", endpoint, err)
		}
		if !toolName.MatchString(t.Name) || len(out) >= maxListed {
			continue
		}
		desc := strings.Join(strings.Fields(t.Description), " ")
		if r := []rune(desc); len(r) > maxToolText {
			desc = string(r[:maxToolText-1]) + "…"
		}
		out = append(out, AgentTool{Name: t.Name, Description: desc})
	}
	slices.SortFunc(out, func(x, y AgentTool) int { return strings.Compare(x.Name, y.Name) })
	return out, nil
}

// AgentSettingsIn is what an owner saves: a new agent when ID is 0. An empty key
// keeps the one saved, but only for the same endpoint: a key isn't sent to an address
// it wasn't given for.
type AgentSettingsIn struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	BaseURL   string   `json:"base_url"`
	APIKey    string   `json:"api_key"`
	Model     string   `json:"model"`
	ToolsURL  string   `json:"tools_url"`
	SearchURL string   `json:"search_url"`
	Tools     []string `json:"tools"`
	// Fallback is the id of the agent that takes over, 0 for none; FallbackMinutes is
	// how long it waits first, 0 for the default.
	Fallback        int64 `json:"fallback"`
	FallbackMinutes int   `json:"fallback_minutes"`
	ThinkingOff     bool  `json:"thinking_off"`
}

const maxAgentName = 40

func agentName(raw string) (string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	if name == "" {
		return "", say.New("give the agent a name, such as the model it uses")
	}
	if len([]rune(name)) > maxAgentName {
		return "", say.Errorf("an agent's name is %d characters at most", maxAgentName)
	}
	return name, nil
}

// saveAgent stores an agent's settings, then checks them. A check that fails, as when
// the endpoint's daily limit is spent, is reported but doesn't stop the save.
func (a *App) saveAgent(ctx context.Context, org int64, in AgentSettingsIn, by string) (*Agent, error) {
	name, err := agentName(in.Name)
	if err != nil {
		return nil, err
	}
	c := &Agent{OrgID: org, Kind: kindOpenAI, Models: "[]", Tools: "[]", Allowed: "[]"}
	if in.ID != 0 {
		if c, err = a.s.AgentByID(org, in.ID); err != nil {
			return nil, err
		}
	}
	// A save that doesn't say keeps the agent's kind.
	kind := strings.TrimSpace(in.Kind)
	if kind == "" {
		kind = c.Kind
	}
	if !slices.ContainsFunc(AgentKinds, func(k struct{ Key, Label string }) bool { return k.Key == kind }) {
		return nil, say.Errorf("an agent is OpenAI-compatible or Anthropic, not %q", kind)
	}
	base, err := endpointURL(in.BaseURL)
	if err != nil {
		return nil, err
	}
	// Anthropic's own address is the SDK's; another is for a proxy in front of it.
	if base == "" && kind == kindOpenAI {
		return nil, say.New("enter the endpoint's address")
	}
	tools, err := endpointURL(in.ToolsURL)
	if err != nil {
		return nil, fmt.Errorf("tools: %w", err)
	}
	search, err := endpointURL(in.SearchURL)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	if taken, err := a.s.AgentNameTaken(org, name, c.ID); err != nil {
		return nil, err
	} else if taken {
		return nil, conflict{Text: say.Errorf("there is already an agent called %q", name)}
	}
	renamed := c.ID != 0 && c.Name != name
	c.Name = name
	key := strings.TrimSpace(in.APIKey)
	sameEndpoint := c.BaseURL == base && c.Kind == kind
	if key == "" && c.APIKey != "" && sameEndpoint {
		if key, err = a.s.Keyring.Open(c.APIKey); err != nil {
			return nil, fmt.Errorf("the saved key can't be read: enter it again (%w)", err)
		}
	}
	if key == "" {
		return nil, say.New("enter the endpoint's API key")
	}
	if len(key) > 4096 || strings.ContainsAny(key, " \r\n\t") {
		return nil, say.New("that doesn't look like an API key")
	}
	if a.s.Keyring == nil {
		return nil, say.New("this server has no secret key, so it can't keep an API key safely: ask whoever runs it to set PROSPECTOR_SECRET_KEYS")
	}
	sealedKey, err := a.s.Keyring.Seal(key)
	if err != nil {
		return nil, err
	}
	if !sameEndpoint {
		c.Models = "[]"
	}
	if c.ToolsURL != tools {
		c.Tools, c.Allowed = "[]", "[]"
	}
	fallback := in.Fallback
	if fallback != 0 {
		next, err := a.s.AgentByID(org, fallback)
		if err != nil {
			return nil, say.New("the fallback agent isn't one of this organisation's")
		}
		if next.ID == c.ID && c.ID != 0 {
			return nil, say.New("an agent can't be its own fallback")
		}
		// Agents falling back to each other would hand a task round without ever waiting.
		for id, seen := next.FallbackID, 0; id != 0 && c.ID != 0 && seen < 100; seen++ {
			if id == c.ID {
				return nil, say.Errorf("%s's fallbacks lead back to %s, which would hand a task round without waiting", next.Name, c.Name)
			}
			later, err := a.s.AgentByID(org, id)
			if err != nil {
				break
			}
			id = later.FallbackID
		}
	}
	switch {
	case in.FallbackMinutes == 0:
		in.FallbackMinutes = 15
	case in.FallbackMinutes < 1 || in.FallbackMinutes > 7*24*60:
		return nil, say.New("wait between a minute and a week before falling back")
	}
	c.FallbackID, c.FallbackMinutes = fallback, in.FallbackMinutes
	c.ThinkingOff = in.ThinkingOff && kind == kindOpenAI
	c.Kind, c.BaseURL, c.APIKey, c.ToolsURL, c.SearchURL, c.UpdatedBy = kind, base, sealedKey, tools, search, by
	c.Model = strings.TrimSpace(in.Model)
	if len(c.Model) > 200 {
		return nil, say.New("that model name is too long")
	}

	var problems []string
	check, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var models []string
	if kind == kindAnthropic {
		models, err = a.listClaudeModels(check, c, key)
	} else {
		models, err = a.listModels(check, base, key)
	}
	if err != nil {
		problems = append(problems, "Models: "+err.Error())
	} else {
		c.Models = encodeList(models)
		switch {
		case c.Model != "":
		case len(models) == 1:
			c.Model = models[0]
		case kind == kindAnthropic && slices.Contains(models, defaultClaude):
			c.Model = defaultClaude
		}
	}
	if search != "" {
		if _, err := a.webSearch(check, search, "prospector", 0); err != nil {
			problems = append(problems, "Search: "+err.Error())
		}
	}
	if tools != "" {
		if found, err := a.listTools(check, tools, c.ToolsKey(key)); err != nil {
			problems = append(problems, "Tools: "+err.Error())
		} else {
			c.Tools = encodeList(found)
		}
	}
	// Only tools on offer can be allowed; one that appears later starts unticked.
	var allowed []string
	for _, t := range c.ToolList() {
		if slices.Contains(in.Tools, t.Name) {
			allowed = append(allowed, t.Name)
		}
	}
	if in.Tools == nil {
		allowed = slices.DeleteFunc(c.AllowedTools(), func(name string) bool {
			return !slices.ContainsFunc(c.ToolList(), func(t AgentTool) bool { return t.Name == name })
		})
	}
	c.Allowed = encodeList(allowed)
	now := time.Now()
	c.Problem, c.CheckedAt = strings.Join(problems, " "), &now
	if err := a.s.SaveAgentRow(org, c); err != nil {
		return nil, err
	}
	if renamed && c.KeyID != 0 {
		// Its key carries its name into the record; MCP servers built for it hold the old one.
		if err := a.s.RenameKey(org, c.KeyID, builtinKeyName(c.Name)); err != nil {
			return nil, err
		}
		a.mu.Lock()
		a.mcp = nil
		a.mu.Unlock()
	}
	return c, nil
}

// deleteAgent stops what the agent is doing, revokes its key so nothing more is done
// in its name, and forgets its settings. What it found stays, as any agent's does.
func (a *App) deleteAgent(org, id int64) error {
	c, err := a.s.AgentByID(org, id)
	if err != nil {
		return err
	}
	going, err := a.s.AgentRunsGoing(org, c.ID)
	if err != nil {
		return err
	}
	for _, run := range going {
		if err := a.StopRun(org, run.ID); err != nil {
			log.Printf("agent %d: run %d not asked to stop: %v", c.ID, run.ID, err)
		}
	}
	return a.s.DeleteAgent(org, c.ID)
}

// AgentView is what the Agents page shows of an agent: never the key.
type AgentView struct {
	Agent
	HasKey bool
	// Others are the agents it could fall back to.
	Others []Agent
}

// agentView is c as its card shows it, with those of all it could fall back to.
func agentView(c Agent, all []Agent) AgentView {
	view := AgentView{Agent: c, HasKey: c.APIKey != ""}
	view.APIKey = ""
	for _, o := range all {
		if o.ID != c.ID {
			view.Others = append(view.Others, Agent{ID: o.ID, Name: o.Name})
		}
	}
	return view
}

// AgentState is how an agent's card looks: ok, warn or stop, a line saying so, and
// what to do about it.
type AgentState struct{ Rail, Line, Todo string }

func (v AgentView) State() AgentState {
	checked := ""
	if v.CheckedAt != nil {
		checked = " · checked " + v.CheckedAt.Format("2 Jan 15:04")
	}
	switch {
	case !v.HasKey:
		return AgentState{"stop", "✕ Can't run", "It needs an API key."}
	case v.Kind == kindOpenAI && v.BaseURL == "":
		return AgentState{"stop", "✕ Can't run", "It needs the endpoint's address."}
	case v.Model == "":
		return AgentState{"stop", "✕ Can't run" + checked, "Pick one of the models the endpoint lists."}
	case v.Problem != "":
		return AgentState{"warn", "● Its last check found a problem" + checked, "Last check: " + v.Problem}
	case !v.Searches():
		return AgentState{"warn", "● Runs, but can't search the web" + checked,
			"Add a search address or allow a search tool; until then it only reads pages it's given."}
	}
	return AgentState{"ok", "✓ Ready" + checked, ""}
}

// FallbackText says who takes its task over, and when.
func (v AgentView) FallbackText() string {
	for _, o := range v.Others {
		if o.ID == v.FallbackID {
			return o.Name + ", after " + plural(agentFallbackMinutes(v), "minute")
		}
	}
	return "nobody: it waits"
}

func (v AgentView) ToolsText() string {
	switch offered := len(v.ToolList()); {
	case v.ToolsURL == "":
		return "built-in only"
	case offered == 0:
		return "none listed"
	default:
		return fmt.Sprintf("%d of %d allowed", len(v.AllowedTools()), offered)
	}
}

func (v AgentView) SearchText() string {
	switch {
	case v.SearchURL != "":
		return "✓ SearXNG"
	case v.Searches():
		return "✓ through its tools"
	}
	return "none"
}

func agentFallbackMinutes(v AgentView) int {
	if v.FallbackMinutes == 0 {
		return 15
	}
	return v.FallbackMinutes
}
