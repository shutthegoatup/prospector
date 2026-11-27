package main

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"slices"

	"golang.org/x/text/language"

	"github.com/shutthegoatup/prospector/internal/say"
)

// localesJSON is the one list of the languages Prospector speaks, which the web build
// compiles catalogues for.
//
//go:embed web/locales.json
var localesJSON []byte

// localeSet is what web/locales.json says: the language the app is written in, those
// it has been translated into, and pseudo-locales, which stretch and accent the source
// so that text left out of the catalogues stands out. A pseudo-locale is only ever
// chosen by name, never matched from a browser's languages.
type localeSet struct {
	Source     string   `json:"source"`
	Translated []string `json:"translated"`
	Pseudo     []string `json:"pseudo"`
}

var locales = func() localeSet {
	var l localeSet
	if err := json.Unmarshal(localesJSON, &l); err != nil || l.Source == "" {
		panic("web/locales.json doesn't say what language the app is written in")
	}
	for _, p := range l.Pseudo {
		say.Pseudo(p)
	}
	return l
}()

func (l localeSet) offers(lang string) bool {
	return lang == l.Source || slices.Contains(l.Translated, lang) || slices.Contains(l.Pseudo, lang)
}

// choose is the language to speak: one chosen by name if it is offered, otherwise the
// best of the browser's that the app has been translated into, otherwise the source.
func (l localeSet) choose(chosen, acceptLanguage string) string {
	if l.offers(chosen) {
		return chosen
	}
	wanted, _, err := language.ParseAcceptLanguage(acceptLanguage)
	if err != nil || len(wanted) == 0 {
		return l.Source
	}
	have := append([]string{l.Source}, l.Translated...)
	tags := make([]language.Tag, len(have))
	for i, h := range have {
		tags[i] = language.Make(h)
	}
	_, i, confidence := language.NewMatcher(tags).Match(wanted...)
	if confidence == language.No {
		return l.Source
	}
	return have[i]
}

const langCookie = "lang"

// languageOf is the language to answer r in.
func languageOf(r *http.Request) string {
	chosen := ""
	if c, err := r.Cookie(langCookie); err == nil {
		chosen = c.Value
	}
	return locales.choose(chosen, r.Header.Get("Accept-Language"))
}

// pickLanguage keeps the language asked for by opening one of the app's pages with
// ?lang=, until another is picked; an empty or unknown one forgets it. It answers with
// the language r is then in.
func (a *App) pickLanguage(w http.ResponseWriter, r *http.Request) string {
	q := r.URL.Query()
	if !q.Has(langCookie) {
		return languageOf(r)
	}
	c := &http.Cookie{Name: langCookie, Value: q.Get(langCookie), Path: "/", MaxAge: 365 * 24 * 60 * 60, HttpOnly: true, Secure: a.secure(r), SameSite: http.SameSiteLaxMode}
	if !locales.offers(c.Value) {
		c.Value, c.MaxAge = "", -1
	}
	http.SetCookie(w, c)
	return locales.choose(c.Value, r.Header.Get("Accept-Language"))
}
