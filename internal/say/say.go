// Package say is the server's words for people: errors and answers made like
// errors.New and fmt.Errorf, which read in English as errors do and in another language
// where the server answers someone who reads one. A format is the key lang/ translates
// it by.
package say

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"strings"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
)

// Source is the language the server is written in.
const Source = "en-GB"

// Text is words for a person, as an error.
type Text struct {
	format string
	args   []any
	// literal is text with no verbs to fill, which may hold a % of its own.
	literal bool
}

func New(text string) *Text { return &Text{format: text, literal: true} }

// Nothing is no words, for a part of a sentence that isn't there this time.
var Nothing = &Text{literal: true}

// Errorf is fmt.Errorf without %w: what a person reads doesn't wrap another error.
func Errorf(format string, args ...any) *Text { return &Text{format: format, args: args} }

func (t *Text) Error() string { return t.In(Source) }

// In is the text in lang, or in English where lang has no translation of it.
func (t *Text) In(lang string) string {
	args := make([]any, len(t.args))
	for i, a := range t.args {
		if inner, ok := a.(*Text); ok {
			a = inner.In(lang)
		}
		args[i] = a
	}
	format := t.format
	for _, l := range []string{lang, Source} {
		if m, ok := catalogues[l][t.format]; ok {
			format = m.choose(l, args)
			break
		}
	}
	if t.literal {
		return format
	}
	return fmt.Sprintf(format, args...)
}

// Sayer is an error with words for a person, which errors.As finds under the kinds of
// error that carry a Text.
type Sayer interface {
	error
	In(lang string) string
}

// Message is one entry of a catalogue: a translation, or one for each plural form of
// the format's first signed integer, as CLDR names them ("one", "few", "other"…).
type Message struct {
	Text  string
	Forms map[string]string
}

func (m *Message) UnmarshalJSON(b []byte) error {
	if json.Unmarshal(b, &m.Text) == nil {
		return nil
	}
	return json.Unmarshal(b, &m.Forms)
}

func (m Message) MarshalJSON() ([]byte, error) {
	if m.Forms != nil {
		return json.Marshal(m.Forms)
	}
	return json.Marshal(m.Text)
}

var formNames = map[plural.Form]string{plural.Other: "other", plural.Zero: "zero", plural.One: "one", plural.Two: "two", plural.Few: "few", plural.Many: "many"}

func (m Message) choose(lang string, args []any) string {
	if m.Forms == nil {
		return m.Text
	}
	for _, a := range args {
		if v := reflect.ValueOf(a); v.CanInt() {
			n := int(v.Int())
			if f, ok := m.Forms[formNames[plural.Cardinal.MatchPlural(language.Make(lang), max(n, -n), 0, 0, 0, 0)]]; ok {
				return f
			}
			break
		}
	}
	return m.Forms["other"]
}

//go:embed lang/*.json
var files embed.FS

// catalogues are the translations in lang/, the source's own included for its plural
// forms, by language.
var catalogues = func() map[string]map[string]Message {
	all := map[string]map[string]Message{}
	entries, _ := files.ReadDir("lang")
	for _, e := range entries {
		var c map[string]Message
		b, _ := files.ReadFile(path.Join("lang", e.Name()))
		if err := json.Unmarshal(b, &c); err != nil {
			panic(fmt.Sprintf("internal/say/lang/%s: %v", e.Name(), err))
		}
		all[strings.TrimSuffix(e.Name(), ".json")] = c
	}
	return all
}()

// Catalogue is what lang/ holds for lang.
func Catalogue(lang string) map[string]Message { return catalogues[lang] }

// Languages are those lang/ has a catalogue for.
func Languages() []string {
	var l []string
	entries, _ := files.ReadDir("lang")
	for _, e := range entries {
		l = append(l, strings.TrimSuffix(e.Name(), ".json"))
	}
	return l
}

// Pseudo makes lang a pseudo-locale of the source: each of its formats with the letters
// accented and the whole bracketed, its verbs as they were, so that text the server
// didn't translate stands out. It is for the server's start, before any request.
func Pseudo(lang string) {
	c := map[string]Message{}
	for k, m := range catalogues[Source] {
		m.Text = pseudo(m.Text)
		if m.Forms != nil {
			forms := map[string]string{}
			for f, text := range m.Forms {
				forms[f] = pseudo(text)
			}
			m.Forms = forms
		}
		c[k] = m
	}
	catalogues[lang] = c
}

var accented = strings.NewReplacer(
	"a", "á", "b", "ƀ", "c", "ç", "d", "ð", "e", "é", "f", "ƒ", "g", "ĝ", "h", "ĥ", "i", "î", "j", "ĵ", "k", "ķ", "l", "ļ", "m", "ɱ",
	"n", "ñ", "o", "ö", "p", "þ", "q", "ǫ", "r", "ŕ", "s", "š", "t", "ţ", "u", "û", "v", "ṽ", "w", "ŵ", "x", "ẋ", "y", "ý", "z", "ž",
	"A", "Å", "B", "Ɓ", "C", "Ç", "D", "Ð", "E", "É", "F", "Ƒ", "G", "Ĝ", "H", "Ĥ", "I", "Î", "J", "Ĵ", "K", "Ķ", "L", "Ļ", "M", "Ṁ",
	"N", "Ñ", "O", "Ö", "P", "Þ", "Q", "Ǫ", "R", "Ŕ", "S", "Š", "T", "Ţ", "U", "Û", "V", "Ṽ", "W", "Ŵ", "X", "Ẋ", "Y", "Ý", "Z", "Ž",
)

func pseudo(format string) string {
	if format == "" {
		return ""
	}
	var b strings.Builder
	for _, part := range Split(format) {
		if strings.HasPrefix(part, "%") {
			b.WriteString(part)
		} else {
			b.WriteString(accented.Replace(part))
		}
	}
	return "[" + b.String() + "]"
}

// Split is a format in pieces: runs of text, and each verb (flags, width, precision and
// argument index included) on its own.
func Split(format string) []string {
	var parts []string
	for rest := format; rest != ""; {
		i := strings.IndexByte(rest, '%')
		if i < 0 {
			return append(parts, rest)
		}
		if i > 0 {
			parts = append(parts, rest[:i])
		}
		n := len(rest)
		for j := i + 1; j < len(rest); j++ {
			if c := rest[j]; c == '%' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') {
				n = j + 1
				break
			}
		}
		parts = append(parts, rest[i:n])
		rest = rest[n:]
	}
	return parts
}
