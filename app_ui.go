package main

import (
	"log"
	"net/http"
	"slices"
	"strings"
)

// reactPages are the pages the React app (web/src/main.tsx) has; each lights up its own
// sidebar entry. A page moved to React is listed here and routed in main.tsx.
var reactPages = []string{"research", "campaigns", "onboarding", "enquiries", "subscriptions", "tasks", "add", "agents", "conversations", "companies", "settings"}

// reactUnder are React pages without a sidebar entry of their own, and the entry they light.
var reactUnder = map[string]string{"tasks": "research"}

// reactPage serves a React page inside the usual layout: the server keeps the sidebar
// while pages move over one at a time, and the page reads everything through the API.
func (a *App) reactPage(w http.ResponseWriter, r *http.Request) {
	page, _, _ := strings.Cut(r.PathValue("page"), "/")
	if !slices.Contains(reactPages, page) {
		http.NotFound(w, r)
		return
	}
	q := reqOf(r)
	// The bookmarklet opens the add form in a small window of its own.
	if page == "add" && r.URL.Query().Get("popup") != "" {
		w.Header().Set("content-type", "text/html; charset=utf-8")
		if err := ReactPopup(q, "Add to "+q.Org.Name).Render(r.Context(), w); err != nil {
			log.Print(err)
		}
		return
	}
	active := page
	if under, ok := reactUnder[page]; ok {
		active = under
	}
	a.page(w, r, active, ReactMount(q))
}

// navPath is where the sidebar sends a page: its React version once it has one.
func navPath(key string) string {
	if slices.Contains(reactPages, key) {
		return "app/" + key
	}
	return key
}
