package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/shutthegoatup/prospector/api"
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

var pathParam = regexp.MustCompile(`\{(?:[a-z_]+)\}`)

// orgRoutes reads every read and every change of an organisation's out of the API's
// spec, so an operation added later is tried without anyone remembering to list it. The
// pages themselves are one shell that shows nothing until it reads the API. A change
// is its method and path.
func orgRoutes(t *testing.T) (gets, posts []string) {
	t.Helper()
	spec, err := api.Spec()
	if err != nil {
		t.Fatal(err)
	}
	for path, item := range spec.Paths.Map() {
		if !strings.HasPrefix(path, "/o/{org}/") {
			continue
		}
		path = strings.Replace(pathParam.ReplaceAllString(path, "{id}"), "/o/{id}/", "/o/{org}/", 1)
		if item.Get != nil {
			gets = append(gets, path)
		}
		for method, op := range map[string]*openapi3.Operation{"POST": item.Post, "PUT": item.Put, "PATCH": item.Patch, "DELETE": item.Delete} {
			if op != nil {
				posts = append(posts, method+" "+path)
			}
		}
	}
	slices.Sort(posts)
	return gets, posts
}

// bodyFields is every field a JSON body can carry anywhere in the package, and
// whether it is read as a number, as text or as a yes-or-no. A name read both as a
// number and as text is in both.
func bodyFields(t *testing.T) (numbers, texts, flags, lists map[string]bool) {
	t.Helper()
	numbers, texts, flags, lists = map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	fset := token.NewFileSet()
	for _, name := range append(sourceFiles(t), "api/api.gen.go") {
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
				default:
					// The API's own kinds of text, such as a role or a step.
					if id.Obj != nil {
						if spec, ok := id.Obj.Decl.(*ast.TypeSpec); ok {
							if under, ok := spec.Type.(*ast.Ident); ok && under.Name == "string" {
								texts[tag[1]] = true
							}
						}
					}
				}
			}
			return true
		})
	}
	return numbers, texts, flags, lists
}

// formWords are tryWords and the choices a form checks before it looks anything up.
var formWords = append([]string{"new", "pursued", "renewal", "month", "year", "openai", "anthropic", "read", "owner", "sales", "hunter", "https://intruder.example"}, tryWords...)

// stopsShort are the changes that never answer yes to these bodies, and why: each is
// refused for what it was sent before or after it looked for a row, which the test
// can't tell apart.
var stopsShort = map[string]string{
	"PATCH /o/{org}/api/v1/contacts/1/deal":        "takes only a deal's own fields, and every field is sent",
	"PATCH /o/{org}/api/v1/settings":               "every text field is sent, and a site address is one",
	"POST /o/{org}/api/v1/agents":                  "the same word is never both a kind of agent and its address",
	"POST /o/{org}/api/v1/agreements/1/recipients": "names a contact or a member, and both are sent",
	"POST /o/{org}/api/v1/campaigns":               "the same word is never both a kind and an audience",
	"POST /o/{org}/api/v1/campaigns/1/resume":      "needs a mail server to send through",
	"POST /o/{org}/api/v1/campaigns/1/send":        "needs a mail server to send through",
	"POST /o/{org}/api/v1/enquiries/1/replies":     "needs a mail server to send through",
	"POST /o/{org}/api/v1/onboardings/1/invites":   "needs a mail server to send through",
	"POST /o/{org}/api/v1/mail-server/test":        "needs a mail server to send through",
	"POST /o/{org}/api/v1/me/password":             "needs the current password",
	"POST /o/{org}/api/v1/subscriptions":           "a price's period is a word no other field takes",
	"PUT /o/{org}/api/v1/agreements/1/document":    "takes a PDF, not JSON",
	"PUT /o/{org}/api/v1/agreements/1/fields":      "takes a list, not an object",
}

// problemDetail is what an API refusal says, or the answer as it came when it isn't one.
func problemDetail(said string) string {
	var p struct{ Detail string }
	if json.Unmarshal([]byte(said), &p) == nil && p.Detail != "" {
		return p.Detail
	}
	return said
}

// looked matches a refusal that says the form went looking for a row and found none
// of this organisation's.
var looked = regexp.MustCompile(`^no \S+ |not found|isn't in this organisation|isn't listed|isn't going`)

