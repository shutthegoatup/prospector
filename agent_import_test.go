package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// What `prospector push` and `pull` send and get back: an import of the old tracker's
// drafts, and the organisation's contacts as text. They hold whichever way the routes
// are served.
func TestPushAndPullAnswerAsTheyAlwaysHave(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	importer, _, err := s.CreateKey(o.ID, "laptop", []string{"import"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	reader, _, err := s.CreateKey(o.ID, "reader", []string{"read"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	call := func(key, method, path, body string) (*http.Response, string) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+"/o/alpha/api/v1/"+path, strings.NewReader(body))
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		} else {
			// Without a key, the site's own rule wants JSON before any route is reached.
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res, string(b)
	}

	// The CLI sends the payload with no content type.
	payload := `{"drafts":[{"key":"larkmoor-hannah","file":"leads/outreach/yachts.md","section":"Larkmoor","fit":"A","maker":"Larkmoor Yachts","person":"Hannah Pryce","first":"Hannah","linkedin":"https://www.linkedin.com/in/hannah-pryce-example","email":"hannah@larkmoor.example","note":"Hello Hannah.","subject":"Your boats","body":"Hello.","followup":"Again.","flags":"","hold":null,"sources":"https://larkmoor.example/team","phone":"+44 20 7946 0000"}],"companies":[{"name":"Larkmoor Yachts","country":"GB"}],"sent_log":[{"date":"2026-01-05","line":"Hannah Pryce: LinkedIn"}]}`
	res, body := call(importer, "POST", "import", payload)
	if res.StatusCode != http.StatusOK || strings.TrimSpace(body) != `{"contacts":1}` {
		t.Fatalf("a push: %d %s", res.StatusCode, body)
	}
	people := must(s.FindOne(o.ID, "Hannah Pryce"))
	if deref(people.Email) != "hannah@larkmoor.example" || deref(people.Subject) != "Your boats" || deref(people.Phone) == "" {
		t.Errorf("a pushed draft didn't land whole: %+v", people)
	}

	res, body = call(importer, "GET", "snapshot", "")
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/plain") || !strings.Contains(body, "Hannah Pryce") {
		t.Fatalf("a pull: %d %q %.200s", res.StatusCode, res.Header.Get("Content-Type"), body)
	}

	if res, body := call(importer, "POST", "import", "{"); res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "bad request body") {
		t.Errorf("a push that isn't JSON: %d %s", res.StatusCode, body)
	}
	for _, route := range [][2]string{{"POST", "import"}, {"GET", "snapshot"}} {
		if res, _ := call("", route[0], route[1], "{}"); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s with no key: %d", route[0], route[1], res.StatusCode)
		}
		if res, body := call(reader, route[0], route[1], "{}"); res.StatusCode != http.StatusForbidden || !strings.Contains(body, "import") {
			t.Errorf("%s %s with a key lacking import: %d %s", route[0], route[1], res.StatusCode, body)
		}
		req, _ := http.NewRequest(route[0], srv.URL+"/o/alpha/api/v1/"+route[1], strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", srv.URL)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s signed in, with no key: %d", route[0], route[1], res.StatusCode)
		}
	}
}
