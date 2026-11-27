package prospecting

import (
	"strings"

	"github.com/shutthegoatup/prospector/internal/store"
)

const SuggestionAccepted = "accepted"

const SuggestionDismissed = "dismissed"

var Suggestable = []string{"email", "phone", "linkedin"}

const MaxWaiting = 3

const MaxDetail = 320

// sameDetail compares the way a person would: an address by its letters, a profile by
// its path, a number by its digits.
func sameDetail(field, a, b string) bool {
	switch field {
	case "email":
		// The column can hold several addresses; one of them is already held.
		for _, held := range store.EmailRe.FindAllString(strings.ToLower(b), -1) {
			if strings.EqualFold(strings.TrimSpace(a), strings.Trim(held, ".")) {
				return true
			}
		}
	case "linkedin":
		if pa, pb := store.LinkedinPath(a), store.LinkedinPath(b); pa != "" || pb != "" {
			return pa == pb
		}
	case "phone":
		return digits(a) == digits(b)
	}
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func digits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

func DetailOf(c store.Contact, field string) string {
	switch field {
	case "email":
		return store.Deref(c.Email)
	case "phone":
		return store.Deref(c.Phone)
	case "linkedin":
		return store.Deref(c.Linkedin)
	}
	return ""
}

func waitingFor(before []store.Suggestion, field, value string) bool {
	for _, b := range before {
		if b.Field == field && sameDetail(field, b.Value, value) {
			return b.Decision == ""
		}
	}
	return false
}

// PendingDetail is a waiting suggestion beside what the person has now.
type PendingDetail struct {
	store.Suggestion
	Current string
}
