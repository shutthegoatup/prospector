package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/api"
)

// asKey calls the API with an API key, as an agent or script would.
func asKey(t *testing.T, method, u, key string, body any) (int, string) {
	t.Helper()
	var r io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		r = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, u, r)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(out)
}

func TestARobotWorksAgentsThroughTheAPIAsItsScopesAllow(t *testing.T) {
	fake := newFakeEndpoint(t)
	s, srv, _ := agentSite(t, fake)
	o := must(s.OrgBySlug("alpha"))
	key := func(scopes ...string) string {
		secret, _, err := s.CreateKey(o.ID, "robot "+strings.Join(scopes, "+"), scopes, "Ada Lovelace")
		if err != nil {
			t.Fatal(err)
		}
		return secret
	}
	reader, runner, configurer := key("read"), key("read", "agents:run"), key("agents:configure")
	agents := srv.URL + "/o/alpha/api/v1/agents"

	if code, body := asKey(t, "GET", agents, "", nil); code != http.StatusUnauthorized || !strings.Contains(body, "API key") {
		t.Fatalf("nobody at all: %d %s", code, body)
	}
	if code, body := asKey(t, "GET", agents, reader, nil); code != http.StatusOK || body != "{\"items\":[]}\n" {
		t.Fatalf("a read key listing no agents: %d %s", code, body)
	}
	settings := map[string]any{"name": "Gateway", "base_url": fake.srv.URL + "/v1", "api_key": agentTestKey, "model": "router/auto"}
	if code, body := asKey(t, "POST", agents, runner, settings); code != http.StatusForbidden || !strings.Contains(body, "agents:configure") {
		t.Fatalf("a key without agents:configure adding an agent: %d %s", code, body)
	}
	code, body := asKey(t, "POST", agents, configurer, settings)
	var saved api.AgentSaved
	if json.Unmarshal([]byte(body), &saved); code != http.StatusCreated || !saved.Agent.Ready || !saved.Agent.HasKey || strings.Contains(body, agentTestKey) {
		t.Fatalf("a configuring key adding an agent: %d %s", code, body)
	}
	if c := must(s.AgentByID(o.ID, saved.Agent.Id)); c.UpdatedBy != "agent:robot agents:configure" {
		t.Fatalf("the change should be the key's: %+v", c)
	}
	settings["model"], settings["api_key"] = "router/smart", ""
	if code, body := asKey(t, "PUT", fmt.Sprintf("%s/%d", agents, saved.Agent.Id), configurer, settings); code != http.StatusOK || !strings.Contains(body, `"model":"router/smart"`) {
		t.Fatalf("changing the model, keeping the key: %d %s", code, body)
	}
	if code, body := asKey(t, "POST", agents, configurer, map[string]any{"name": 7}); code != http.StatusBadRequest {
		t.Fatalf("a body that isn't the spec's: %d %s", code, body)
	}

	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders with a configurator.", "Ada Lovelace"))
	runs := fmt.Sprintf("%s/o/alpha/api/v1/tasks/%d/runs", srv.URL, task.ID)
	if code, body := asKey(t, "POST", runs, reader, map[string]any{"agent": saved.Agent.Id}); code != http.StatusForbidden {
		t.Fatalf("a read key starting a run: %d %s", code, body)
	}
	code, body = asKey(t, "POST", runs, runner, map[string]any{"agent": saved.Agent.Id})
	var run api.Run
	if json.Unmarshal([]byte(body), &run); code != http.StatusCreated || run.AgentName != "Gateway" || run.StartedBy != "agent:robot read+agents:run" {
		t.Fatalf("a run key starting a run: %d %s", code, body)
	}
	if code, body := asKey(t, "GET", runs, reader, nil); code != http.StatusOK || !strings.Contains(body, fmt.Sprintf(`"id":%d`, run.Id)) {
		t.Fatalf("listing the task's runs: %d %s", code, body)
	}
	if code, body := asKey(t, "POST", fmt.Sprintf("%s/o/alpha/api/v1/runs/%d/stop", srv.URL, run.Id), runner, nil); code != http.StatusNoContent {
		t.Fatalf("stopping the run: %d %s", code, body)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var ended AgentRun
		if err := s.db.First(&ended, run.Id).Error; err == nil && ended.Status != RunWorking && ended.Status != RunWaiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the stopped run never ended")
		}
	}
	if code, body := asKey(t, "POST", fmt.Sprintf("%s/o/alpha/api/v1/runs/%d/stop", srv.URL, run.Id), runner, nil); code != http.StatusNoContent {
		t.Fatalf("stopping a run that has ended, as the contract says, is accepted: %d %s", code, body)
	}
	if code, _ := asKey(t, "POST", srv.URL+"/o/alpha/api/v1/runs/999/stop", runner, nil); code != http.StatusNotFound {
		t.Fatalf("stopping a run that isn't there: %d", code)
	}
	if code, _ := asKey(t, "GET", fmt.Sprintf("%s/o/alpha/api/v1/agents/%d", srv.URL, 999), reader, nil); code != http.StatusNotFound {
		t.Fatalf("an agent that isn't there: %d", code)
	}
	if code, _ := asKey(t, "GET", fmt.Sprintf("%s/o/alpha/api/v1/tasks/%d/runs", srv.URL, 999), reader, nil); code != http.StatusNotFound {
		t.Fatalf("another task's runs: %d", code)
	}

	beta := mustOrg(t, s, "beta")
	theirs, _, _ := s.CreateKey(beta.ID, "theirs", []string{"read", "agents:configure"}, "Grace Hopper")
	if code, _ := asKey(t, "GET", agents, theirs, nil); code != http.StatusForbidden {
		t.Fatalf("another organisation's key: %d", code)
	}
}

func TestTheContractPageIsPublicAndShowsEveryOperation(t *testing.T) {
	fake := newFakeEndpoint(t)
	_, srv, _ := agentSite(t, fake)
	code, body := call(t, newMailClient(), "GET", srv.URL+"/api", "", nil)
	if code != http.StatusOK || strings.Contains(body, "<script") {
		t.Fatalf("the contract page, signed out: %d", code)
	}
	spec := must(api.Spec())
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			if !strings.Contains(body, `id="op-`+op.OperationID+`"`) || !strings.Contains(body, path) {
				t.Errorf("the page lacks %s %s", method, path)
			}
		}
	}
	for name := range spec.Components.Schemas {
		if !strings.Contains(body, `id="schema-`+name+`"`) {
			t.Errorf("the page lacks the %s schema", name)
		}
	}
	for _, s := range Scopes {
		if !strings.Contains(body, "<code>"+s.Name+"</code>") {
			t.Errorf("the page doesn't explain the %s scope", s.Name)
		}
	}
}

func TestTheSpecIsServedAndEveryOperationNamesAScopeAKeyCanHave(t *testing.T) {
	fake := newFakeEndpoint(t)
	_, srv, _ := agentSite(t, fake)
	code, body := call(t, newMailClient(), "GET", srv.URL+"/api/openapi.json", "", nil)
	if code != http.StatusOK || !strings.Contains(body, `"createAgent"`) {
		t.Fatalf("the spec: %d %.200s", code, body)
	}
	spec := must(api.Spec())
	ops := 0
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			ops++
			var scope string
			if op.Security != nil {
				for _, req := range *op.Security {
					if s := req["bearer"]; len(s) == 1 {
						scope = s[0]
					}
				}
			}
			if !validScope(scope) {
				t.Errorf("%s %s names no scope a key can be given: %q", method, path, scope)
			}
		}
	}
	if ops < 8 {
		t.Fatalf("only %d operations in the spec", ops)
	}
}
