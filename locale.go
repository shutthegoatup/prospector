package main

import (
	"net/http"
	"slices"

	"github.com/shutthegoatup/prospector/internal/request"
)

// speaks is whether lang is one a person can choose: the source or a translation, not
// a pseudo-locale, which is only for looking at the app with ?lang=.
func speaks(lang string) bool {
	return lang == request.Locales.Source || slices.Contains(request.Locales.Translated, lang)
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
	if !request.Locales.Offers(c.Value) {
		c.Value, c.MaxAge = "", -1
	}
	http.SetCookie(w, c)
	return spoken(r, c.Value, "")
}
