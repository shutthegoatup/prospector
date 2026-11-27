package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// An agent reaches its model one of two ways: any OpenAI-compatible endpoint, or
// Anthropic's own API through its SDK.
const (
	kindOpenAI    = "openai"
	kindAnthropic = "anthropic"
)

var AgentKinds = []struct{ Key, Label string }{
	{kindOpenAI, "OpenAI-compatible"},
	{kindAnthropic, "Anthropic (Claude)"},
}

// defaultClaude is the model an Anthropic agent is offered first.
const defaultClaude = "claude-opus-5-5"

// claudeMaxTokens is a turn's ceiling: room for an agent's reasoning and its tool
// calls, short of what needs a streamed answer to arrive in time.
const claudeMaxTokens = 16000

// claudeFallbacks are the current models: they think adaptively, and their refusals
// Anthropic can serve from another model in the same call, both of which it recommends.
var claudeFallbacks = []string{"claude-fable-5-2", "claude-fable-5-1", "claude-opus-5-5", "claude-opus-5", "claude-sonnet-5-5"}

// claudeClient talks to Anthropic as the agent, through the client that refuses this
// machine's network. It uses the agent's key and nothing else: the server's own
// environment, profiles and base URL stay out of it, so an operator's credentials can
// never go to an address an owner typed. Prospector's loop does its own waiting, so the
// SDK doesn't retry.
func (a *App) claudeClient(c *Agent, key string) anthropic.Client {
	opts := []option.RequestOption{option.WithoutEnvironmentDefaults(), option.WithAPIKey(key), option.WithHTTPClient(a.agentHTTP()),
		option.WithMaxRetries(0), option.WithRequestTimeout(completionLimit)}
	if c.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(c.BaseURL))
	}
	return anthropic.NewClient(opts...)
}

// claudeErr gives an SDK failure the shape the run waits on or ends with.
func claudeErr(err error) error {
	var refused *anthropic.Error
	if errors.As(err, &refused) {
		e := &endpointError{Status: refused.StatusCode, Message: strings.Join(strings.Fields(refused.Message), " ")}
		if r := []rune(e.Message); len(r) > 200 {
			e.Message = string(r[:200]) + "…"
		}
		if refused.Response != nil {
			if after := refused.Response.Header.Get("Retry-After"); after != "" {
				if secs, err := strconv.Atoi(after); err == nil {
					e.RetryAfter = time.Now().Add(time.Duration(secs) * time.Second)
				} else if at, err := http.ParseTime(after); err == nil {
					e.RetryAfter = at
				}
			}
		}
		return e
	}
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrUnexpectedEOF) {
		return &unreachable{err}
	}
	return err
}

func (a *App) listClaudeModels(ctx context.Context, c *Agent, key string) ([]string, error) {
	client := a.claudeClient(c, key)
	pages := client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	var out []string
	for pages.Next() && len(out) < maxListed {
		out = append(out, pages.Current().ID)
	}
	if err := pages.Err(); err != nil {
		return nil, claudeErr(err)
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// claudeTools are the run's tools as Anthropic takes them, from the same schemas.
func claudeTools(defs []toolDef) []anthropic.BetaToolUnionParam {
	out := make([]anthropic.BetaToolUnionParam, 0, len(defs))
	for _, d := range defs {
		var schema map[string]any
		_ = json.Unmarshal(d.Function.Parameters, &schema)
		input := anthropic.BetaToolInputSchemaParam{Properties: schema["properties"], ExtraFields: map[string]any{}}
		if req, ok := schema["required"].([]any); ok {
			for _, r := range req {
				if s, ok := r.(string); ok {
					input.Required = append(input.Required, s)
				}
			}
		}
		for k, v := range schema {
			if k != "type" && k != "properties" && k != "required" {
				input.ExtraFields[k] = v
			}
		}
		tool := anthropic.BetaToolParam{Name: d.Function.Name, Description: anthropic.String(d.Function.Description), InputSchema: input}
		out = append(out, anthropic.BetaToolUnionParam{OfTool: &tool})
	}
	return out
}

// claudeChat is an episode with Claude. Its history only ever grows, as Claude's
// thinking needs: each answer is kept exactly as it came.
type claudeChat struct {
	client  anthropic.Client
	params  anthropic.BetaMessageNewParams
	pending []anthropic.BetaContentBlockParamUnion
}

func (a *App) newClaudeChat(c *Agent, key string, tools []toolDef, system, opening string) *claudeChat {
	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(c.Model),
		MaxTokens: claudeMaxTokens,
		System:    []anthropic.BetaTextBlockParam{{Text: system}},
		Tools:     claudeTools(tools),
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(opening))},
		// The conversation so far is cached, so each turn pays full price only for what is new.
		CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
	}
	if slices.Contains(claudeFallbacks, c.Model) {
		params.Thinking = anthropic.BetaThinkingConfigParamUnion{OfAdaptive: &anthropic.BetaThinkingConfigAdaptiveParam{}}
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
		params.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
	}
	return &claudeChat{client: a.claudeClient(c, key), params: params}
}

func (c *claudeChat) next(ctx context.Context) (*modelTurn, error) {
	if len(c.pending) > 0 {
		c.params.Messages = append(c.params.Messages, anthropic.NewBetaUserMessage(c.pending...))
		c.pending = nil
	}
	resp, err := c.client.Beta.Messages.New(ctx, c.params)
	if err != nil {
		return nil, claudeErr(err)
	}
	said := resp.ToParam()
	// An empty answer can only stand last, and a nudge follows it.
	if len(resp.Content) == 0 && said.OfAssistant != nil {
		said.OfAssistant.Content = []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock("(no reply)")}
	}
	c.params.Messages = append(c.params.Messages, said)
	if resp.StopReason == anthropic.BetaStopReasonRefusal {
		return nil, fmt.Errorf("%s declined to go on with this task", c.params.Model)
	}
	turn := &modelTurn{tokensIn: resp.Usage.InputTokens + resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens,
		tokensOut: resp.Usage.OutputTokens}
	for _, block := range resp.Content {
		if use, ok := block.AsAny().(anthropic.BetaToolUseBlock); ok {
			turn.calls = append(turn.calls, modelCall{id: use.ID, name: use.Name, args: use.JSON.Input.Raw()})
		}
	}
	return turn, nil
}

// answer and say queue what goes back in the next user message: every tool result of
// a turn together, as Claude expects.
func (c *claudeChat) answer(results []toolResult) {
	for _, r := range results {
		c.pending = append(c.pending, anthropic.NewBetaToolResultBlock(r.id, r.text, r.failed))
	}
}

func (c *claudeChat) say(text string) {
	c.pending = append(c.pending, anthropic.NewBetaTextBlock(text))
}
