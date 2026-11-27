package request

import "github.com/shutthegoatup/prospector/internal/prospecting"

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
	if f := prospecting.FirstField(v.Caller); f != "" {
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
