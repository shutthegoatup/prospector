package main

import (
	"strings"
)

const (
	SuggestionAccepted  = "accepted"
	SuggestionDismissed = "dismissed"
)

var suggestable = []string{"email", "phone", "linkedin"}

// maxWaiting is how many suggestions for one field of one person may wait at once, and
// maxDetail how long one may be.
const (
	maxWaiting = 3
	maxDetail  = 320
)

// sameDetail compares the way a person would: an address by its letters, a profile by
// its path, a number by its digits.
func sameDetail(field, a, b string) bool {
	switch field {
	case "email":
		// The column can hold several addresses; one of them is already held.
		for _, held := range emailRe.FindAllString(strings.ToLower(b), -1) {
			if strings.EqualFold(strings.TrimSpace(a), strings.Trim(held, ".")) {
				return true
			}
		}
	case "linkedin":
		if pa, pb := linkedinPath(a), linkedinPath(b); pa != "" || pb != "" {
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

func detailOf(c Contact, field string) string {
	switch field {
	case "email":
		return deref(c.Email)
	case "phone":
		return deref(c.Phone)
	case "linkedin":
		return deref(c.Linkedin)
	}
	return ""
}

func waitingFor(before []Suggestion, field, value string) bool {
	for _, b := range before {
		if b.Field == field && sameDetail(field, b.Value, value) {
			return b.Decision == ""
		}
	}
	return false
}

// PendingDetail is a waiting suggestion beside what the person has now.
type PendingDetail struct {
	Suggestion
	Current string
}
