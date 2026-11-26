package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
)

func TestReactPagesMountInTheLayout(t *testing.T) {
	_, _, srv, client := signedIn(t)
	code, page := call(t, client, "GET", srv.URL+"/o/alpha/app/research?task=3", "", nil)
	if code != http.StatusOK {
		t.Fatalf("the React Research page: %d", code)
	}
	for _, want := range []string{`<div id="app" data-org="alpha" data-sales="true" data-support="true" data-no-refresh="true" data-owner="true"></div>`, `<script type="module" src="/assets/app/main.js?v=`, "Research</span>"} {
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
		if !strings.Contains(string(main), `path="`+p+`"`) && !strings.Contains(string(main), `path="`+p+`/`) {
			t.Errorf("%s is served as a React page but main.tsx doesn't route it", p)
		}
	}
}

func TestReactAddPerson(t *testing.T) {
	_, _, srv, client := signedIn(t)

	// In the layout, the sidebar's button sends people to it.
	_, inLayout := call(t, client, "GET", srv.URL+"/o/alpha/app/add?company=Larkmoor+Yachts", "", nil)
	if !strings.Contains(inLayout, `id="app"`) || !strings.Contains(inLayout, `aria-label="Main"`) || !strings.Contains(inLayout, `href="/o/alpha/app/add"`) {
		t.Fatal("the React add page should sit in the layout, with the sidebar's button sending people to it")
	}

	// The window the bookmarklet opens has the form alone, under the same policy and sign-in.
	res, err := client.Get(srv.URL + "/o/alpha/app/add?popup=1&name=Hannah")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	popup := string(body)
	if res.StatusCode != http.StatusOK || !strings.Contains(popup, `id="app" data-org="alpha"`) || !strings.Contains(popup, "<title>Add to Alpha</title>") {
		t.Fatalf("the add popup: %d %s", res.StatusCode, popup)
	}
	if strings.Contains(popup, `aria-label="Main"`) || strings.Contains(popup, `id="content"`) {
		t.Error("the add popup should come without the sidebar")
	}
	if res.Header.Get("Content-Security-Policy") != contentPolicy {
		t.Errorf("the add popup lost the content policy: %q", res.Header.Get("Content-Security-Policy"))
	}
	signedOut := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if code, _ := call(t, signedOut, "GET", srv.URL+"/o/alpha/app/add?popup=1&name=Hannah", "", nil); code == http.StatusOK {
		t.Fatal("the add popup was served to someone signed out")
	}

	// What a LinkedIn profile gives the form, as the templ page read it.
	q := url.Values{"url": {"https://www.linkedin.com/in/hannah-pryce?trk=nav"}, "name": {"(3) Hannah Pryce | LinkedIn"}, "headline": {"Head of Sales at Larkmoor Yachts | Sailing"}}
	var draft api.PersonDraft
	code, out := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/people/draft?"+q.Encode(), "", nil)
	if code != http.StatusOK || json.Unmarshal([]byte(out), &draft) != nil {
		t.Fatalf("drafting a person: %d %s", code, out)
	}
	if draft.Linkedin != "https://www.linkedin.com/in/hannah-pryce" || draft.Name != "Hannah Pryce" || draft.Title != "Head of Sales" || draft.Company != "Larkmoor Yachts" {
		t.Errorf("the profile read as %+v", draft)
	}
	if !strings.HasPrefix(draft.Bookmarklet, "javascript:") || !strings.Contains(draft.Bookmarklet, srv.URL+"/o/alpha/app/add?popup=1&url=") {
		t.Errorf("the bookmarklet should open the React popup: %s", draft.Bookmarklet)
	}
	q.Set("company", "Quay Marine")
	if _, out := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/people/draft?"+q.Encode(), "", nil); !strings.Contains(out, `"company":"Quay Marine"`) {
		t.Errorf("a company named in the address should win over the headline's: %s", out)
	}
}
