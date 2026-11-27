package store

import (
	"regexp"
	"strings"
)

var NameCut = regexp.MustCompile(`[,;(]`)

func DisplayName(c Contact) string {
	n := Deref(c.Person)
	if n == "" {
		n = Deref(c.First)
	}
	return strings.TrimSpace(NameCut.Split(n, 2)[0])
}
