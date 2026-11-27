package store

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"time"
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

func DecodeList[T any](raw string) []T {
	var out []T
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func (c Agent) ModelList() []string { return DecodeList[string](c.Models) }

func (c Agent) ToolList() []AgentTool { return DecodeList[AgentTool](c.Tools) }

func (c Agent) AllowedTools() []string { return DecodeList[string](c.Allowed) }

func (c Agent) Ready() bool {
	return (c.BaseURL != "" || c.Kind == KindAnthropic) && c.APIKey != "" && c.Model != ""
}

func (c Agent) Allows(tool string) bool { return slices.Contains(c.AllowedTools(), tool) }

func SameOrigin(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	return errA == nil && errB == nil && ua.Scheme == ub.Scheme && ua.Host == ub.Host
}

// ToolsKey is the key the tools URL may be sent: the endpoint's, on the same origin only.
func (c Agent) ToolsKey(key string) string {
	// An Anthropic key is for Anthropic, and no tool server is.
	if c.Kind == KindOpenAI && SameOrigin(c.BaseURL, c.ToolsURL) {
		return key
	}
	return ""
}

// Searches is whether it can look things up on the web, through SearXNG or a tool.
func (c Agent) Searches() bool {
	return c.SearchURL != "" || slices.ContainsFunc(c.AllowedTools(), func(t string) bool { return strings.Contains(t, "search") })
}

const KindOpenAI = "openai"

const KindAnthropic = "anthropic"
