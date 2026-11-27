package request

import (
	"encoding/json"
	"net/http"
	"slices"

	"golang.org/x/text/language"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/web"
)

var localesJSON = web.Locales

// LocaleSet is what web/locales.json says: the language the app is written in, those
// it has been translated into, and pseudo-locales, which stretch and accent the source
// so that text left out of the catalogues stands out. A pseudo-locale is only ever
// chosen by name, never matched from a browser's languages.
type LocaleSet struct {
	Source     string   `json:"source"`
	Translated []string `json:"translated"`
	Pseudo     []string `json:"pseudo"`
}

var Locales = func() LocaleSet {
	var l LocaleSet
	if err := json.Unmarshal(localesJSON, &l); err != nil || l.Source == "" {
		panic("web/locales.json doesn't say what language the app is written in")
	}
	for _, p := range l.Pseudo {
		say.Pseudo(p)
	}
	return l
}()

func (l LocaleSet) Offers(lang string) bool {
	return lang == l.Source || slices.Contains(l.Translated, lang) || slices.Contains(l.Pseudo, lang)
}

// Choose is the language to speak: one chosen if it is offered, otherwise the best of
// the browser's that the app has been translated into, otherwise fallback (an
// organisation's), otherwise the source.
func (l LocaleSet) Choose(chosen, acceptLanguage, fallback string) string {
	if l.Offers(chosen) {
		return chosen
	}
	if !l.Offers(fallback) {
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

const LangCookie = "lang"

// LanguageOf is the language to answer r in.
func LanguageOf(r *http.Request) string { return LanguageFor(r, "") }

// LanguageFor is the language to answer r in: one picked with ?lang=, then the
// signed-in member's own, then the best of their browser's, then fallback, which is
// what an organisation speaks to its outsiders and, unless another is given, to its
// members.
func LanguageFor(r *http.Request, fallback string) string {
	picked := ""
	if c, err := r.Cookie(LangCookie); err == nil {
		picked = c.Value
	}
	return Spoken(r, picked, fallback)
}

func Spoken(r *http.Request, picked, fallback string) string {
	// Only a person has a language; a key's caller is answered by its own browser, if any.
	if q, ok := r.Context().Value(ReqKey{}).(*Req); ok && q.User != nil {
		if !Locales.Offers(picked) {
			picked = q.User.Language
		}
		if fallback == "" && q.Org != nil {
			fallback = q.Org.Settings.CustomerLanguage
		}
	}
	return Locales.Choose(picked, r.Header.Get("Accept-Language"), fallback)
}
