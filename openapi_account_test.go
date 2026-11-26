package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
)

// Until their handlers land, the account operations answer nothing but 501, to a
// signed-in browser, a stranger and a key alike.
func TestAccountOperationsAreNotBuiltYet(t *testing.T) {
	s, o, srv, browser := signedIn(t)
	secret, _, err := s.CreateKey(o.ID, "robot", []string{"read"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	spec := must(api.Spec())
	ops := 0
	for path, item := range spec.Paths.Map() {
		if !accountPath(path) {
			continue
		}
		for method := range item.Operations() {
			ops++
			url := srv.URL + strings.ReplaceAll(path, "{token}", "abc")
			body := map[string]any{"email": "ada@example.com", "password": "correct horse battery", "current": "x", "new": "y"}
			if code, text := call(t, browser, method, url, "application/json", body); code != http.StatusNotImplemented {
				t.Errorf("%s %s, signed in: %d %s", method, path, code, text)
			}
			if code, text := call(t, newMailClient(), method, url, "application/json", body); code != http.StatusNotImplemented {
				t.Errorf("%s %s, signed out: %d %s", method, path, code, text)
			}
			if code, text := asKey(t, method, url, secret, body); code != http.StatusNotImplemented {
				t.Errorf("%s %s, with a key: %d %s", method, path, code, text)
			}
		}
	}
	if ops != 9 {
		t.Fatalf("found %d account operations, want 9", ops)
	}
	if code, _ := call(t, browser, "GET", srv.URL+"/o/alpha/today", "", nil); code != http.StatusOK {
		t.Fatalf("the session the old sign-in made should still work: %d", code)
	}
}
