package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"gorm.io/gorm"
)

// Agent is one of an organisation's own agents: what it is called, how it reaches a
// model (an OpenAI-compatible endpoint), and an MCP server whose tools it may use.
type Agent struct {
	ID    int64  `gorm:"primaryKey"`
	OrgID int64  `gorm:"uniqueIndex:idx_agents_org_name;not null"`
	Name  string `gorm:"uniqueIndex:idx_agents_org_name;not null"`
	Kind  string `gorm:"not null;default:'openai'"`
	// KeyID is its built-in API key, which everything it does is recorded under.
	KeyID   int64  `gorm:"not null;default:0"`
	BaseURL string `gorm:"not null;default:''"`
	// APIKey is sealed under the server's key, never kept or shown in clear.
	APIKey   string `gorm:"not null;default:''"`
	Model    string `gorm:"not null;default:''"`
	ToolsURL string `gorm:"not null;default:''"`
	// SearchURL is a SearXNG instance its web_search asks; empty, it has no search of its own.
	SearchURL string `gorm:"not null;default:''"`
	// Models and Tools are what the endpoints offered when last checked, as JSON, so
	// Settings never has to call them to render. Allowed is the tools ticked.
	Models  string `gorm:"not null;default:'[]'"`
	Tools   string `gorm:"not null;default:'[]'"`
	Allowed string `gorm:"not null;default:'[]'"`
	// FallbackID is the agent that takes the task over once this one's endpoint has kept
	// it waiting FallbackMinutes; 0 is none, so it waits however long.
	FallbackID      int64 `gorm:"not null;default:0"`
	FallbackMinutes int   `gorm:"not null;default:15"`
	// ThinkingOff asks an OpenAI-compatible endpoint's model not to think first: a local
	// thinking model is too slow to think before every tool call.
	ThinkingOff bool   `gorm:"not null;default:false"`
	Problem     string `gorm:"not null;default:''"`
	CheckedAt   *time.Time
	UpdatedBy   string `gorm:"not null;default:''"`
	UpdatedAt   time.Time
}

type AgentTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

const (
	maxListed    = 300
	maxToolText  = 500
	maxListBytes = 4 << 20
)

// A function name every OpenAI-compatible endpoint accepts.
var toolName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func decodeList[T any](raw string) []T {
	var out []T
	json.Unmarshal([]byte(raw), &out)
	return out
}

func encodeList(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func (c Agent) ModelList() []string    { return decodeList[string](c.Models) }
func (c Agent) ToolList() []AgentTool  { return decodeList[AgentTool](c.Tools) }
func (c Agent) AllowedTools() []string { return decodeList[string](c.Allowed) }
func (c Agent) Ready() bool {
	return (c.BaseURL != "" || c.Kind == kindAnthropic) && c.APIKey != "" && c.Model != ""
}
func (c Agent) Allows(tool string) bool { return slices.Contains(c.AllowedTools(), tool) }

// Agents are an organisation's own agents, by name.
func (s *Store) Agents(org int64) ([]Agent, error) {
	var out []Agent
	return out, s.db.Where("org_id = ?", org).Order("name, id").Find(&out).Error
}

func (s *Store) AgentByID(org, id int64) (*Agent, error) {
	var c Agent
	if err := s.db.Where("org_id = ? and id = ?", org, id).Limit(1).Find(&c).Error; err != nil || c.ID == 0 {
		return nil, missing(err, "no agent %d in this organisation", id)
	}
	return &c, nil
}

func (s *Store) agentNamed(org int64, name string) (*Agent, error) {
	var c Agent
	if err := s.db.Where("org_id = ? and name = ?", org, name).Limit(1).Find(&c).Error; err != nil || c.ID == 0 {
		return nil, errors.Join(err, fmt.Errorf("no agent called %q in this organisation", name))
	}
	return &c, nil
}

// migrateAgentConfigs moves each organisation's single agent from before there could
// be several into the agents table, as one called "Prospector" with the built-in key
// its work is already recorded under, and points its runs at it. The old row is
// deleted in the same transaction, so an agent its owner later deletes doesn't return.
func migrateAgentConfigs(db *gorm.DB) error {
	if !db.Migrator().HasTable("agent_configs") {
		return nil
	}
	var old []struct {
		OrgID                                           int64
		BaseURL, APIKey, Model, ToolsURL, Models, Tools string
		Allowed, Problem, UpdatedBy                     string
		CheckedAt                                       *time.Time
		UpdatedAt                                       time.Time
	}
	if err := db.Table("agent_configs").Find(&old).Error; err != nil {
		return err
	}
	for _, c := range old {
		err := db.Transaction(func(tx *gorm.DB) error {
			var key APIKey
			// Only a key no agent holds: one held is another agent's record, renamed or
			// added since the move. Without one, its first run makes it a key of its own.
			if err := tx.Where("org_id = ? and prefix = ? and revoked_at is null and id not in (?)", c.OrgID, builtinPrefix,
				tx.Model(&Agent{}).Select("key_id").Where("org_id = ?", c.OrgID)).Order("id").Limit(1).Find(&key).Error; err != nil {
				return err
			}
			if c.BaseURL != "" {
				settings := map[string]any{"base_url": c.BaseURL, "api_key": c.APIKey, "model": c.Model, "tools_url": c.ToolsURL,
					"models": c.Models, "tools": c.Tools, "allowed": c.Allowed, "problem": c.Problem, "checked_at": c.CheckedAt,
					"updated_by": c.UpdatedBy, "updated_at": c.UpdatedAt}
				// Moved before, then entered again on an earlier release: the newer
				// settings are the ones in this row.
				var a Agent
				if err := tx.Where("org_id = ? and name = ?", c.OrgID, "Prospector").Limit(1).Find(&a).Error; err != nil {
					return err
				}
				if a.ID != 0 {
					if err := tx.Model(&Agent{}).Where("id = ?", a.ID).Updates(settings).Error; err != nil {
						return err
					}
				} else {
					a = Agent{OrgID: c.OrgID, Name: "Prospector", Kind: "openai", KeyID: key.ID, BaseURL: c.BaseURL, APIKey: c.APIKey, Model: c.Model,
						ToolsURL: c.ToolsURL, Models: c.Models, Tools: c.Tools, Allowed: c.Allowed, Problem: c.Problem, CheckedAt: c.CheckedAt,
						UpdatedBy: c.UpdatedBy, UpdatedAt: c.UpdatedAt}
					if err := tx.Create(&a).Error; err != nil {
						return err
					}
				}
				if key.ID != 0 && key.ID == a.KeyID {
					if err := tx.Model(&APIKey{}).Where("id = ?", key.ID).Update("name", builtinKeyName(a.Name)).Error; err != nil {
						return err
					}
				}
				if err := tx.Model(&AgentRun{}).Where("org_id = ? and agent_id = 0", c.OrgID).
					Updates(map[string]any{"agent_id": a.ID, "agent_name": a.Name}).Error; err != nil {
					return err
				}
			}
			return tx.Exec("delete from agent_configs where org_id = ?", c.OrgID).Error
		})
		if err != nil {
			return fmt.Errorf("moving organisation %d's agent: %w", c.OrgID, err)
		}
	}
	return nil
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
				return fmt.Errorf("%s isn't a public address", address)
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
		return "", fmt.Errorf("%q isn't an address like https://llm.example.com/v1", raw)
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && os.Getenv("PROSPECTOR_AGENT_ALLOW_PRIVATE") == "1") {
		return "", errors.New("the address must start with https://")
	}
	return raw, nil
}

