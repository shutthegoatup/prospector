package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// federatedMCP answers as an LLM gateway's federated /mcp does: JSON-RPC over POST
// only, no sessions, a 405 for anything else, initialize answered by the gateway
// itself, and each tool's name prefixed with its server's.
type federatedMCP struct {
	mu     sync.Mutex
	calls  []string
	others int
	page   string
	// limited is how many more of each method to turn away with the gateway's 429,
	// which comes before it looks at what was asked.
	limited map[string]int
	refused int
}

func (f *federatedMCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		f.mu.Lock()
		f.others++
		f.mu.Unlock()
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var peek struct {
		Method string `json:"method"`
	}
	json.Unmarshal(raw, &peek)
	f.mu.Lock()
	turnedAway := f.limited[peek.Method] > 0
	if turnedAway {
		f.limited[peek.Method]--
		f.refused++
	}
	f.mu.Unlock()
	if turnedAway {
		w.Header().Set("Retry-After", "10")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"code":"429","message":"exceeded requests-per-minute limit (30)","type":"rate_limit_error"}}`))
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ProtocolVersion string         `json:"protocolVersion"`
			Name            string         `json:"name"`
			Arguments       map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "parse error", http.StatusBadRequest)
		return
	}
	if len(req.ID) == 0 || string(req.ID) == "null" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	// As the gateway does: a server-sent event for a client that accepts them, else JSON.
	reply := func(body map[string]any) {
		body["jsonrpc"], body["id"] = "2.0", req.ID
		raw, _ := json.Marshal(body)
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: %s\n\n", raw)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(raw)
	}
	var result any
	switch req.Method {
	case "initialize":
		version := "2025-11-25"
		if slices.Contains([]string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25"}, req.Params.ProtocolVersion) {
			version = req.Params.ProtocolVersion
		}
		result = map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{"name": "gateway", "version": "1"}}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		schema := func(field string) map[string]any {
			return map[string]any{"type": "object", "properties": map[string]any{field: map[string]any{"type": "string"}}}
		}
		result = map[string]any{"tools": []any{
			map[string]any{"name": "brave-search_brave_web_search", "description": "Search the web.", "inputSchema": schema("query")},
			map[string]any{"name": "fetch_fetch", "description": "Read a web page.", "inputSchema": schema("url")},
		}}
	case "tools/call":
		f.mu.Lock()
		f.calls = append(f.calls, req.Params.Name)
		f.mu.Unlock()
		text := "1. Larkmoor Yachts builds day cruisers. https://larkmoor.example/build"
		if req.Params.Name == "fetch_fetch" {
			text = f.page
		}
		result = map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
	default:
		reply(map[string]any{"error": map[string]any{"code": -32601, "message": "method not found: " + req.Method}})
		return
	}
	reply(map[string]any{"result": result})
}

func TestOnlyAToolServersPassingFailuresAreWaitedOut(t *testing.T) {
	for msg, passes := range map[string]bool{
		`calling "initialize": rejected by transport: sending "initialize": Too Many Requests`: true,
		`sending "tools/list": Service Unavailable`:                                            true,
		`calling "initialize": rejected by transport: sending "initialize": Unauthorized`:      false,
		`sending "initialize": Not Found`:                                                      false,
	} {
		if toolsPassing(errors.New(msg)) != passes {
			t.Errorf("%q: waited out %v, want %v", msg, !passes, passes)
		}
	}
	if !toolsPassing(fmt.Errorf("listing: %w", context.DeadlineExceeded)) {
		t.Error("a tool server that is slow to answer should be waited out")
	}
}

func TestARunWaitsOutAGatewaysLimitOnItsTools(t *testing.T) {
	for _, method := range []string{"initialize", "tools/list", "tools/call"} {
		t.Run(method, func(t *testing.T) {
			gateway := &federatedMCP{limited: map[string]int{method: 2}}
			tools := httptest.NewTLSServer(gateway)
			t.Cleanup(tools.Close)
			fake := newFakeEndpoint(t)
			fake.srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs.AddCert(tools.Certificate())
			r := newRunSetup(t, fake)
			gateway.mu.Lock()
			gateway.limited[method] = 0
			gateway.mu.Unlock()
			if _, err := r.a.saveAgentConfig(context.Background(), r.o.ID, AgentSettingsIn{BaseURL: fake.srv.URL + "/v1", APIKey: agentTestKey,
				Model: "router/auto", ToolsURL: tools.URL + "/mcp", Tools: []string{"brave-search_brave_web_search"}}, "Ada Lovelace"); err != nil {
				t.Fatal(err)
			}
			gateway.mu.Lock()
			gateway.limited[method], gateway.refused = 2, 0
			gateway.mu.Unlock()
			fake.chat = func(n int, _ map[string]any) (int, string) {
				if n == 0 {
					return 200, chatTools("brave-search_brave_web_search", `{"query":"yacht builders"}`)
				}
				return 200, chatTools("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
			}
			got := r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, "Ada Lovelace")).ID)
			gateway.mu.Lock()
			refused, calls := gateway.refused, slices.Clone(gateway.calls)
			gateway.mu.Unlock()
			if got.Status != RunFinished || refused != 2 || !slices.Contains(calls, "brave-search_brave_web_search") {
				t.Fatalf("a run should wait out the gateway turning away %s, then use its tools: %+v, refused %d, calls %v", method, got, refused, calls)
			}
			if len(r.sleeps) < 2 {
				t.Fatalf("it should have waited each time: %v", r.sleeps)
			}
		})
	}
}

func TestTheAgentWorksThroughAGatewaysFederatedTools(t *testing.T) {
	gateway := &federatedMCP{page: "Larkmoor Yachts was founded in 1987 on the Solent. " + strings.Repeat("It builds day cruisers to order, each finished by hand in its own yard. ", 4)}
	tools := httptest.NewTLSServer(gateway)
	t.Cleanup(tools.Close)
	fake := newFakeEndpoint(t)
	trust := fake.srv.Client()
	trust.Transport.(*http.Transport).TLSClientConfig.RootCAs.AddCert(tools.Certificate())
	r := newRunSetup(t, fake)

	found, err := r.a.listTools(context.Background(), tools.URL+"/mcp", "")
	if err != nil || len(found) != 2 || found[0].Name != "brave-search_brave_web_search" {
		t.Fatalf("listing a gateway's federated tools: %v %+v", err, found)
	}
	if _, err := r.a.saveAgentConfig(context.Background(), r.o.ID, AgentSettingsIn{BaseURL: fake.srv.URL + "/v1", APIKey: agentTestKey,
		Model: "router/auto", ToolsURL: tools.URL + "/mcp", Tools: []string{"brave-search_brave_web_search", "fetch_fetch"}}, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}

	about := "https://larkmoor.example/about"
	fake.chat = func(n int, _ map[string]any) (int, string) {
		switch n {
		case 0:
			// The page it reads, and another address slipped into the same call.
			return 200, chatTools("fetch_fetch", fmt.Sprintf(`{"url":%q,"note":"see https://made-up.example/award"}`, about))
		case 1:
			return 200, chatTools(
				"add_company", fmt.Sprintf(`{"name":"Larkmoor Yachts","facts":[{"field":"founded","value":"1987","source_url":%q}]}`, about),
				"add_fact", `{"company":"Larkmoor Yachts","field":"award","value":"Boat of the year","source_url":"https://made-up.example/award"}`)
		}
		return 200, chatTools("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	got := r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, "Ada Lovelace")).ID)
	if got.Status != RunFinished || got.Added != 1 {
		t.Fatalf("a run through a gateway's tools: %+v", got)
	}
	gateway.mu.Lock()
	calls := slices.Clone(gateway.calls)
	gateway.mu.Unlock()
	if !slices.Contains(calls, "fetch_fetch") {
		t.Fatalf("the gateway wasn't asked to fetch the page: %v", calls)
	}
	c := must(r.s.companyRef(r.o.ID, "Larkmoor Yachts"))
	facts := must(r.s.Facts(r.o.ID, c.ID))
	if len(facts) != 1 || facts[0].Field != "founded" || facts[0].SourceURL != about {
		t.Fatalf("the page it read should count as shown, and nothing else from that call: %+v", facts)
	}
}
