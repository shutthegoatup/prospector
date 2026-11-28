package app

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/shutthegoatup/prospector/api"

	"github.com/shutthegoatup/prospector/internal/apidoc"

	"github.com/shutthegoatup/prospector/internal/request"
)

// The public site's pages are React, rendered when the app is built (web/prerender.mjs)
// into assets/app: the front page and the /api reference. Search engines and readers
// without script get the whole page; the server adds only what it knows as it answers.

var assetRef = regexp.MustCompile(`((?:src|href)="/assets/[^"?]+)"`)

// hashedScript is a script Vite named for its content. It gets no version: its chunks
// import it by that bare name, and the same script under two addresses would run twice.
var hashedScript = regexp.MustCompile(`/assets/app/[^"]*-[A-Za-z0-9_-]{8}\.js"$`)

func versioned(b []byte) []byte {
	return assetRef.ReplaceAllFunc(b, func(ref []byte) []byte {
		if hashedScript.Match(ref) {
			return ref
		}
		return append(ref[:len(ref)-1:len(ref)-1], []byte(`?v=`+assetVersion+`"`)...)
	})
}

// builtPage reads a page Vite or prerender.mjs wrote, with the version on each of its
// files so a browser can keep them until the next release.
func builtPage(name string, edit func([]byte) []byte) func() ([]byte, error) {
	return sync.OnceValues(func() ([]byte, error) {
		b, err := assetFS.ReadFile("app/" + name)
		if err != nil {
			return nil, err
		}
		if edit != nil {
			b = edit(b)
		}
		return versioned(b), nil
	})
}

// siteScript brings the front page's enquiry form, and nothing else.
const siteScript = `<script type="module" src="/assets/app/site.js"></script>`

type builtFront struct{ withForm, plain func() ([]byte, error) }

// frontPages is the front page in each language prerender.mjs built it in.
var frontPages = func() map[string]builtFront {
	l := request.Locales
	pages := map[string]builtFront{}
	for _, lang := range slices.Concat([]string{l.Source}, l.Translated, l.Pseudo) {
		name := "site." + lang + ".html"
		pages[lang] = builtFront{
			withForm: builtPage(name, nil),
			// Without a form the front page loads no script at all.
			plain: builtPage(name, func(b []byte) []byte { return bytes.Replace(b, []byte(siteScript), nil, 1) }),
		}
	}
	return pages
}()

var apiReferencePage = builtPage("api.html", nil)

func (a *App) frontPage(w http.ResponseWriter, r *http.Request) {
	lang := a.pickLanguage(w, r)
	intake, origin := a.frontIntake(r)
	read := func(p builtFront) ([]byte, error) {
		if intake != "" {
			return p.withForm()
		}
		return p.plain()
	}
	b, err := read(frontPages[lang])
	if err != nil && lang != request.Locales.Source {
		log.Print(err)
		lang = request.Locales.Source
		b, err = read(frontPages[lang])
	}
	w.Header().Set("Content-Language", lang)
	w.Header().Add("Vary", "Accept-Language, Cookie")
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
	_, _ = w.Write(apidoc.WithBase(b, a.baseURL(r)))
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
	_, _ = w.Write(apidoc.WithBase(b, a.baseURL(r)))
}

// apiProblem sends a problem type's link to its entry on the reference page, so the
// type a client was given leads somewhere a person can read.
func (a *App) apiProblem() http.HandlerFunc {
	spec, err := api.Spec()
	if err != nil {
		log.Fatalf("the API spec: %v", err)
	}
	known := map[string]bool{}
	for _, p := range apidoc.ProblemsOf(spec) {
		if p.Type != string(api.ProblemTypeUntyped) {
			known[p.Name] = true
		}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !known[name] {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/api#"+apidoc.ProblemAnchor(name), http.StatusFound)
	}
}
