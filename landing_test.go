package main

import (
	"net/http"
	"path"
	"regexp"
	"strings"
	"testing"
)

func TestFrontPage(t *testing.T) {
	_, _, srv, member := signedIn(t)
	visitor := &http.Client{}

	code, body := call(t, visitor, "GET", srv.URL+"/", "", nil)
	if code != http.StatusOK || !strings.Contains(body, "One queue a day.") || !strings.Contains(body, `href="/login"`) {
		t.Fatalf("a signed-out visitor at /: %d, without the front page and its sign-in link", code)
	}
	for _, private := range []string{"alpha", "Ada Lovelace", "ada@example.com"} {
		if strings.Contains(body, private) {
			t.Errorf("the front page shows %q from this server's data", private)
		}
	}
	if found := regexp.MustCompile(`(?i)<script|<[^>]*\son[a-z]+\s*=`).FindString(body); found != "" {
		t.Errorf("the front page has a script or an inline handler: %s", found)
	}

	// Everything the page loads must be in the binary and served as what it is: the
	// release image has no system table of file types to fall back on.
	types := map[string]string{".css": "text/css", ".svg": "image/svg+xml", ".webp": "image/webp"}
	assets := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`).FindAllStringSubmatch(body, -1)
	if len(assets) < 8 {
		t.Fatalf("the front page loads only %d assets", len(assets))
	}
	for _, m := range assets {
		res, err := visitor.Get(srv.URL + m[1])
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		want := types[path.Ext(strings.SplitN(m[1], "?", 2)[0])]
		if res.StatusCode != http.StatusOK || want == "" || !strings.HasPrefix(res.Header.Get("Content-Type"), want) {
			t.Errorf("GET %s: %d %q, want %q", m[1], res.StatusCode, res.Header.Get("Content-Type"), want)
		}
		if !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
			t.Errorf("GET %s isn't cached for good: %q", m[1], res.Header.Get("Cache-Control"))
		}
	}

	// Someone signed in never sees it: / still takes them to their queue.
	code, body = call(t, member, "GET", srv.URL+"/", "", nil)
	if code != http.StatusOK || strings.Contains(body, "One queue a day.") || !strings.Contains(body, `data-base="/o/alpha/"`) {
		t.Fatalf("a signed-in member at / didn't land on their organisation: %d", code)
	}
}

// The app shares the front page's light palette: a page left on the old dark class
// would turn the components' dark variants back on.
func TestAppUsesTheLightPalette(t *testing.T) {
	_, _, srv, member := signedIn(t)
	for _, page := range []string{"/o/alpha/today", "/o/alpha/settings"} {
		_, body := call(t, member, "GET", srv.URL+page, "", nil)
		if strings.Contains(body, `class="dark"`) || !strings.Contains(body, `<meta name="color-scheme" content="light"`) {
			t.Errorf("%s isn't on the light palette", page)
		}
		if !strings.Contains(body, `<img src="/assets/img/mark.svg`) {
			t.Errorf("%s doesn't show the logo", page)
		}
	}
}
