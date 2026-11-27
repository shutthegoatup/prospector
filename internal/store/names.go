package store

import (
	"regexp"
	"strings"
)

var personSplit = regexp.MustCompile(`[,;(]`)

func PersonKey(person string) string {
	words := strings.Fields(personSplit.Split(person, 2)[0])
	return strings.Join(words[:min(2, len(words))], " ")
}

var NameCut = regexp.MustCompile(`[,;(]`)

func DisplayName(c Contact) string {
	n := Deref(c.Person)
	if n == "" {
		n = Deref(c.First)
	}
	return strings.TrimSpace(NameCut.Split(n, 2)[0])
}
