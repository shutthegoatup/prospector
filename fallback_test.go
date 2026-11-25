package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestAWaitingAgentHandsItsTaskToItsFallback(t *testing.T) {
	r, claude, gateway := claudeSetup(t)
	ctx := context.Background()
	claudeID := r.agent
	first := must(r.s.agentNamed(r.o.ID, "Prospector"))
	in := AgentSettingsIn{ID: first.ID, Name: first.Name, BaseURL: first.BaseURL, Model: first.Model, ToolsURL: first.ToolsURL,
		Tools: first.AllowedTools(), Fallback: claudeID}
	if got := must(r.a.saveAgent(ctx, r.o.ID, in, "Ada Lovelace")); got.FallbackID != claudeID || got.FallbackMinutes != 15 {
		t.Fatalf("a fallback should be kept, waiting 15 minutes unless told: %+v", got)
	}
	if v := r.a.ownAgents(r.o.ID); len(v.Agents) != 2 || len(v.Agents[0].Others) != 1 || len(v.New.Others) != 2 {
		t.Fatalf("each form should offer the other agents as fallbacks: %+v", v)
	}

	// The gateway's day is spent, an hour off; Claude is free.
	gateway.chat = func(int, map[string]any) (int, string) {
		return http.StatusTooManyRequests, `{"error":{"message":"daily limit"}}`
	}
	claude.reply = func(int) (int, string) {
		return 200, claudeTurn("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	// A full server still lets a run hand over: its own place goes to the fallback.
	was := maxRunning
	maxRunning = 1
	t.Cleanup(func() { maxRunning = was })
	run := r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, first.ID, "Ada Lovelace")).ID)
	if run.Status != RunStopped || !strings.Contains(run.StopReason, "handed the task to Claude") || len(r.sleeps) != 0 {
		t.Fatalf("a run facing an hour's wait should hand over at once: %+v, waited %v", run, r.sleeps)
	}
	var next AgentRun
	if err := r.s.db.Where("task_id = ? and agent_id = ?", r.task.ID, claudeID).First(&next).Error; err != nil {
		t.Fatalf("the fallback should have been started: %v", err)
	}
	if next = r.finished(t, next.ID); next.Status != RunFinished || next.StopReason != "the request is done" || next.StartedBy != "Prospector (fallback)" {
		t.Fatalf("the fallback's run: %+v", next)
	}

	maxRunning = was
	export := must(r.s.ExportOrg(r.o.ID, "Ada Lovelace"))
	for _, e := range export.Agents {
		if e.Name == "Prospector" && (e.FallsBackTo != "Claude" || e.FallbackMinutes != 15) {
			t.Fatalf("an export should say which agent takes over: %+v", e)
		}
	}

	// An hour's wait inside a two-hour allowance is waited out.
	if err := r.s.SetRequestDone(r.o.ID, r.brief, false, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	in.FallbackMinutes = 120
	must(r.a.saveAgent(ctx, r.o.ID, in, "Ada Lovelace"))
	gateway.chat = func(n int, _ map[string]any) (int, string) {
		if n == 0 {
			return http.StatusTooManyRequests, `{"error":{"message":"daily limit"}}`
		}
		return 200, chatTools("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	gateway.mu.Lock()
	gateway.chats = 0
	gateway.mu.Unlock()
	r.sleeps = nil
	if run := r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, first.ID, "Ada Lovelace")).ID); run.Status != RunFinished || len(r.sleeps) != 1 {
		t.Fatalf("a wait inside the allowance should be waited out: %+v, waited %v", run, r.sleeps)
	}

	if err := r.a.deleteAgent(r.o.ID, claudeID); err != nil {
		t.Fatal(err)
	}
	if got := must(r.s.AgentByID(r.o.ID, first.ID)); got.FallbackID != 0 {
		t.Fatalf("deleting the fallback should leave the agent without one: %+v", got)
	}
}

func TestExistingAgentsSurviveTheFallbackColumns(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	if err := s.db.Create(&Agent{OrgID: o.ID, Name: "Gateway", Kind: kindOpenAI, Models: "[]", Tools: "[]", Allowed: "[]"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, col := range []string{"fallback_id", "fallback_minutes"} {
		if err := s.db.Migrator().DropColumn(&Agent{}, col); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.db.AutoMigrate(models...); err != nil {
		t.Fatalf("adding the fallback to a table with rows: %v", err)
	}
	got := must(s.agentNamed(o.ID, "Gateway"))
	if got.FallbackID != 0 || got.FallbackMinutes != 15 {
		t.Fatalf("an agent from before fallbacks should have none, waiting 15 minutes if given one: %+v", got)
	}
}

func TestAFallbackIsAnotherOfTheOrganisationsAgents(t *testing.T) {
	r, _, _ := claudeSetup(t)
	c := must(r.s.AgentByID(r.o.ID, r.agent))
	first := must(r.s.agentNamed(r.o.ID, "Prospector"))
	must(r.a.saveAgent(context.Background(), r.o.ID, AgentSettingsIn{ID: first.ID, Name: first.Name, BaseURL: first.BaseURL, Model: first.Model,
		ToolsURL: first.ToolsURL, Tools: first.AllowedTools(), Fallback: c.ID}, "Ada Lovelace"))
	base := AgentSettingsIn{ID: c.ID, Name: c.Name, BaseURL: c.BaseURL, Model: c.Model, ToolsURL: c.ToolsURL, Tools: []string{searchTool}}
	for _, bad := range []struct {
		fallback int64
		minutes  int
	}{{c.ID, 0}, {first.ID, 0}, {999, 0}, {0, -5}, {0, 7*24*60 + 1}} {
		in := base
		in.Fallback, in.FallbackMinutes = bad.fallback, bad.minutes
		if _, err := r.a.saveAgent(context.Background(), r.o.ID, in, "Ada Lovelace"); err == nil {
			t.Fatalf("fallback %d after %d minutes was taken", bad.fallback, bad.minutes)
		}
	}
}
