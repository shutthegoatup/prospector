package app

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/api"
)

func TestACallerPastItsBurstWaitsAndOthersDont(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	u, err := s.CreateUser("ada@example.com", "Ada Lovelace", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddMember(o.ID, u.ID, "owner", u.Name, "Owner One"); err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	a := &App{s: s}
	a.rates.on, a.rates.now = true, func() time.Time { return clock }
	srv := httptest.NewServer(a.Routes())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	ada := &http.Client{Jar: jar}
	if res, err := signInWith(ada, srv.URL, "ada@example.com", "correct horse battery"); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("sign-in: %v %v", res, err)
	}
	busy, _, err := s.CreateKey(o.ID, "busy", []string{"read", "import"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	quiet, _, err := s.CreateKey(o.ID, "quiet", []string{"read"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	agents := srv.URL + "/o/alpha/api/v1/agents"

	for i := range keyRate.burst {
		if code, body := asKey(t, "GET", agents, busy, nil); code != http.StatusOK {
			t.Fatalf("request %d of the burst: %d %s", i+1, code, body)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, agents, nil)
	req.Header.Set("Authorization", "Bearer "+busy)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") != "1" {
		t.Fatalf("past the burst: %d, Retry-After %q", res.StatusCode, res.Header.Get("Retry-After"))
	}
	if code, body := asKey(t, "GET", agents, busy, nil); !strings.Contains(body, string(api.ProblemTypeThrottled)) || !strings.Contains(body, "try again in 1 second") {
		t.Fatalf("the refusal should be a throttled Problem saying how long: %d %s", code, body)
	}
	if code, body := asKey(t, "GET", srv.URL+"/o/alpha/api/v1/snapshot", busy, nil); code != http.StatusTooManyRequests {
		t.Fatalf("a key's routes outside the spec share its bucket: %d %s", code, body)
	}
	initialize := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}}}
	if code, body := asKey(t, "POST", srv.URL+"/o/alpha/mcp", busy, initialize); code != http.StatusTooManyRequests || !strings.Contains(body, "try again in 1 second") {
		t.Fatalf("so does its MCP endpoint: %d %s", code, body)
	}

	if code, body := asKey(t, "GET", agents, quiet, nil); code != http.StatusOK {
		t.Fatalf("another key of the same organisation: %d %s", code, body)
	}
	if code, body := call(t, ada, "GET", agents, "", nil); code != http.StatusOK {
		t.Fatalf("a member of the same organisation: %d %s", code, body)
	}

	clock = clock.Add(time.Second / time.Duration(keyRate.perSecond))
	if code, body := asKey(t, "GET", agents, busy, nil); code != http.StatusOK {
		t.Fatalf("a token later: %d %s", code, body)
	}
	if code, _ := asKey(t, "GET", agents, busy, nil); code != http.StatusTooManyRequests {
		t.Fatalf("that token is spent: %d", code)
	}
}

func TestAMembersPagesAreNotCountedOnlyTheirCalls(t *testing.T) {
	s, _, _, _ := signedIn(t)
	clock := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	a := &App{s: s}
	a.rates.on, a.rates.now = true, func() time.Time { return clock }
	srv := httptest.NewServer(a.Routes())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	ada := &http.Client{Jar: jar}
	if res, err := signInWith(ada, srv.URL, "ada@example.com", "correct horse battery"); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("sign-in: %v %v", res, err)
	}
	for i := range memberRate.burst + 5 {
		if code, _ := call(t, ada, "GET", srv.URL+"/o/alpha/today", "", nil); code != http.StatusOK {
			t.Fatalf("page %d: %d", i+1, code)
		}
	}
	for i := range memberRate.burst {
		if code, body := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/nav", "", nil); code != http.StatusOK {
			t.Fatalf("call %d of the burst: %d %s", i+1, code, body)
		}
	}
	if code, body := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/nav", "", nil); code != http.StatusTooManyRequests {
		t.Fatalf("past the burst: %d %s", code, body)
	}
}

func TestEveryLimitedOperationDeclaresThrottled(t *testing.T) {
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			// The account operations are asked by whoever is there, before anyone is known.
			if slices.Contains(op.Tags, "account") {
				continue
			}
			if op.Responses.Status(http.StatusTooManyRequests) == nil {
				t.Errorf("%s %s can be throttled but doesn't say 429", method, path)
			}
		}
	}
	want := fmt.Sprintf("a key may make %d at once and then %v a second, a person %d at once and then %v a second", keyRate.burst, keyRate.perSecond, memberRate.burst, memberRate.perSecond)
	if !strings.Contains(strings.Join(strings.Fields(spec.Info.Description), " "), want) {
		t.Errorf("the spec's description should state the limits as the server has them:\n%s", want)
	}
}

func TestRatesAreOffUntilServeTurnsThemOn(t *testing.T) {
	var g rateGuard
	for range keyRate.burst + 1 {
		if wait := g.take("anyone", keyRate); wait != 0 {
			t.Fatalf("an App the tests make shouldn't limit: wait %v", wait)
		}
	}
	main, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(main), "serve(&App{s: s, rates: rateGuard{on: true},") {
		t.Error("serve should start the App it serves with rates on")
	}
}

func TestFullBucketsAreForgottenToMakeRoom(t *testing.T) {
	clock := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	g := &rateGuard{on: true, now: func() time.Time { return clock }}
	for i := range maxTracked {
		g.take(fmt.Sprint(i), keyRate)
	}
	if len(g.buckets) != maxTracked {
		t.Fatalf("%d buckets, want %d", len(g.buckets), maxTracked)
	}
	clock = clock.Add(time.Minute)
	g.take("newcomer", keyRate)
	if len(g.buckets) != 1 || g.buckets["newcomer"] == nil {
		t.Fatalf("buckets that filled up again should make room: %d left", len(g.buckets))
	}
}
