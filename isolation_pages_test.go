package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
)

// sourceFiles are the package's own, without tests or generated templates.
func sourceFiles(t *testing.T) (names []string) {
	t.Helper()
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if n := f.Name(); strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") && !strings.HasSuffix(n, "_templ.go") {
			names = append(names, n)
		}
	}
	return names
}

// wrongField reads, from a refused body, the field that was the wrong kind of value.
var wrongField = regexp.MustCompile(`cannot unmarshal \w+ into Go (?:struct field|value of type) \S*?\.?([a-z_]+) of type`)

var routeRe = regexp.MustCompile(`mux\.HandleFunc\("(GET|POST) (/o/\{org\}/[^"]*)"`)

// orgRoutes reads every page and form of an organisation out of the source, so a
// route added later is tried without anyone remembering to list it.
func orgRoutes(t *testing.T) (gets, posts []string) {
	t.Helper()
	for _, name := range sourceFiles(t) {
		text, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range routeRe.FindAllStringSubmatch(string(text), -1) {
			if m[1] == "GET" {
				gets = append(gets, m[2])
			} else {
				posts = append(posts, m[2])
			}
		}
	}
	return gets, posts
}

// bodyFields is every field a JSON body can carry anywhere in the package, and
// whether it is read as a number, as text or as a yes-or-no. A name read both as a
// number and as text is in both.
func bodyFields(t *testing.T) (numbers, texts, flags map[string]bool) {
	t.Helper()
	numbers, texts, flags = map[string]bool{}, map[string]bool{}, map[string]bool{}
	fset := token.NewFileSet()
	for _, name := range sourceFiles(t) {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			f, ok := n.(*ast.Field)
			if !ok || f.Tag == nil {
				return true
			}
			tag := regexp.MustCompile(`json:"([a-z_]+)`).FindStringSubmatch(f.Tag.Value)
			if tag == nil {
				return true
			}
			kind := f.Type
			if star, ok := kind.(*ast.StarExpr); ok {
				kind = star.X
			}
			if id, ok := kind.(*ast.Ident); ok {
				switch id.Name {
				case "int64", "int", "float64":
					numbers[tag[1]] = true
				case "string":
					texts[tag[1]] = true
				case "bool":
					flags[tag[1]] = true
				}
			}
			return true
		})
	}
	return numbers, texts, flags
}

func signInAs(t *testing.T, srv *httptest.Server, email, password string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	res, err := client.PostForm(srv.URL+"/login", url.Values{"email": {email}, "password": {password}})
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("signing in as %s: %v %v", email, res, err)
	}
	res.Body.Close()
	return client
}

