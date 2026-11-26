package main

import (
	"net/http"
	"regexp"
	"sync"
)

// appPages are the pages the React app (web/src/main.tsx) draws. Each is served the
// same shell, at its own address and below it, and the app routes from there.
var appPages = []string{"today", "research", "conversations", "enquiries", "onboarding", "subscriptions", "campaigns", "tasks", "companies", "settings", "agents", "add"}

func (a *App) appRoutes(mux *http.ServeMux) {
	for _, p := range appPages {
		mux.HandleFunc("GET /o/{org}/"+p, a.member(a.shell))
	}
	mux.HandleFunc("GET /o/{org}/companies/{id}", a.member(a.shell))
	mux.HandleFunc("GET /o/{org}/tasks/{id}", a.member(a.shell))
	// Where the React pages lived while the sidebar was still the server's; bookmarklets
	// already dragged to a bar still open the add form there.
	mux.HandleFunc("GET /o/{org}/app/{page...}", a.member(func(w http.ResponseWriter, r *http.Request) {
		to := reqOf(r).Base() + r.PathValue("page")
		if r.URL.RawQuery != "" {
			to += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, to, http.StatusFound)
	}))
}

// shellHTML is the page Vite builds from web/index.html, with the version on each of
// the app's own files so a browser can keep them until the next release.
var shellHTML = sync.OnceValues(func() ([]byte, error) {
	b, err := assetFS.ReadFile("assets/app/index.html")
	if err != nil {
		return nil, err
	}
	return regexp.MustCompile(`((?:src|href)="/assets/[^"?]+)"`).ReplaceAll(b, []byte(`$1?v=`+assetVersion+`"`)), nil
})

// shell serves the React app: everything it shows, it reads through the API.
func (a *App) shell(w http.ResponseWriter, r *http.Request) {
	b, err := shellHTML()
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("content-type", "text/html; charset=utf-8")
	w.Write(b)
}