func sameOrigin(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	return errA == nil && errB == nil && ua.Scheme == ub.Scheme && ua.Host == ub.Host
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
		return nil, fmt.Errorf("%s/models didn't answer with a model list", base)
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
	defer session.Close()
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

// toolsKey is the key the tools URL may be sent: the endpoint's, on the same origin only.
func (c Agent) toolsKey(key string) string {
	// An Anthropic key is for Anthropic, and no tool server is.
	if c.Kind == kindOpenAI && sameOrigin(c.BaseURL, c.ToolsURL) {
		return key
	}
	return ""
}

const maxAgentName = 40

func agentName(raw string) (string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	if name == "" {
		return "", errors.New("give the agent a name, such as the model it uses")
	}
	if len([]rune(name)) > maxAgentName {
		return "", fmt.Errorf("an agent's name is %d characters at most", maxAgentName)
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
		return nil, fmt.Errorf("an agent is OpenAI-compatible or Anthropic, not %q", kind)
	}
	base, err := endpointURL(in.BaseURL)
	if err != nil {
		return nil, err
	}
	// Anthropic's own address is the SDK's; another is for a proxy in front of it.
	if base == "" && kind == kindOpenAI {
		return nil, errors.New("enter the endpoint's address")
	}
	tools, err := endpointURL(in.ToolsURL)
	if err != nil {
		return nil, fmt.Errorf("tools: %w", err)
	}
	search, err := endpointURL(in.SearchURL)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	var taken int64
	if err := a.s.db.Model(&Agent{}).Where("org_id = ? and name = ? and id <> ?", org, name, c.ID).Count(&taken).Error; err != nil {
		return nil, err
	}
	if taken > 0 {
		return nil, conflict(fmt.Sprintf("there is already an agent called %q", name))
	}
	renamed := c.ID != 0 && c.Name != name
	c.Name = name
	key := strings.TrimSpace(in.APIKey)
	sameEndpoint := c.BaseURL == base && c.Kind == kind
	if key == "" && c.APIKey != "" && sameEndpoint {
		if key, err = a.s.keys.open(c.APIKey); err != nil {
			return nil, fmt.Errorf("the saved key can't be read: enter it again (%w)", err)
		}
	}
	if key == "" {
		return nil, errors.New("enter the endpoint's API key")
	}
	if len(key) > 4096 || strings.ContainsAny(key, " \r\n\t") {
		return nil, errors.New("that doesn't look like an API key")
	}
	if a.s.keys == nil {
		return nil, errors.New("this server has no secret key, so it can't keep an API key safely: ask whoever runs it to set PROSPECTOR_SECRET_KEYS")
	}
	sealedKey, err := a.s.keys.seal(key)
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
			return nil, errors.New("the fallback agent isn't one of this organisation's")
		}
		if next.ID == c.ID && c.ID != 0 {
			return nil, errors.New("an agent can't be its own fallback")
		}
		// Agents falling back to each other would hand a task round without ever waiting.
		for id, seen := next.FallbackID, 0; id != 0 && c.ID != 0 && seen < 100; seen++ {
			if id == c.ID {
				return nil, fmt.Errorf("%s's fallbacks lead back to %s, which would hand a task round without waiting", next.Name, c.Name)
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
		return nil, errors.New("wait between a minute and a week before falling back")
	}
	c.FallbackID, c.FallbackMinutes = fallback, in.FallbackMinutes
	c.ThinkingOff = in.ThinkingOff && kind == kindOpenAI
	c.Kind, c.BaseURL, c.APIKey, c.ToolsURL, c.SearchURL, c.UpdatedBy = kind, base, sealedKey, tools, search, by
	c.Model = strings.TrimSpace(in.Model)
	if len(c.Model) > 200 {
		return nil, errors.New("that model name is too long")
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
		if found, err := a.listTools(check, tools, c.toolsKey(key)); err != nil {
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
	if err := a.s.writeAgent(c); err != nil {
		return nil, err
	}
	if renamed && c.KeyID != 0 {
		// Its key carries its name into the record; MCP servers built for it hold the old one.
		if err := a.s.db.Model(&APIKey{}).Where("id = ?", c.KeyID).Update("name", builtinKeyName(c.Name)).Error; err != nil {
			return nil, err
		}
		a.mu.Lock()
		a.mcp = nil
		a.mu.Unlock()
	}
	return c, nil
}

// writeAgent stores an agent's settings: a new one is made; an existing one has only
// what an owner edits written, and only while it is still there. Checking an endpoint
// takes seconds, and an agent deleted meanwhile mustn't come back, nor a key its first
// run made meanwhile be lost.
func (s *Store) writeAgent(c *Agent) error {
	if c.ID == 0 {
		return s.db.Create(c).Error
	}
	res := s.db.Model(&Agent{}).Where("id = ? and org_id = ?", c.ID, c.OrgID).
		Select("name", "kind", "base_url", "fallback_id", "fallback_minutes", "thinking_off", "api_key", "model", "tools_url", "search_url", "models", "tools", "allowed", "problem", "checked_at", "updated_by", "updated_at").
		Updates(c)
	if res.Error == nil && res.RowsAffected == 0 {
		return fmt.Errorf("%s was deleted meanwhile", c.Name)
	}
	return res.Error
}

// deleteAgent stops what the agent is doing, revokes its key so nothing more is done
// in its name, and forgets its settings. What it found stays, as any agent's does.
func (a *App) deleteAgent(org, id int64) error {
	c, err := a.s.AgentByID(org, id)
	if err != nil {
		return err
	}
	var going []AgentRun
	if err := a.s.db.Where("org_id = ? and agent_id = ? and status in ?", org, c.ID, []string{RunWorking, RunWaiting}).Find(&going).Error; err != nil {
		return err
	}
	for _, run := range going {
		a.StopRun(org, run.ID)
	}
	now := time.Now()
	return a.s.db.Transaction(func(tx *gorm.DB) error {
		if c.KeyID != 0 {
			if err := tx.Model(&APIKey{}).Where("id = ? and revoked_at is null", c.KeyID).Update("revoked_at", &now).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&Agent{}).Where("org_id = ? and fallback_id = ?", org, c.ID).Update("fallback_id", 0).Error; err != nil {
			return err
		}
		return tx.Delete(c).Error
	})
}

// AgentView is what the Agents page shows of an agent: never the key.
type AgentView struct {
	Agent
	HasKey bool
	// Others are the agents it could fall back to.
	Others []Agent
}

type OwnAgentsView struct {
	Agents []AgentView
	// Sealing is whether the server can keep a key at all.
	Sealing bool
	// New is the form for adding one.
	New AgentView
}

func (a *App) ownAgents(org int64) OwnAgentsView {
	list, _ := a.s.Agents(org)
	v := OwnAgentsView{Sealing: a.s.keys != nil}
	for _, c := range list {
		v.Agents = append(v.Agents, agentView(c, list))
	}
	for _, c := range list {
		v.New.Others = append(v.New.Others, Agent{ID: c.ID, Name: c.Name})
	}
	return v
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

// Searches is whether it can look things up on the web, through SearXNG or a tool.
func (c Agent) Searches() bool {
	return c.SearchURL != "" || slices.ContainsFunc(c.AllowedTools(), func(t string) bool { return strings.Contains(t, "search") })
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

// Tally counts the agents in each state, for the line above them.
func (v OwnAgentsView) Tally() (ok, warn, stop int) {
	for _, a := range v.Agents {
		switch a.State().Rail {
		case "ok":
			ok++
		case "warn":
			warn++
		default:
			stop++
		}
	}
	return ok, warn, stop
}
