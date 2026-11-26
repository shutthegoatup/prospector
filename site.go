package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

// The public site's pages are React, rendered when the app is built (web/prerender.mjs)
// into assets/app: the front page and the /api reference. Search engines and readers
// without script get the whole page; the server adds only what it knows as it answers.

var assetRef = regexp.MustCompile(`((?:src|href)="/assets/[^"?]+)"`)

// builtPage reads a page Vite or prerender.mjs wrote, with the version on each of its
// files so a browser can keep them until the next release.
func builtPage(name string, edit func([]byte) []byte) func() ([]byte, error) {
	return sync.OnceValues(func() ([]byte, error) {
		b, err := assetFS.ReadFile("assets/app/" + name)
		if err != nil {
			return nil, err
		}
		if edit != nil {
			b = edit(b)
		}
		return assetRef.ReplaceAll(b, []byte(`$1?v=`+assetVersion+`"`)), nil
	})
}

// siteScript brings the front page's enquiry form, and nothing else.
const siteScript = `<script type="module" src="/assets/app/site.js"></script>`

var (
	frontWithForm = builtPage("site.html", nil)
	// Without a form the front page loads no script at all.
	frontPlain       = builtPage("site.html", func(b []byte) []byte { return bytes.Replace(b, []byte(siteScript), nil, 1) })
	apiReferencePage = builtPage("api.html", nil)
)

func (a *App) frontPage(w http.ResponseWriter, r *http.Request) {
	intake, origin := a.frontIntake(r)
	read := frontPlain
	if intake != "" {
		read = frontWithForm
	}
	b, err := read()
	if err == nil && intake != "" {
		var data []byte
		data, err = json.Marshal(map[string]string{"intake": intake, "origin": origin})
		// json.Marshal writes <, > and & as escapes, so quotes are all that could leave
		// the attribute.
		attr := strings.ReplaceAll(string(data), `"`, "&quot;")
		b = bytes.Replace(b, []byte(`<div id="site">`), []byte(`<div id="site" data-page="`+attr+`">`), 1)
	}
	if err != nil {
		log.Print(err)
		http.Error(w, "the front page couldn't be read", http.StatusInternalServerError)
		return
	}
	w.Header().Set("content-type", "text/html; charset=utf-8")
	w.Write(b)
}

// apiDocPage serves /api, with this server's address in its examples.
func (a *App) apiDocPage(w http.ResponseWriter, r *http.Request) {
	b, err := apiReferencePage()
	if err != nil {
		log.Print(err)
		http.Error(w, "the API reference couldn't be read", http.StatusInternalServerError)
		return
	}
	w.Header().Set("content-type", "text/html; charset=utf-8")
	w.Write(withBase(b, a.baseURL(r)))
}
