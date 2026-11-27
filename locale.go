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

// speaks is whether lang is one a person can choose: the source or a translation, not
// a pseudo-locale, which is only for looking at the app with ?lang=.
func speaks(lang string) bool {
	return lang == locales.Source || slices.Contains(locales.Translated, lang)
}

func (l localeSet) offers(lang string) bool {
	return lang == l.Source || slices.Contains(l.Translated, lang) || slices.Contains(l.Pseudo, lang)
}

// choose is the language to speak: one chosen if it is offered, otherwise the best of
// the browser's that the app has been translated into, otherwise fallback (an
// organisation's), otherwise the source.
func (l localeSet) choose(chosen, acceptLanguage, fallback string) string {
	if l.offers(chosen) {
		return chosen
	}
	if !l.offers(fallback) {
		fallback = l.Source
	}
	wanted, _, err := language.ParseAcceptLanguage(acceptLanguage)
	if err != nil || len(wanted) == 0 {
		return fallback
	}
	have := append([]string{l.Source}, l.Translated...)
	tags := make([]language.Tag, len(have))
	for i, h := range have {
		tags[i] = language.Make(h)
	}
	_, i, confidence := language.NewMatcher(tags).Match(wanted...)
	if confidence == language.No {
		return fallback
	}
	return have[i]
}

const langCookie = "lang"

// languageOf is the language to answer r in.
func languageOf(r *http.Request) string { return languageFor(r, "") }

// languageFor is the language to answer r in: one picked with ?lang=, then the
// signed-in member's own, then the best of their browser's, then fallback, which is
// what an organisation speaks to its outsiders and, unless another is given, to its
// members.
func languageFor(r *http.Request, fallback string) string {
	picked := ""
	if c, err := r.Cookie(langCookie); err == nil {
		picked = c.Value
	}
	return spoken(r, picked, fallback)
}

func spoken(r *http.Request, picked, fallback string) string {
	// Only a person has a language; a key's caller is answered by its own browser, if any.
	if q, ok := r.Context().Value(reqKey{}).(*Req); ok && q.User != nil {
		if !locales.offers(picked) {
			picked = q.User.Language
		}
		if fallback == "" && q.Org != nil {
			fallback = q.Org.Settings.CustomerLanguage
		}
	}
	return locales.choose(picked, r.Header.Get("Accept-Language"), fallback)
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
	return spoken(r, c.Value, "")
}
