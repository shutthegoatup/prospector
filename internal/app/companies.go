package app

import (
	"regexp"
	"strings"

	"github.com/shutthegoatup/prospector/internal/store"
)

// NeedsNextStep is a live conversation with no next step, or one that's due.
func NeedsNextStep(c store.Contact, day string) bool {
	if c.Stage == "won" || c.Stage == "lost" {
		return false
	}
	return c.NextStep == "" || (c.NextDate != nil && *c.NextDate <= day)
}

// LinkedIn page titles look like "(3) Hannah Pryce | LinkedIn"; headlines "Head of Marketing at Larkmoor Yachts".
var (
	unreadPrefix = regexp.MustCompile(`^\(\d+\)\s*`)
	linkedinTail = regexp.MustCompile(`\s*[|–-]\s*LinkedIn.*$`)
)

func ParseLinkedIn(title, headline string) (name, role, company string) {
	name = strings.TrimSpace(linkedinTail.ReplaceAllString(unreadPrefix.ReplaceAllString(title, ""), ""))
	headline = strings.TrimSpace(strings.SplitN(headline, "|", 2)[0])
	if i := strings.LastIndex(headline, " at "); i > 0 {
		return name, strings.TrimSpace(headline[:i]), strings.TrimSpace(headline[i+4:])
	}
	if i := strings.LastIndex(headline, " @ "); i > 0 {
		return name, strings.TrimSpace(headline[:i]), strings.TrimSpace(headline[i+3:])
	}
	return name, headline, ""
}
