package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/shutthegoatup/prospector/internal/request"
)

// appPages are the pages the React app (web/src/router.tsx) draws. Each is served the
// same shell at its own address, and the app routes from there.
var appPages = []string{"today", "research", "conversations", "enquiries", "onboarding", "subscriptions", "campaigns", "tasks", "companies", "settings", "agents", "add"}

func (a *App) appRoutes(mux *http.ServeMux) {
	for _, p := range appPages {
		mux.HandleFunc("GET /o/{org}/"+p, a.member(a.shell))
	}
	mux.HandleFunc("GET /o/{org}/companies/{id}", a.member(a.shell))
	mux.HandleFunc("GET /o/{org}/tasks/{id}", a.member(a.shell))
	// An agreement's PDF is drawn with pdf.js, which needs WebAssembly for some images.
	mux.HandleFunc("GET /o/{org}/agreements/{id}/prepare", a.member(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", pdfPolicy)
		a.shell(w, r)
	}))
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

// shellHTML is the page Vite builds from web/index.html.
var shellHTML = builtPage("index.html", nil)

// shellIn is the shell saying it is in lang, which the app reads to load that
// language's messages before it draws anything.
func shellIn(lang string) ([]byte, error) {
	b, err := shellHTML()
	if err != nil {
		return nil, err
	}
	return bytes.Replace(b, []byte(`<html lang="`+request.Locales.Source+`">`), []byte(`<html lang="`+lang+`">`), 1), nil
}

// writeShell answers with the app's shell carrying page: what the server already decided
// a page shows, for the pages with no API to ask (an error, the start of a first
// organisation, a customer's or an unsubscriber's), in lang. Its "page" field names which, and
// the app draws it in place of whatever the address would route to. With no shell to
// read, the answer is fallback as plain text.
func writeShell(w http.ResponseWriter, lang string, code int, page any, fallback string) {
	b, err := shellIn(lang)
	var data []byte
	if err == nil {
		data, err = json.Marshal(page)
	}
	if err != nil {
		w.Header().Set("content-type", "text/plain; charset=utf-8")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(fallback))
		return
	}
	// json.Marshal already writes <, > and & as escapes, so quotes are all that could
	// leave the attribute.
	attr := strings.ReplaceAll(string(data), `"`, "&quot;")
	b = bytes.Replace(b, []byte(`<div id="app"></div>`), []byte(`<div id="app" data-page="`+attr+`"></div>`), 1)
	w.Header().Set("content-type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(b)
}

// shell serves the React app: everything it shows, it reads through the API.
func (a *App) shell(w http.ResponseWriter, r *http.Request) {
	b, err := shellIn(a.pickLanguage(w, r))
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("content-type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}
