package main

import (
	"bytes"
	"log"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

// wantsPage is a person's browser opening a page or submitting a form, as opposed to
// the page script, the CLI or an agent, which read an error as plain text.
func wantsPage(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html") && !hasKey(r)
}

// hasKey is a request carrying an API key. A browser never attaches a Bearer header by
// itself, unlike the Basic one it resends once a proxy in front has asked for it, so
// only Bearer can stand in for the checks that guard a signed-in browser.
func hasKey(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// problemPage turns a plain-text error on its way to a browser into a page that looks
// like the rest of the app, so handlers can keep calling http.Error.
type problemPage struct {
	http.ResponseWriter
	r       *http.Request
	started bool
	code    int
	text    bytes.Buffer
}

func (p *problemPage) Unwrap() http.ResponseWriter { return p.ResponseWriter }

func (p *problemPage) WriteHeader(code int) {
	if p.started {
		return
	}
	p.started = true
	if code >= 400 && strings.HasPrefix(p.Header().Get("Content-Type"), "text/plain") {
		p.code = code
		return
	}
	p.ResponseWriter.WriteHeader(code)
}

func (p *problemPage) Write(b []byte) (int, error) {
	if !p.started {
		p.WriteHeader(http.StatusOK)
	}
	if p.code == 0 {
		return p.ResponseWriter.Write(b)
	}
	if p.text.Len() < 2048 {
		p.text.Write(b)
	}
	return len(b), nil
}

func (p *problemPage) finish() {
	if p.code == 0 {
		return
	}
	h := p.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Del("Content-Length")
	p.ResponseWriter.WriteHeader(p.code)
	title, message := problemText(p.code, p.text.String())
	if err := ProblemPage(p.code, title, message, "/").Render(p.r.Context(), p.ResponseWriter); err != nil {
		log.Print(err)
	}
}

func problemText(code int, text string) (title, message string) {
	message = strings.TrimSpace(text)
	if first, size := utf8.DecodeRuneInString(message); size > 0 {
		message = string(unicode.ToUpper(first)) + message[size:]
	}
	if last, _ := utf8.DecodeLastRuneInString(message); message != "" && !strings.ContainsRune(".!?…", last) {
		message += "."
	}
	switch {
	case code == http.StatusNotFound:
		return "Nothing here", "That page doesn't exist, or it belongs to an organisation you're not in."
	case code == http.StatusForbidden:
		return "Not allowed", message
	case code == http.StatusUnauthorized:
		return "Signed out", message
	case code == http.StatusTooManyRequests:
		return "Slow down", message
	case code >= 500:
		// The text may carry internals, so it stays in the plain-text response only.
		return "Something went wrong", "The server couldn't finish that. Try again in a moment."
	}
	return "That didn't work", message
}
