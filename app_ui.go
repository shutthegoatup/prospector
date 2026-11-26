package main

import (
	"net/http"
	"slices"
	"strings"
)

// reactPages are the pages the React app (web/src/main.tsx) has; each lights up its own
// sidebar entry. A page moved to React is listed here and routed in main.tsx.
var reactPages = []string{"research", "campaigns", "onboarding", "tasks"}

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
	active := page
	if under, ok := reactUnder[page]; ok {
		active = under
	}
	a.page(w, r, active, ReactMount(reqOf(r)))
}

// navPath is where the sidebar sends a page: its React version once it has one.
func navPath(key string) string {
	if slices.Contains(reactPages, key) {
		return "app/" + key
	}
	return key
}
