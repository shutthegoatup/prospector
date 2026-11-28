package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/request"
)

// Every answer an operation gives across the suite is held to the spec: a status it
// doesn't declare, or an error that isn't a Problem as the spec describes one, fails the
// run, so a client made from the spec knows every way a call can go.
var answers struct {
	sync.Mutex
	wrong map[string]bool
}

func TestMain(m *testing.M) {
	if err := webBuilt(assetFS); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	spec, err := api.GetSpec()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ops := map[string]*openapi3.Operation{}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			ops[method+" "+path] = op
		}
	}
	problem := spec.Components.Schemas["Problem"].Value
	answers.wrong = map[string]bool{}
	seen := func(pattern string, status int, h http.Header, body []byte) {
		if why := againstSpec(ops[pattern], problem, status, h, body); why != "" {
			answers.Lock()
			answers.wrong[fmt.Sprintf("%s answered %d: %s", pattern, status, why)] = true
			answers.Unlock()
		}
	}
	watchAPI = func(mux *http.ServeMux) api.ServeMux { return watched{mux, seen} }
	code := withDatabases(m.Run)
	if len(answers.wrong) > 0 {
		wrong := make([]string, 0, len(answers.wrong))
		for w := range answers.wrong {
			wrong = append(wrong, w)
		}
		sort.Strings(wrong)
		fmt.Fprintf(os.Stderr, "answers the spec doesn't describe:\n  %s\n", strings.Join(wrong, "\n  "))
		code = 1
	}
	os.Exit(code)
}

func TestEveryOperationNamesItsErrors(t *testing.T) {
	t.Parallel()
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			if op.Responses.Default() != nil {
				t.Errorf("%s %s answers a default rather than naming its errors", method, path)
			}
			for _, status := range []int{403, 500} {
				if op.Responses.Status(status) == nil {
					t.Errorf("%s %s doesn't declare %d", method, path, status)
				}
			}
			for code, res := range op.Responses.Map() {
				if code >= "400" && res.Value.Content.Get("application/problem+json") == nil {
					t.Errorf("%s %s answers %s with something other than a Problem", method, path, code)
				}
			}
		}
	}
}

func TestAProblemToActOnLinksToWhatItMeans(t *testing.T) {
	t.Parallel()
	_, _, srv, _ := signedIn(t)
	res := asBrowser(t, freshBrowser(), "GET", srv.URL+"/api/v1/session", nil)
	p := problemOf(t, res)
	if p.Type != api.ProblemTypeSignedOut || p.Title != "Signed out" || p.Status != http.StatusUnauthorized {
		t.Fatalf("signed out: %+v", p)
	}
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := noRedirect.Get(srv.URL + string(p.Type))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	entry := res.Header.Get("Location")
	if res.StatusCode != http.StatusFound || entry != "/api#problem-signed_out" {
		t.Fatalf("%s: %s %q", p.Type, res.Status, entry)
	}
	page, err := http.Get(srv.URL + "/api")
	if err != nil {
		t.Fatal(err)
	}
	defer page.Body.Close()
	body, _ := io.ReadAll(page.Body)
	if !strings.Contains(string(body), `id="problem-signed_out"`) || !strings.Contains(string(body), "no session or key") {
		t.Fatal("the reference page has no entry for signed_out")
	}
	for _, name := range []string{"about:blank", "nope"} {
		res, err := noRedirect.Get(srv.URL + "/api/problems/" + name)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("/api/problems/%s: %s", name, res.Status)
		}
	}
}

func TestEveryProblemTypeHasItsTitle(t *testing.T) {
	t.Parallel()
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range spec.Components.Schemas["ProblemType"].Value.Enum {
		kind := api.ProblemType(e.(string))
		if _, ok := request.ProblemTitles[kind]; !ok && kind != api.ProblemTypeUntyped {
			t.Errorf("%s has no title, so it would go out as about:blank", kind)
		}
	}
}

func TestFindingSingleSignOnWithoutAnAddressIsNotHere(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer((&App{s: tempStore(t)}).Routes())
	t.Cleanup(srv.Close)
	res := asBrowser(t, freshBrowser(), "POST", srv.URL+"/api/v1/sign-in/sso", api.FindSingleSignOnJSONRequestBody{Email: "ada@alpha.example"})
	if p := problemOf(t, res); res.StatusCode != http.StatusNotFound || p.Status != http.StatusNotFound {
		t.Fatalf("no PROSPECTOR_URL: %s %+v", res.Status, p)
	}
}

func TestAnAPICallRefusedBeforeItsOperationIsStillAProblem(t *testing.T) {
	t.Parallel()
	_, _, srv, client := signedIn(t)
	for name, c := range map[string]struct {
		path, contentType string
		status            int
	}{
		"a body that isn't JSON":     {"/o/alpha/api/v1/people", "text/plain", http.StatusUnsupportedMediaType},
		"an organisation not theirs": {"/o/nope/api/v1/people", "application/json", http.StatusNotFound},
	} {
		req, _ := http.NewRequest("POST", srv.URL+c.path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", c.contentType)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if p := problemOf(t, res); res.StatusCode != c.status || res.Header.Get("Content-Type") != "application/problem+json" || p.Status != c.status {
			t.Errorf("%s: %s %s %+v", name, res.Status, res.Header.Get("Content-Type"), p)
		}
		res.Body.Close()
	}
}

func againstSpec(op *openapi3.Operation, problem *openapi3.Schema, status int, h http.Header, body []byte) string {
	if op == nil {
		return "no operation in the spec"
	}
	declared := op.Responses.Status(status)
	if declared == nil {
		return "a status it doesn't declare"
	}
	if status < 400 {
		return ""
	}
	if ct := h.Get("Content-Type"); ct != "application/problem+json" {
		return fmt.Sprintf("an error as %q, not a Problem", ct)
	}
	if declared.Value.Content.Get("application/problem+json") == nil {
		return "declared without a Problem"
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return "a Problem that isn't JSON"
	}
	if err := problem.VisitJSON(v); err != nil {
		return "a Problem the spec doesn't describe: " + oneLine(err.Error())
	}
	return ""
}

type watched struct {
	*http.ServeMux
	seen func(pattern string, status int, header http.Header, body []byte)
}

func (m watched) HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request)) {
	m.ServeMux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		rec := &recorder{ResponseWriter: w}
		h(rec, r)
		m.seen(pattern, rec.status(), w.Header(), rec.body)
	})
}

type recorder struct {
	http.ResponseWriter
	code int
	body []byte
}

func (r *recorder) WriteHeader(code int) {
	if r.code == 0 {
		r.code = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	if r.code >= 400 && len(r.body) < 8<<10 {
		r.body = append(r.body, b...)
	}
	return r.ResponseWriter.Write(b)
}

func (r *recorder) Flush() {
	http.NewResponseController(r.ResponseWriter).Flush()
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *recorder) status() int {
	if r.code == 0 {
		return http.StatusOK
	}
	return r.code
}
