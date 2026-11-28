package app

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
)

func TestTheAppSpeaksTheBestOfTheBrowsersLanguagesItHas(t *testing.T) {
	t.Parallel()
	set := request.LocaleSet{Source: "en-GB", Translated: []string{"de", "fr", "pt"}, Pseudo: []string{"en-XA"}}
	for _, c := range []struct{ chosen, accept, fallback, want string }{
		{"", "", "", "en-GB"},
		{"", "de-CH,de;q=0.9,en;q=0.5", "", "de"},
		{"", "pt-BR", "", "pt"},
		{"", "ja,ko;q=0.8", "", "en-GB"},
		{"", "en-US,fr;q=0.5", "", "en-GB"},
		{"", "en-XA", "", "en-GB"},
		{"", "not a header;;", "", "en-GB"},
		{"fr", "de", "", "fr"},
		{"en-XA", "de", "", "en-XA"},
		{"xx", "de", "", "de"},
		// An organisation's language stands in only where the browser asks for none it has.
		{"", "ja,ko;q=0.8", "fr", "fr"},
		{"", "", "fr", "fr"},
		{"", "de", "fr", "de"},
		{"pt", "de", "fr", "pt"},
		{"", "ja", "xx", "en-GB"},
	} {
		if got := set.Choose(c.chosen, c.accept, c.fallback); got != c.want {
			t.Errorf("chosen %q, Accept-Language %q, fallback %q: %s, want %s", c.chosen, c.accept, c.fallback, got, c.want)
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
	if got := lang("/o/alpha/today"); got != request.Locales.Source {
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
	if got := lang(`/o/alpha/today?lang="><script>`); got != request.Locales.Source {
		t.Errorf("one it doesn't have: %s", got)
	}
	if got := lang("/o/alpha/today"); got != request.Locales.Source {
		t.Errorf("after an unknown one forgets the pick: %s", got)
	}
}

func TestALanguageIsPickedOnlyByName(t *testing.T) {
	t.Parallel()
	_, _, srv, _ := signedIn(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/login", nil)
	req.Header.Set("Accept-Language", strings.Join(request.Locales.Pseudo, ","))
	res, err := noRedirects().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if body := string(b); res.StatusCode != http.StatusOK || !strings.Contains(body, `<html lang="`+request.Locales.Source+`">`) {
		t.Errorf("a browser asking for a pseudo-locale gets it: %d %s", res.StatusCode, body)
	}
	if len(res.Cookies()) != 0 {
		t.Errorf("nothing was picked, yet %v", res.Cookies())
	}
}

// A Problem is in the language the pages are, and says so (RFC 9457, RFC 9110 §8.5).
func TestAProblemIsInTheLanguageThePagesAre(t *testing.T) {
	t.Parallel()
	_, _, srv, client := signedIn(t)
	problem := func() (*http.Response, string) {
		t.Helper()
		res, body := get(t, client, srv.URL+"/o/alpha/api/v1/companies/9999999/overview")
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%d %s", res.StatusCode, body)
		}
		return res, body
	}
	res, body := problem()
	if !strings.Contains(body, `"detail":"no company 9999999"`) || res.Header.Get("Content-Language") != request.Locales.Source {
		t.Errorf("before any is picked: %s %s", res.Header.Get("Content-Language"), body)
	}
	if vary := res.Header.Values("Vary"); !strings.Contains(strings.Join(vary, ","), "Accept-Language") || !strings.Contains(strings.Join(vary, ","), "Cookie") {
		t.Errorf("a cache could hand this to someone reading another language: Vary %q", vary)
	}
	get(t, client, srv.URL+"/o/alpha/today?lang=en-XA")
	res, body = problem()
	if !strings.Contains(body, `"detail":"[ñö çöɱþáñý 9999999]"`) || res.Header.Get("Content-Language") != "en-XA" {
		t.Errorf("picked: %s %s", res.Header.Get("Content-Language"), body)
	}
}

// A page the server makes of a browser's failed request is in the reader's language too:
// its title and what the handler said.
func TestAProblemPageIsInTheReadersLanguage(t *testing.T) {
	t.Parallel()
	_, _, srv, _ := signedIn(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/reset/not-a-token", nil)
	req.Header.Set("Accept", "text/html")
	req.AddCookie(&http.Cookie{Name: request.LangCookie, Value: "en-XA"})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	page := served(t, string(b))
	if res.StatusCode != http.StatusGone || page.Title != say.New("That didn't work").In("en-XA") || page.Message != "[ţĥîš ļîñķ ĥáš éẋþîŕéð öŕ ĥáš ƀééñ ûšéð: ášķ ƒöŕ á ñéŵ öñé]." {
		t.Errorf("a link that has gone: %d %q %q", res.StatusCode, page.Title, page.Message)
	}
}
