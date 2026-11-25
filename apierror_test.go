package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
)

// An agent is told what it got wrong in the store's words, and nothing of the database's.
func TestAgentToolsDontPassDatabaseErrorsOn(t *testing.T) {
	s, o, srv, _ := signedIn(t)
	key, _, _ := s.CreateKey(o.ID, "claude", []string{"read", "research:write"}, "test")
	session := mcpSession(t, srv.URL, key)
	said := func(name string, args map[string]any) string {
		t.Helper()
		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || !res.IsError {
			t.Fatalf("%s should have been refused: %v %+v", name, err, res)
		}
		text, _ := json.Marshal(res.Content)
		return string(text)
	}
	if got := said("add_fact", map[string]any{"company": "Nobody Ltd", "field": "configurator", "value": "x", "source_url": "https://nobody.example"}); !strings.Contains(got, "Nobody Ltd") {
		t.Errorf("an agent naming a company that isn't here is told: %s", got)
	}
	s.db.Callback().Query().Before("gorm:query").Register("lost", func(tx *gorm.DB) {
		if tx.Statement.Table == "companies" || tx.Statement.Table == "contacts" || tx.Statement.Table == "tasks" {
			tx.AddError(&pgconn.PgError{Code: "08006", Message: "connection to db.internal:5432 lost"})
		}
	})
	_, err := session.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "work_task", Arguments: map[string]string{"id": "1"}})
	if err == nil || !strings.Contains(err.Error(), "try again in a moment") || strings.Contains(err.Error(), "db.internal") {
		t.Errorf("an agent asking for a task's prompt with the database gone is told: %v", err)
	}
	if got := said("search", map[string]any{"query": "yachts"}); !strings.Contains(got, "try again in a moment") || strings.Contains(got, "db.internal") {
		t.Errorf("an agent searching with the database gone is told: %s", got)
	}
}

// Two people asking for the same new address at once both find it free; the one the
// database stops is told the same as if they had come second.
func TestALostRaceForAnAddressReadsAsTaken(t *testing.T) {
	s := tempStore(t)
	mustOrg(t, s, "alpha")
	clash := s.db.Create(&Org{Slug: "alpha", Name: "Alpha", Color: "#56c5f4"}).Error
	if !unstorable(clash) {
		t.Fatalf("the index on an organisation's slug answered: %v", clash)
	}
	// The look-up finds /o/beta free, and the index then answers as it just did.
	s.db.Callback().Create().Before("gorm:create").Register("beaten", func(tx *gorm.DB) {
		if tx.Statement.Table == "orgs" {
			tx.AddError(clash)
		}
	})
	_, err := s.CreateOrg("beta", "Beta")
	if err == nil || !strings.Contains(err.Error(), "already has the address /o/beta") || statusOf(err) != http.StatusBadRequest {
		t.Errorf("the second of two taking /o/beta at once: %v", err)
	}
}

// Nobody is shown the database's own words: not a page's script, not an API caller.
func TestNoRawDatabaseErrorsGoOut(t *testing.T) {
	s := tempStore(t)
	mustOrg(t, s, "alpha")
	clash := s.db.Create(&Org{Slug: "alpha", Name: "Alpha", Color: "#56c5f4"}).Error
	if clash == nil {
		t.Fatal("a second organisation took a slug already in use")
	}
	raw := regexp.MustCompile(`(?i)unique|constraint|duplicate|orgs|sqlstate|context|/var/lib`)
	for name, c := range map[string]struct {
		err         error
		given, code int
		text        string
	}{
		"a row the database turns down":        {fmt.Errorf("saving it: %w", clash), 400, 400, sayUnstorable},
		"the same, from a handler calling 500": {clash, 500, 400, sayUnstorable},
		"a database that has gone away":        {fmt.Errorf("query: %w", &pgconn.PgError{Code: "08006", Message: "connection failure"}), 400, 500, sayOurs},
		"a failure a handler calls its own":    {errors.New("open /var/lib/prospector/x: permission denied"), 500, 500, sayOurs},
		"what the caller got wrong":            {errors.New("a fact needs a field and a value"), 400, 400, "a fact needs a field and a value"},
		"what another server said":             {errors.New("the mail server refused the password"), 502, 502, "the mail server refused the password"},
	} {
		page := httptest.NewRecorder()
		fail(page, c.err, c.given)
		body := strings.TrimSpace(page.Body.String())
		if page.Code != c.code || body != c.text || raw.MatchString(body) {
			t.Errorf("%s, to a page: %d %q, want %d %q", name, page.Code, body, c.code, c.text)
		}
	}

	if !fromDatabase(&pgconn.ConnectError{}) {
		t.Error("a failure to connect to Postgres, which names its user and host, would go out as it is")
	}

	// The sign-in page says as little when the right password can't be given a session.
	app, _, down, _ := signedIn(t)
	app.db.Callback().Create().Before("gorm:create").Register("no-sessions", func(tx *gorm.DB) {
		if tx.Statement.Table == "sessions" {
			tx.AddError(&pgconn.PgError{Code: "08006", Message: "connection to db.internal:5432 lost"})
		}
	})
	res, err := http.PostForm(down.URL+"/login", url.Values{"email": {"ada@example.com"}, "password": {"correct horse battery"}})
	if err != nil {
		t.Fatal(err)
	}
	said, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusInternalServerError || !strings.Contains(string(said), "try again in a moment") || strings.Contains(string(said), "db.internal") {
		t.Errorf("signing in when no session can be kept: %d %s", res.StatusCode, said)
	}

	api := httptest.NewRecorder()
	apiFail(api, httptest.NewRequest("POST", "/o/alpha/api/v1/companies", nil), clash)
	if api.Code != http.StatusBadRequest || !strings.Contains(api.Body.String(), sayUnstorable) || raw.MatchString(api.Body.String()) {
		t.Errorf("to an API caller: %d %s", api.Code, api.Body)
	}
}

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
		{missing(gorm.ErrRecordNotFound, "no task %d", 3), 404},
		// A look-up that failed says nothing about whether the thing is there.
		{missing(context.DeadlineExceeded, "no task %d", 3), 500},
		{missing(&pgconn.PgError{Code: "08006"}, "no task %d", 3), 500},
		// A character Postgres won't store is the caller's to take out.
		{&pgconn.PgError{Code: "22021"}, 400},
		{&pgconn.PgError{Code: "23505"}, 400},
	} {
		if got := statusOf(c.err); got != c.code {
			t.Errorf("%v: %d, want %d", c.err, got, c.code)
		}
	}
}
