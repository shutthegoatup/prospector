package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeWeb is a SearXNG instance and the pages its results point to.
type fakeWeb struct {
	srv     *httptest.Server
	queries []string
	// search answers a search; nil is the results below.
	search func(w http.ResponseWriter, r *http.Request) bool
}

func newFakeWeb(t *testing.T, trusting *fakeEndpoint) *fakeWeb {
	t.Helper()
	f := &fakeWeb{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		f.queries = append(f.queries, r.URL.RawQuery)
		if f.search != nil && f.search(w, r) {
			return
		}
		if r.URL.Query().Get("format") != "json" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"query":%q,"results":[{"url":"%s/larkmoor","title":"Larkmoor Yachts – build your boat","content":"Configure a day cruiser online.","engine":"duckduckgo"},`+
			`{"url":"%s/other","title":"Another yard","content":"","engine":"bing"}]}`, r.URL.Query().Get("q"), f.srv.URL, f.srv.URL)
	})
	mux.HandleFunc("GET /larkmoor", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/larkmoor/build", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /larkmoor/build", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>Build your Larkmoor</title><style>body{color:red}</style>
<script>ignore("this")</script></head><body><header><a href="/">Home</a></header>
<h1>Day cruiser configurator</h1><p>Choose a hull, an engine   and a colour.</p><p>Larkmoor employs 40 people in Lymington.</p>
<a href="/contact#team">Contact the team</a><noscript>Turn on scripts</noscript></body></html>`)
	})
	mux.HandleFunc("GET /brochure.pdf", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.7"))
	})
	f.srv = httptest.NewTLSServer(mux)
	t.Cleanup(f.srv.Close)
	if trusting != nil {
		trusting.srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs.AddCert(f.srv.Certificate())
	}
	return f
}

func TestWebSearchAsksSearXNGForJSON(t *testing.T) {
	web := newFakeWeb(t, nil)
	a := &App{agentClient: web.srv.Client()}
	got, err := a.webSearch(context.Background(), web.srv.URL+"/", "yacht configurator", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "1. Larkmoor Yachts – build your boat\n   "+web.srv.URL+"/larkmoor\n   Configure a day cruiser online.") || !strings.Contains(got, "2. Another yard") {
		t.Fatalf("search results as the model reads them:\n%s", got)
	}
	if q := web.queries[0]; !strings.Contains(q, "format=json") || !strings.Contains(q, "pageno=2") || !strings.Contains(q, "q=yacht+configurator") {
		t.Fatalf("what SearXNG was asked: %s", q)
	}

	web.search = func(w http.ResponseWriter, r *http.Request) bool { w.WriteHeader(http.StatusForbidden); return true }
	if _, err := a.webSearch(context.Background(), web.srv.URL, "x", 0); err == nil || !strings.Contains(err.Error(), "search.formats") {
		t.Fatalf("an instance without JSON should say how to turn it on: %v", err)
	}
	web.search = func(w http.ResponseWriter, r *http.Request) bool {
		w.WriteHeader(http.StatusTooManyRequests)
		return true
	}
	if _, err := a.webSearch(context.Background(), web.srv.URL, "x", 0); err == nil || !strings.Contains(err.Error(), "Too Many Requests") {
		t.Fatalf("a limited instance should be waited out like any limit: %v", err)
	}
}

func TestFetchPageReadsWhatABrowserWouldShow(t *testing.T) {
	web := newFakeWeb(t, nil)
	a := &App{agentClient: web.srv.Client()}
	got, err := a.fetchPage(context.Background(), web.srv.URL+"/larkmoor")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Address: " + web.srv.URL + "/larkmoor/build", "Title: Build your Larkmoor", "Day cruiser configurator",
		"Choose a hull, an engine and a colour.", "Larkmoor employs 40 people", "- Contact the team: " + web.srv.URL + "/contact"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the page should have %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"ignore(", "color:red", "Turn on scripts", "#team"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("the page shouldn't have %q:\n%s", unwanted, got)
		}
	}
	if _, err := a.fetchPage(context.Background(), web.srv.URL+"/brochure.pdf"); err == nil || !strings.Contains(err.Error(), "application/pdf") {
		t.Fatalf("a PDF: %v", err)
	}
	for _, bad := range []string{"", "file:///etc/passwd", "ftp://example.com/x", "larkmoor.example"} {
		if _, err := a.fetchPage(context.Background(), bad); err == nil {
			t.Fatalf("%q was fetched", bad)
		}
	}
	// Prospector's own client refuses this machine's network, redirects included.
	if _, err := (&App{}).fetchPage(context.Background(), "http://127.0.0.1:1/"); err == nil || !strings.Contains(err.Error(), "public address") {
		t.Fatalf("a private address: %v", err)
	}
}

