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
)

// AgentConfig is how an organisation's own agent reaches a model and its tools:
// an OpenAI-compatible endpoint, and an MCP server whose tools it may use.
type AgentConfig struct {
	OrgID   int64  `gorm:"primaryKey;autoIncrement:false"`
	BaseURL string `gorm:"not null;default:''"`
	// APIKey is sealed under the server's key, never kept or shown in clear.
	APIKey   string `gorm:"not null;default:''"`
	Model    string `gorm:"not null;default:''"`
	ToolsURL string `gorm:"not null;default:''"`
	// Models and Tools are what the endpoints offered when last checked, as JSON, so
	// Settings never has to call them to render. Allowed is the tools ticked.
	Models    string `gorm:"not null;default:'[]'"`
	Tools     string `gorm:"not null;default:'[]'"`
	Allowed   string `gorm:"not null;default:'[]'"`
	Problem   string `gorm:"not null;default:''"`
	CheckedAt *time.Time
	UpdatedBy string `gorm:"not null;default:''"`
	UpdatedAt time.Time
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

func (c AgentConfig) ModelList() []string     { return decodeList[string](c.Models) }
func (c AgentConfig) ToolList() []AgentTool   { return decodeList[AgentTool](c.Tools) }
func (c AgentConfig) AllowedTools() []string  { return decodeList[string](c.Allowed) }
func (c AgentConfig) Ready() bool             { return c.BaseURL != "" && c.APIKey != "" && c.Model != "" }
func (c AgentConfig) Allows(tool string) bool { return slices.Contains(c.AllowedTools(), tool) }

func (s *Store) AgentConfigOf(org int64) (*AgentConfig, error) {
	c := AgentConfig{OrgID: org, Models: "[]", Tools: "[]", Allowed: "[]"}
	err := s.db.Where("org_id = ?", org).Limit(1).Find(&c).Error
	return &c, err
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
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if os.Getenv("PROSPECTOR_AGENT_ALLOW_PRIVATE") != "1" {
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

// AgentSettingsIn is what an owner saves. An empty key keeps the one saved, but
// only for the same endpoint: a key isn't sent to an address it wasn't given for.
type AgentSettingsIn struct {
	BaseURL  string   `json:"base_url"`
	APIKey   string   `json:"api_key"`
	Model    string   `json:"model"`
	ToolsURL string   `json:"tools_url"`
	Tools    []string `json:"tools"`
}

// toolsKey is the key the tools URL may be sent: the endpoint's, on the same origin only.
func (c AgentConfig) toolsKey(key string) string {
	if sameOrigin(c.BaseURL, c.ToolsURL) {
		return key
	}
	return ""
}

// saveAgentConfig stores the settings, then checks them. A check that fails, as when
// the endpoint's daily limit is spent, is reported but doesn't stop the save.
func (a *App) saveAgentConfig(ctx context.Context, org int64, in AgentSettingsIn, by string) (*AgentConfig, error) {
	base, err := endpointURL(in.BaseURL)
	if err != nil {
		return nil, err
	}
	tools, err := endpointURL(in.ToolsURL)
	if err != nil {
		return nil, fmt.Errorf("tools: %w", err)
	}
	c, err := a.s.AgentConfigOf(org)
	if err != nil {
		return nil, err
	}
	if base == "" {
		*c = AgentConfig{OrgID: org, Models: "[]", Tools: "[]", Allowed: "[]", UpdatedBy: by}
		return c, a.s.db.Save(c).Error
	}
	key := strings.TrimSpace(in.APIKey)
	if key == "" && c.APIKey != "" && c.BaseURL == base {
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
	if c.BaseURL != base {
		c.Models = "[]"
	}
	if c.ToolsURL != tools {
		c.Tools, c.Allowed = "[]", "[]"
	}
	c.BaseURL, c.APIKey, c.ToolsURL, c.UpdatedBy = base, sealedKey, tools, by
	c.Model = strings.TrimSpace(in.Model)
	if len(c.Model) > 200 {
		return nil, errors.New("that model name is too long")
	}

	var problems []string
	check, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if models, err := a.listModels(check, base, key); err != nil {
		problems = append(problems, "Models: "+err.Error())
	} else {
		c.Models = encodeList(models)
		if c.Model == "" && len(models) == 1 {
			c.Model = models[0]
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
	return c, a.s.db.Save(c).Error
}

func (a *App) uiAgent(w http.ResponseWriter, r *http.Request) {
	var in AgentSettingsIn
	if !decode(w, r, &in) {
		return
	}
	q := reqOf(r)
	c, err := a.saveAgentConfig(r.Context(), q.Org.ID, in, q.Actor())
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	msg := "Saved."
	switch {
	case c.BaseURL == "":
		msg = "Prospector's agent is off: no model endpoint."
	case c.Problem != "":
		msg = "Saved, but " + c.Problem
	case !c.Ready():
		msg = "Saved. Pick a model to finish."
	}
	writeJSON(w, map[string]string{"message": msg})
}

// AgentView is what Settings shows of the agent: never the key.
type AgentView struct {
	AgentConfig
	HasKey bool
	// Sealing is whether the server can keep a key at all.
	Sealing bool
}

func (a *App) agentView(org int64) AgentView {
	c, _ := a.s.AgentConfigOf(org)
	v := AgentView{AgentConfig: *c, HasKey: c.APIKey != "", Sealing: a.s.keys != nil}
	v.APIKey = ""
	return v
}
