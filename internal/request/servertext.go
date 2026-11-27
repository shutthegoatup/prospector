package request

import "net/http"

// speaking stamps an answer with the language r is answered in, which varies with the
// request's own languages and the cookie that can choose one (RFC 9110 §8.5, §12.5.5).
func speaking(w http.ResponseWriter, r *http.Request) string {
	lang := LanguageOf(r)
	w.Header().Set("Content-Language", lang)
	w.Header().Add("Vary", "Accept-Language, Cookie")
	return lang
}
