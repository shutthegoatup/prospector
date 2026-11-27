package main

import (
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// Postgres won't keep a NUL in text, nor bytes that aren't UTF-8. No one means to send
// either, so they are taken out of what arrives before anything reads it.

func cleanText(s string) string {
	if strings.IndexByte(s, 0) < 0 && isUTF8(s) {
		return s
	}
	return strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), "�")
}

func isUTF8(s string) bool { return strings.ToValidUTF8(s, "") == s }

func cleanValues(v url.Values) {
	for k, vs := range v {
		for i := range vs {
			vs[i] = cleanText(vs[i])
		}
		if c := cleanText(k); c != k {
			delete(v, k)
			v[c] = append(v[c], vs...)
		}
	}
}

// cleanRequest cleans the query, and any body but a form's as it is read: the JSON
// decoders read a body whatever it says it is. A JSON decoder already turns bytes that
// aren't UTF-8 into U+FFFD, and a NUL can only reach it written as \u0000. A form is
// left to parseForm, since each handler bounds its own. A path holding either names
// nothing here, so it is refused.
func cleanRequest(r *http.Request) (ok bool) {
	if cleanText(r.URL.Path) != r.URL.Path {
		return false
	}
	if q := r.URL.Query(); r.URL.RawQuery != "" {
		before := q.Encode()
		cleanValues(q)
		if after := q.Encode(); after != before {
			r.URL.RawQuery = after
		}
	}
	kind, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	// A document is kept byte for byte, so its body is left exactly as it came.
	if r.Body != nil && r.Body != http.NoBody && kind != "application/x-www-form-urlencoded" && kind != "application/pdf" && !strings.HasPrefix(kind, "multipart/") {
		r.Body = &withoutNUL{src: r.Body}
	}
	return true
}

// withoutNUL passes JSON through without its \u0000 escapes. Outside a string JSON has no
// backslashes, so each escape can be read where it starts; one split across two reads
// is held back until the rest arrives.
type withoutNUL struct {
	src     io.ReadCloser
	held    []byte
	ready   []byte
	err     error
	scratch [32 << 10]byte
}

func (w *withoutNUL) Close() error { return w.src.Close() }

func (w *withoutNUL) Read(p []byte) (int, error) {
	for len(w.ready) == 0 {
		if w.err != nil {
			return 0, w.err
		}
		n, err := w.src.Read(w.scratch[:])
		w.held = append(w.held, w.scratch[:n]...)
		w.err = err
		w.ready, w.held = dropNULEscapes(w.held, err != nil)
	}
	n := copy(p, w.ready)
	w.ready = w.ready[n:]
	return n, nil
}

func dropNULEscapes(b []byte, last bool) (out, rest []byte) {
	out = make([]byte, 0, len(b))
	for i := 0; i < len(b); {
		if b[i] != '\\' {
			out = append(out, b[i])
			i++
			continue
		}
		width := 2
		if i+1 < len(b) && b[i+1] == 'u' {
			width = 6
		}
		if i+width > len(b) {
			if last {
				return append(out, b[i:]...), nil
			}
			return out, append([]byte(nil), b[i:]...)
		}
		if width == 6 && string(b[i+2:i+6]) == "0000" {
			i += 6
			continue
		}
		out = append(out, b[i:i+width]...)
		i += width
	}
	return out, nil
}
