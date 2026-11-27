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
