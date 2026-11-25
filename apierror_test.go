package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAnAPIErrorSaysWhoseFaultItIs(t *testing.T) {
	s, o, srv, _ := signedIn(t)
	reader, _, _ := s.CreateKey(o.ID, "reader", []string{"read"}, "test")
	writer, _, _ := s.CreateKey(o.ID, "bot", []string{"read", "research:write"}, "test")
	ask := func(key, method, path, body string) (*http.Response, problem) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+"/o/alpha/api/v1/"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		var p problem
		if res.StatusCode >= 400 {
			if ctype := res.Header.Get("Content-Type"); ctype != "application/problem+json" {
				t.Fatalf("%s %s: an error came as %q: %s", method, path, ctype, raw)
			}
			if err := json.Unmarshal(raw, &p); err != nil || p.Status != res.StatusCode || p.Title == "" || p.Type != "about:blank" {
				t.Fatalf("%s %s: not a problem document: %s", method, path, raw)
			}
		}
		return res, p
	}
	fact := `{"company":"Larkmoor","field":"press","value":"Opened a workshop","source_url":"https://larkmoor.example/news"}`

	for _, c := range []struct {
		name, key, method, path, body string
		code                          int
		says, challenge               string
	}{
		{"no key", "", "GET", "due", "", 401, "API key", `Bearer realm="prospector"`},
		{"a key that isn't one", "pk_nope", "GET", "due", "", 401, "unknown or revoked", `error="invalid_token"`},
		{"a key without the scope", reader, "POST", "facts", fact, 403, "research:write", `error="insufficient_scope", scope="research:write"`},
		{"a body that isn't JSON", writer, "POST", "facts", "{", 400, "bad request body", ""},
		{"a company that isn't here", writer, "GET", "companies/nope", "", 404, `no company "nope"`, ""},
		{"a fact for a company that isn't here", writer, "POST", "facts", fact, 404, `no company "Larkmoor"`, ""},
		{"a task that isn't here", writer, "GET", "tasks/99", "", 404, "no task 99", ""},
		{"a fact with no source", writer, "POST", "facts", `{"company":"Larkmoor","field":"press","value":"x"}`, 400, "source_url", ""},
	} {
		res, p := ask(c.key, c.method, c.path, c.body)
		if res.StatusCode != c.code || !strings.Contains(p.Detail, c.says) {
			t.Errorf("%s: %d %q, want %d and %q", c.name, res.StatusCode, p.Detail, c.code, c.says)
		}
		if got := res.Header.Get("WWW-Authenticate"); !strings.Contains(got, c.challenge) || (c.challenge == "" && got != "") {
			t.Errorf("%s: WWW-Authenticate is %q, want %q in it", c.name, got, c.challenge)
		}
	}

	// The database failing is the server's fault, and its own words stay in the log.
	if err := s.db.Migrator().DropTable(&Fact{}); err != nil {
		t.Fatal(err)
	}
	if _, p := ask(writer, "POST", "companies", `{"name":"Larkmoor"}`); p.Status != 0 {
		t.Fatalf("adding a company: %+v", p)
	}
	res, p := ask(writer, "POST", "facts", fact)
	if res.StatusCode != http.StatusInternalServerError || strings.Contains(strings.ToLower(p.Detail), "fact") || !strings.Contains(p.Detail, "try again") {
		t.Fatalf("a database failure: %d %q", res.StatusCode, p.Detail)
	}
}

func TestStatusOf(t *testing.T) {
	for _, c := range []struct {
		err  error
		code int
	}{
		{errors.New("a fact needs a field and a value"), 400},
		{noSuch("no contact %d", 7), 404},
		{fmt.Errorf("reading it: %w", noSuch("no task 3")), 404},
		{errErased, 409},
		{context.DeadlineExceeded, 500},
		{fmt.Errorf("query: %w", context.Canceled), 500},
	} {
		if got := statusOf(c.err); got != c.code {
			t.Errorf("%v: %d, want %d", c.err, got, c.code)
		}
	}
}
