package main

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestReactPagesMountInTheLayout(t *testing.T) {
	_, _, srv, client := signedIn(t)
	code, page := call(t, client, "GET", srv.URL+"/o/alpha/app/research?task=3", "", nil)
	if code != http.StatusOK {
		t.Fatalf("the React Research page: %d", code)
	}
	for _, want := range []string{`<div id="app" data-org="alpha" data-sales="true" data-support="true" data-owner="true" data-no-refresh="true"></div>`, `<script type="module" src="/assets/app/main.js?v=`, "Research</span>"} {
		if !strings.Contains(page, want) {
			t.Errorf("the React page is missing %q", want)
		}
	}
	if code, _ := call(t, client, "GET", srv.URL+"/o/alpha/app/nowhere", "", nil); code != http.StatusNotFound {
		t.Fatalf("a page React doesn't have: %d", code)
	}
	if code, _ := call(t, client, "GET", srv.URL+"/assets/app/main.js", "", nil); code != http.StatusOK {
		t.Fatalf("the React bundle isn't served: %d", code)
	}
	signedOut := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if code, _ := call(t, signedOut, "GET", srv.URL+"/o/alpha/app/research", "", nil); code == http.StatusOK {
		t.Fatal("a React page was served to someone signed out")
	}

	// The sidebar sends each page React has to it.
	_, today := call(t, client, "GET", srv.URL+"/o/alpha/today", "", nil)
	for _, p := range reactPages {
		if _, under := reactUnder[p]; under {
			continue
		}
		if !strings.Contains(today, `href="/o/alpha/app/`+p+`"`) {
			t.Errorf("the sidebar doesn't send %s to its React page", p)
		}
	}
	if !strings.Contains(today, `href="/o/alpha/today"`) {
		t.Error("a page still on the server lost its sidebar link")
	}
	// A task's page lights Research, as the server's tasks pages do.
	_, task := call(t, client, "GET", srv.URL+"/o/alpha/app/tasks/7", "", nil)
	if !strings.Contains(task, `href="/o/alpha/app/research" class="flex items-center justify-between rounded-lg bg-primary`) {
		t.Error("the React task page should light Research in the sidebar")
	}

	// A campaign that can't be sent says why, for the page to show.
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/campaigns", "", nil); !strings.Contains(body, `"not_ready":"`) || strings.Contains(body, `"not_ready":""`) {
		t.Errorf("campaigns should say why nothing can be sent yet: %s", body)
	}

	// Every page React has is routed in main.tsx too.
	main, err := os.ReadFile("web/src/main.tsx")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range reactPages {
		if !strings.Contains(string(main), `path="`+p+`"`) {
			t.Errorf("%s is served as a React page but main.tsx doesn't route it", p)
		}
	}
}
