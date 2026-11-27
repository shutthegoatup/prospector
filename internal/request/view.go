package request

import (
	"fmt"
	"strings"

	"github.com/shutthegoatup/prospector/internal/store"
)

// Voice is who is calling and signing, from the org's settings and the member's own.
type Voice struct {
	Caller, Org, Pitch, Signature string
	// Pitches are the offerings' own pitch lines, by offering, where one was written.
	Pitches map[int64]string
}

func (v Voice) PitchFor(offering int64) string {
	if p := v.Pitches[offering]; p != "" {
		return p
	}
	return v.Pitch
}

func (v Voice) CallerFirst() string {
	if f := store.FirstField(v.Caller); f != "" {
		return f
	}
	return "[YOUR NAME]"
}

func (v Voice) CallerFull() string {
	if v.Caller != "" {
		return v.Caller
	}
	return "[YOUR NAME]"
}

func (v Voice) Signed() string {
	if v.Signature == "" {
		return ""
	}
	return "\n\n" + v.Signature
}

func Plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	switch {
	case word == "person":
		word = "people"
	case strings.HasSuffix(word, "y") && !strings.ContainsAny(word[len(word)-2:len(word)-1], "aeiou"):
		word = word[:len(word)-1] + "ies"
	default:
		word += "s"
	}
	return fmt.Sprintf("%d %s", n, word)
}
