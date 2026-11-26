package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

func getText(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, string(body)
}

func TestWellKnownFilesAreServedSignedOut(t *testing.T) {
	srv := httptest.NewServer((&App{s: tempStore(t), securityContact: "mailto:security@prospector.example"}).Routes())
	t.Cleanup(srv.Close)
	for path, kind := range map[string]string{
		"/robots.txt":               "text/plain; charset=utf-8",
		"/llms.txt":                 "text/plain; charset=utf-8",
		"/sitemap.xml":              "application/xml; charset=utf-8",
		"/.well-known/security.txt": "text/plain; charset=utf-8",
	} {
		res, body := getText(t, srv.URL+path)
		if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != kind || body == "" {
			t.Errorf("%s: %d %q, %d bytes", path, res.StatusCode, res.Header.Get("Content-Type"), len(body))
		}
		if cc := res.Header.Get("Cache-Control"); !strings.HasPrefix(cc, "public") {
			t.Errorf("%s: cache-control %q", path, cc)
		}
	}
}

func TestRobotsKeepsPublicPagesOpen(t *testing.T) {
	srv := httptest.NewServer((&App{s: tempStore(t), base: "https://prospector.example"}).Routes())
	t.Cleanup(srv.Close)
	_, body := getText(t, srv.URL+"/robots.txt")
	if !strings.Contains(body, "Disallow: /o/\n") {
		t.Errorf("members' pages aren't kept from crawlers:\n%s", body)
	}
	// /c/ and /u/ say noindex on the page itself, which a barred crawler never reads.
	for _, open := range []string{"/", "/api", "/llms.txt", "/c/", "/u/"} {
		for _, line := range strings.Split(body, "\n") {
			if rule, ok := strings.CutPrefix(line, "Disallow: "); ok && rule != "" && strings.HasPrefix(open, rule) {
				t.Errorf("%s is disallowed by %q", open, line)
			}
		}
	}
	if !strings.Contains(body, "Sitemap: https://prospector.example/sitemap.xml\n") {
		t.Errorf("the sitemap isn't named from PROSPECTOR_URL:\n%s", body)
	}
	_, sitemap := getText(t, srv.URL+"/sitemap.xml")
	if !strings.Contains(sitemap, "<loc>https://prospector.example/api</loc>") {
		t.Errorf("sitemap:\n%s", sitemap)
	}
}

func TestLLMsTxtLinksWork(t *testing.T) {
	srv := httptest.NewServer((&App{s: tempStore(t)}).Routes())
	t.Cleanup(srv.Close)
	_, body := getText(t, srv.URL+"/llms.txt")
	if !strings.HasPrefix(body, "# Prospector\n\n> ") {
		t.Fatalf("not llms.txt's title and summary:\n%.200s", body)
	}
	if !strings.Contains(body, "/o/{org}/mcp") || !strings.Contains(body, "## Optional\n") {
		t.Errorf("no MCP address or Optional section:\n%s", body)
	}
	_, apiPage := getText(t, srv.URL+"/api")
	links := regexp.MustCompile(`\]\((`+regexp.QuoteMeta(srv.URL)+`[^)]*)\)`).FindAllStringSubmatch(body, -1)
	if len(links) < 10 {
		t.Fatalf("only %d links to this server", len(links))
	}
	for _, l := range links {
		page, anchor, _ := strings.Cut(l[1], "#")
		if anchor != "" {
			if !strings.Contains(apiPage, `id="`+anchor+`"`) {
				t.Errorf("%s: /api has no such anchor", l[1])
			}
			continue
		}
		if res, _ := getText(t, page); res.StatusCode != http.StatusOK {
			t.Errorf("%s: %d", page, res.StatusCode)
		}
	}
}

func TestSecurityTxt(t *testing.T) {
	s := tempStore(t)
	none := httptest.NewServer((&App{s: s}).Routes())
	t.Cleanup(none.Close)
	if res, _ := getText(t, none.URL+"/.well-known/security.txt"); res.StatusCode != http.StatusNotFound {
		t.Errorf("served with no contact to give: %d", res.StatusCode)
	}

	srv := httptest.NewServer((&App{s: s, base: "https://prospector.example", securityContact: "https://prospector.example/security"}).Routes())
	t.Cleanup(srv.Close)
	_, body := getText(t, srv.URL+"/.well-known/security.txt")
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		k, v, _ := strings.Cut(line, ": ")
		fields[k] = v
	}
	if fields["Contact"] != "https://prospector.example/security" || fields["Canonical"] != "https://prospector.example/.well-known/security.txt" {
		t.Errorf("security.txt:\n%s", body)
	}
	expires, err := time.Parse(time.RFC3339, fields["Expires"])
	if err != nil || !expires.After(time.Now()) || expires.After(time.Now().AddDate(1, 0, 0)) {
		t.Errorf("Expires %q should be in the coming year (%v)", fields["Expires"], err)
	}
}

func TestSecurityContactFrom(t *testing.T) {
	for raw, ok := range map[string]bool{
		"":                                   true,
		"mailto:security@prospector.example": true,
		"https://prospector.example/report":  true,
		"tel:+44-20-7946-0000":               true,
		"security@prospector.example":        false,
		"http://prospector.example/report":   false,
		"mailto:":                            false,
		"https://":                           false,
	} {
		if _, err := securityContactFrom(raw); (err == nil) != ok {
			t.Errorf("%q: %v", raw, err)
		}
	}
}