func signInAs(t *testing.T, srv *httptest.Server, email, password string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	res, err := signInWith(client, srv.URL, email, password)
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
	if len(gets) < 10 || len(posts) < 19 {
		t.Fatalf("found only %d reads and %d changes in the API spec: has the way it is read changed?", len(gets), len(posts))
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
			"?step=linkedin", "?segment=" + theirs, "?state=waiting", "?kind=other", "?page=2",
			"?filter=review", "?filter=details", "?filter=rejected", "?closed=true", "?spam=true", "?stage=research", "?task=1", "?company=1",
			"?url=https://www.linkedin.com/in/" + findable + "&name=" + findable} {
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
	if shown < 20 {
		t.Fatalf("only %d of %d pages show the first organisation its own rows", shown, len(gets))
	}
	unchanged("reading the pages")

	numbers, texts, flags, lists := bodyFields(t)
	for _, route := range posts {
		method, route, _ := strings.Cut(route, " ")
		route = strings.ReplaceAll(route, "{id}", "1")
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
						code, said = call(t, intruder, method, srv.URL+strings.Replace(route, "{org}", "beta", 1), "application/json", body)
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
						t.Errorf("%s %s as another organisation's owner answered with the first one's rows: %.200s", method, route, said)
					}
					answers[fmt.Sprintf("%d %.80s", code, strings.TrimSpace(problemDetail(said)))] = true
					// A yes, or a no that names the row it looked for, shows the form got as far as looking.
					if code < 300 || looked.MatchString(problemDetail(said)) {
						heard = true
					}
				}
			}
		}
		if !heard && stopsShort[method+" "+route] == "" {
			t.Errorf("%s %s was never answered with a yes, or a no naming the row it looked for, so nothing was learnt about it: "+
				"give formWords what it checks for, or name it in stopsShort with why. It answered: %v", method, route, slices.Sorted(maps.Keys(answers)))
		}
		if heard && stopsShort[method+" "+route] != "" {
			t.Errorf("%s %s is in stopsShort and did answer: take it out", method, route)
		}
		unchanged(method + " " + route)
		if code, _ := call(t, intruder, method, srv.URL+strings.Replace(route, "{org}", "alpha", 1), "application/json", map[string]any{"id": 1}); code != http.StatusNotFound {
			t.Errorf("%s %s in the first organisation, by someone who isn't in it: %d", method, route, code)
		}
	}
	unchanged("every form")

	for _, route := range posts {
		method, route, _ := strings.Cut(route, " ")
		route = strings.ReplaceAll(route, "{id}", "1")
		// Once more with the first organisation's own values, as its rows hold them, for
		// a form that finds a row by its text. The answer may say them back, so only what
		// changed is looked at.
		named := map[string]any{}
		for name := range texts {
			named[name] = theirs + " " + name
		}
		said := ""
		for range 8 {
			_, said = call(t, intruder, method, srv.URL+strings.Replace(route, "{org}", "beta", 1), "application/json", named)
			wrong := wrongField.FindStringSubmatch(said)
			if wrong == nil {
				break
			}
			delete(named, wrong[1])
		}
		// What it says back of what it was sent isn't a leak; anything else of theirs is.
		for _, sent := range named {
			// A look-for list says back each line's name as the field it reads it into.
			said = strings.ReplaceAll(strings.ReplaceAll(said, sent.(string), ""), assetKey(sent.(string)), "")
		}
		// A part of one, quoted back by a form that reads several from one field.
		said = strings.ReplaceAll(strings.ReplaceAll(said, `"`+theirs+`"`, ""), `\"`+theirs+`\"`, "")
		if strings.Contains(said, theirs) {
			t.Errorf("%s %s with the first organisation's own values answered with more of them: %.200s", method, route, said)
		}
		unchanged(method + " " + route + " with the first organisation's own values")
	}
	// None of that signed the visitor out, which would have made every answer after it a refusal.
	if code, body := call(t, intruder, "GET", srv.URL+"/o/beta/api/v1/nav", "", nil); code != http.StatusOK || !strings.Contains(body, `"slug":"beta"`) {
		t.Fatalf("the second organisation's owner is no longer signed in: %d", code)
	}
}
