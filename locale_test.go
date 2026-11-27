package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestTheAppSpeaksTheBestOfTheBrowsersLanguagesItHas(t *testing.T) {
	t.Parallel()
	set := localeSet{Source: "en-GB", Translated: []string{"de", "fr", "pt"}, Pseudo: []string{"en-XA"}}
	for _, c := range []struct{ chosen, accept, want string }{
		{"", "", "en-GB"},
		{"", "de-CH,de;q=0.9,en;q=0.5", "de"},
		{"", "pt-BR", "pt"},
		{"", "ja,ko;q=0.8", "en-GB"},
		{"", "en-US,fr;q=0.5", "en-GB"},
		{"", "en-XA", "en-GB"},
		{"", "not a header;;", "en-GB"},
		{"fr", "de", "fr"},
		{"en-XA", "de", "en-XA"},
		{"xx", "de", "de"},
	} {
		if got := set.choose(c.chosen, c.accept); got != c.want {
			t.Errorf("chosen %q, Accept-Language %q: %s, want %s", c.chosen, c.accept, got, c.want)
		}
	}
}

func TestTheShellSaysWhichLanguageToDrawIn(t *testing.T) {
	t.Parallel()
	_, _, srv, client := signedIn(t)
	lang := func(path string) string {
		t.Helper()
		_, body := get(t, client, srv.URL+path)
		_, rest, ok := strings.Cut(body, "<html lang=\"")
		if !ok {
			t.Fatalf("%s has no language: %s", path, body)
		}
		return rest[:strings.IndexByte(rest, '"')]
	}
	if got := lang("/o/alpha/today"); got != locales.Source {
		t.Errorf("before any is picked: %s", got)
	}
	if got := lang("/o/alpha/today?lang=en-XA"); got != "en-XA" {
		t.Errorf("picked: %s", got)
	}
	// Kept for the next page, and for the ones the server decides, such as a customer's.
	if got := lang("/o/alpha/companies"); got != "en-XA" {
		t.Errorf("after picking: %s", got)
	}
	if got := lang("/c"); got != "en-XA" {
		t.Errorf("a customer's page: %s", got)
	}
	if got := lang(`/o/alpha/today?lang="><script>`); got != locales.Source {
		t.Errorf("one it doesn't have: %s", got)
	}
	if got := lang("/o/alpha/today"); got != locales.Source {
		t.Errorf("after an unknown one forgets the pick: %s", got)
	}
}

func TestALanguageIsPickedOnlyByName(t *testing.T) {
	t.Parallel()
	_, _, srv, _ := signedIn(t)
	req, _ := http.NewRequest("GET", srv.URL+"/login", nil)
	req.Header.Set("Accept-Language", strings.Join(locales.Pseudo, ","))
	res, err := noRedirects().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if body := string(b); res.StatusCode != http.StatusOK || !strings.Contains(body, `<html lang="`+locales.Source+`">`) {
		t.Errorf("a browser asking for a pseudo-locale gets it: %d %s", res.StatusCode, body)
	}
	if len(res.Cookies()) != 0 {
		t.Errorf("nothing was picked, yet %v", res.Cookies())
	}
}