func TestAnAgentSearchesAndReadsTheWebWithNoPaidService(t *testing.T) {
	fake := newFakeEndpoint(t)
	web := newFakeWeb(t, fake)
	r := newRunSetup(t, fake)
	c := must(r.s.AgentByID(r.o.ID, r.agent))
	saved := must(r.a.saveAgent(context.Background(), r.o.ID, AgentSettingsIn{ID: c.ID, Name: c.Name, BaseURL: c.BaseURL, Model: c.Model,
		SearchURL: web.srv.URL}, "Ada Lovelace"))
	if saved.SearchURL != web.srv.URL || saved.Problem != "" {
		t.Fatalf("an agent given a SearXNG instance, with no tool server: %+v", saved)
	}

	page := web.srv.URL + "/larkmoor/build"
	fake.chat = func(n int, body map[string]any) (int, string) {
		switch n {
		case 0:
			return 200, chatTools(webSearchTool, `{"query":"yacht configurator"}`)
		case 1:
			return 200, chatTools(fetchPageTool, fmt.Sprintf(`{"url":%q}`, web.srv.URL+"/larkmoor"))
		case 2:
			return 200, chatTools(
				"add_company", fmt.Sprintf(`{"name":"Larkmoor Yachts","facts":[{"field":"size","value":"40 staff","source_url":%q}]}`, page),
				"add_fact", `{"company":"Larkmoor Yachts","field":"hq","value":"Lymington","source_url":"https://made-up.example/about"}`)
		}
		return 200, chatTools("finish_request", fmt.Sprintf(`{"request":%d}`, r.brief))
	}
	run := r.finished(t, must(r.a.StartRun(r.o.ID, r.task.ID, r.agent, "Ada Lovelace")).ID)
	if run.Status != RunFinished || run.Added != 1 {
		t.Fatalf("a run on free search: %+v", run)
	}
	facts := must(r.s.Facts(r.o.ID, must(r.s.companyRef(r.o.ID, "Larkmoor Yachts")).ID))
	if len(facts) != 1 || facts[0].SourceURL != page {
		t.Fatalf("only the fact from the page the agent read should be stored: %+v", facts)
	}
	fake.mu.Lock()
	first := fmt.Sprint(fake.bodies[0]["tools"])
	fake.mu.Unlock()
	if !strings.Contains(first, webSearchTool) || !strings.Contains(first, fetchPageTool) {
		t.Fatalf("the model should be offered both web tools: %s", first)
	}

	// Without an instance it can still read pages, but has no search to offer.
	if names := toolNames(webTools(&Agent{})); names != fetchPageTool {
		t.Fatalf("an agent with no search: %s", names)
	}
}

func TestASearchThatDoesntAnswerIsReportedOnSave(t *testing.T) {
	fake := newFakeEndpoint(t)
	web := newFakeWeb(t, fake)
	r := newRunSetup(t, fake)
	c := must(r.s.AgentByID(r.o.ID, r.agent))
	web.search = func(w http.ResponseWriter, r *http.Request) bool { w.WriteHeader(http.StatusForbidden); return true }
	saved := must(r.a.saveAgent(context.Background(), r.o.ID, AgentSettingsIn{ID: c.ID, Name: c.Name, BaseURL: c.BaseURL, Model: c.Model,
		SearchURL: web.srv.URL}, "Ada Lovelace"))
	if !strings.Contains(saved.Problem, "Search:") || !strings.Contains(saved.Problem, "search.formats") || saved.SearchURL != web.srv.URL {
		t.Fatalf("a search that won't answer in JSON should be saved and reported: %+v", saved)
	}
	if _, err := r.a.saveAgent(context.Background(), r.o.ID, AgentSettingsIn{ID: c.ID, Name: c.Name, BaseURL: c.BaseURL, Model: c.Model,
		SearchURL: "ftp://searx.example"}, "Ada Lovelace"); err == nil {
		t.Fatal("a search address that isn't https was taken")
	}
}

func toolNames(defs []toolDef) string {
	var out []string
	for _, d := range defs {
		out = append(out, d.Function.Name)
	}
	return strings.Join(out, " ")
}