// The store's methods are tried in isolation_test.go. Some pages and forms ask the
// database themselves, so every one of them is tried here the same way: by the owner
// of a second organisation, with the first one's ids and what would find its rows.
func TestNoPageOrFormReachesIntoAnotherOrganisation(t *testing.T) {
	s := tempStore(t)
	first := seedFirst(t, s)
	before := holdings(t, s, first.ID)
	second := mustOrg(t, s, "beta")
	owner := must(s.CreateUser("owner@beta.example", "Owner Two", "another long password"))
	if err := s.AddMember(second.ID, owner.ID, "owner", ""); err != nil {
		t.Fatal(err)
	}
	nowhere := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("no network in this test") })}
	app := &App{s: s, outbound: nowhere, txt: func(context.Context, string) ([]string, error) { return nil, nil }, later: func(func()) {}}
	srv := httptest.NewServer(app.Routes())
	t.Cleanup(srv.Close)
	intruder := signInAs(t, srv, "owner@beta.example", "another long password")
	theirOwner := signInAs(t, srv, "owner@alpha.example", "a long enough password")

	gets, posts := orgRoutes(t)
	if len(gets) < 10 || len(posts) < 30 {
		t.Fatalf("found only %d pages and %d forms in the source: has the way routes are written changed?", len(gets), len(posts))
	}
	unchanged := func(after string) {
		t.Helper()
		now := holdings(t, s, first.ID)
		for table, was := range before {
			if now[table] != was {
				t.Fatalf("%s, by another organisation's owner, changed the first one's %s:\nbefore %s\nafter  %s", after, table, was, now[table])
			}
		}
	}

	shown := 0
	for _, route := range gets {
		path := strings.ReplaceAll(route, "{id}", "1")
		path = strings.TrimSuffix(path, "{$}")
		for _, query := range []string{"", "?q=" + findable, "?f=review", "?f=details", "?closed=1", "?spam=1", "?stage=qualified"} {
			if _, body := call(t, intruder, "GET", srv.URL+strings.Replace(path, "{org}", "beta", 1)+query, "", nil); strings.Contains(body, theirs) {
				t.Errorf("GET %s%s as another organisation's owner shows the first one's rows", route, query)
			}
		}
		// Their own address under the other organisation's name is refused whole.
		if code, body := call(t, intruder, "GET", srv.URL+strings.Replace(path, "{org}", "alpha", 1), "", nil); code != http.StatusNotFound || strings.Contains(body, theirs) {
			t.Errorf("GET %s in the first organisation, by someone who isn't in it: %d", route, code)
		}
		if _, body := call(t, theirOwner, "GET", srv.URL+strings.Replace(path, "{org}", "alpha", 1), "", nil); strings.Contains(body, theirs) {
			shown++
		}
	}
	// The first organisation's own owner does see its rows on those pages, so not
	// seeing them above means something.
	if shown < 8 {
		t.Fatalf("only %d of %d pages show the first organisation its own rows", shown, len(gets))
	}
	unchanged("reading the pages")

	numbers, texts, flags := bodyFields(t)
	for _, route := range posts {
		heard := false
		for _, word := range tryWords {
			for _, preferNumbers := range []bool{true, false} {
				for _, yes := range []bool{true, false} {
					body := map[string]any{}
					for name := range flags {
						body[name] = yes
					}
					for name := range texts {
						if !numbers[name] || !preferNumbers {
							body[name] = word
						}
					}
					for name := range numbers {
						if !texts[name] || preferNumbers {
							body[name] = 1
						}
					}
					body["date"], body["next_date"], body["close_date"] = "2020-01-02", "2020-01-02", "2020-01-02"
					var code int
					var said string
					// A field this form reads as something else (a list, say) is left out and the form tried again.
					for range 8 {
						code, said = call(t, intruder, "POST", srv.URL+strings.Replace(route, "{org}", "beta", 1), "application/json", body)
						wrong := wrongField.FindStringSubmatch(said)
						if wrong == nil {
							break
						}
						delete(body, wrong[1])
					}
					if strings.Contains(said, theirs) {
						t.Errorf("POST %s as another organisation's owner answered with the first one's rows: %.200s", route, said)
					}
					if code != http.StatusUnsupportedMediaType && !strings.Contains(said, "bad request body") && !strings.Contains(said, "404 page not found") {
						heard = true
					}
				}
			}
		}
		if !heard {
			t.Errorf("POST %s never got as far as reading its body, so nothing was learnt about it", route)
		}
		unchanged("POST " + route)
		if code, _ := call(t, intruder, "POST", srv.URL+strings.Replace(route, "{org}", "alpha", 1), "application/json", map[string]any{"id": 1}); code != http.StatusNotFound {
			t.Errorf("POST %s in the first organisation, by someone who isn't in it: %d", route, code)
		}
	}
	unchanged("every form")

	for _, route := range posts {
		// Once more with the first organisation's own values, as its rows hold them, for
		// a form that finds a row by its text. The answer may say them back, so only what
		// changed is looked at.
		named := map[string]any{}
		for name := range texts {
			named[name] = theirs + " " + name
		}
		for range 8 {
			_, said := call(t, intruder, "POST", srv.URL+strings.Replace(route, "{org}", "beta", 1), "application/json", named)
			wrong := wrongField.FindStringSubmatch(said)
			if wrong == nil {
				break
			}
			delete(named, wrong[1])
		}
		unchanged("POST " + route + " with the first organisation's own values")
	}
}
