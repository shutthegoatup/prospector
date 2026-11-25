package main

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"slices"
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
func bodyFields(t *testing.T) (numbers, texts, flags, lists map[string]bool) {
	t.Helper()
	numbers, texts, flags, lists = map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
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
			if list, ok := kind.(*ast.ArrayType); ok {
				if of, ok := list.Elt.(*ast.Ident); ok && of.Name == "string" {
					lists[tag[1]] = true
				}
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
	return numbers, texts, flags, lists
}

// formWords are tryWords and the choices a form checks before it looks anything up.
var formWords = append([]string{"new", "renewal", "month", "year", "openai", "anthropic", "read", "owner", "https://intruder.example"}, tryWords...)

// stopsShort are the forms that never answer yes to these bodies, and why: each is
// refused for what it was sent before or after it looked for a row, which the test
// can't tell apart.
var stopsShort = map[string]string{
	"/o/{org}/ui/mail/test":         "needs a mail server to send through",
	"/o/{org}/ui/onboarding/invite": "needs a mail server to send the link through, and refuses before it looks for anyone",
	"/o/{org}/ui/subscription":      "checks a period, a currency and an amount, which one word for every field can't satisfy together",
	"/o/{org}/ui/settings":          "checks every setting at once, and changes only the organisation in its own address",
}

// looked matches a refusal that says the form went looking for a row and found none
// of this organisation's.
var looked = regexp.MustCompile(`^no \S+ |not found|isn't in this organisation|isn't listed|isn't going`)

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
	// A run still going, since stopping one only looks among those.
	if err := s.db.Model(&AgentRun{}).Where("org_id = ?", first.ID).Update("status", RunWorking).Error; err != nil {
		t.Fatal(err)
	}
	before := holdings(t, s, first.ID)
	second := mustOrg(t, s, "beta")
	owner := must(s.CreateUser("owner@beta.example", "Owner Two", "another long password"))
	if err := s.AddMember(second.ID, owner.ID, "owner", ""); err != nil {
		t.Fatal(err)
	}
	nowhere := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("no network in this test") })}
	app := &App{s: s, outbound: nowhere, later: func(func()) {},
		// Whatever domain is asked about shows the first organisation's token, so a check
		// that found its domain would verify it.
		txt:  func(context.Context, string) ([]string, error) { return []string{theirs + " token"}, nil },
		send: func(context.Context, MailServer, Message) error { return errors.New("no mail in this test") }}
	app.agentClient = nowhere
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
		for _, query := range []string{"", "?q=" + findable, "?f=review", "?f=details", "?closed=1", "?spam=1", "?stage=qualified",
			"?step=linkedin", "?segment=" + theirs, "?state=waiting", "?kind=other", "?page=2"} {
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

	numbers, texts, flags, lists := bodyFields(t)
	for _, route := range posts {
		heard, answers := false, map[string]bool{}
		for _, word := range formWords {
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
					for name := range lists {
						if _, taken := body[name]; !taken {
							body[name] = []string{word}
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
						// Text where a list was wanted: the same name is both, somewhere.
						if _, wasList := body[wrong[1]].([]string); lists[wrong[1]] && !wasList {
							body[wrong[1]] = []string{word}
						} else {
							delete(body, wrong[1])
						}
					}
					if strings.Contains(said, theirs) {
						t.Errorf("POST %s as another organisation's owner answered with the first one's rows: %.200s", route, said)
					}
					answers[fmt.Sprintf("%d %.60s", code, strings.TrimSpace(said))] = true
					// A yes, or a no that names the row it looked for, shows the form got as far as looking.
					if code == http.StatusOK || looked.MatchString(said) {
						heard = true
					}
				}
			}
		}
		if !heard && stopsShort[route] == "" {
			t.Errorf("POST %s was never answered with a yes, or a no naming the row it looked for, so nothing was learnt about it: "+
				"give formWords what it checks for, or name it in stopsShort with why. It answered: %v", route, slices.Sorted(maps.Keys(answers)))
		}
		if heard && stopsShort[route] != "" {
			t.Errorf("POST %s is in stopsShort and did answer: take it out", route)
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
		said := ""
		for range 8 {
			_, said = call(t, intruder, "POST", srv.URL+strings.Replace(route, "{org}", "beta", 1), "application/json", named)
			wrong := wrongField.FindStringSubmatch(said)
			if wrong == nil {
				break
			}
			delete(named, wrong[1])
		}
		// What it says back of what it was sent isn't a leak; anything else of theirs is.
		for _, sent := range named {
			said = strings.ReplaceAll(said, sent.(string), "")
		}
		// A part of one, quoted back by a form that reads several from one field.
		said = strings.ReplaceAll(said, `"`+theirs+`"`, "")
		if strings.Contains(said, theirs) {
			t.Errorf("POST %s with the first organisation's own values answered with more of them: %.200s", route, said)
		}
		unchanged("POST " + route + " with the first organisation's own values")
	}
	// None of that signed the visitor out, which would have made every answer after it a refusal.
	if code, body := call(t, intruder, "GET", srv.URL+"/o/beta/today", "", nil); code != http.StatusOK || !strings.Contains(body, "Today") {
		t.Fatalf("the second organisation's owner is no longer signed in: %d", code)
	}
}
