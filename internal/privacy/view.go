package privacy

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Person struct{ Name, Title string }

var altSplit = regexp.MustCompile(`(?i);|\s+·\s+|\bbackups?:?\s*|\bor\s+`)

var altPart = regexp.MustCompile(`^([^,(]+?)(?:\s*\(([^)]*)\))?\s*(?:,\s*(.*))?$`)

var altPrefix = regexp.MustCompile(`(?i)^(AC route|Backup \d|or)\s+`)

// Alternates splits the research's backups ("Ada Lovelace, CMO; Grace Hopper, Head of Digital") into choices.
func Alternates(person string) []Person {
	var parts []string
	last := 0
	for _, m := range altSplit.FindAllStringIndex(person, -1) {
		sep := strings.ToLower(strings.TrimSpace(person[m[0]:m[1]]))
		if sep == "or" {
			if r, _ := utf8.DecodeRuneInString(person[m[1]:]); !unicode.IsLetter(r) {
				continue
			}
		}
		parts = append(parts, person[last:m[0]])
		last = m[1]
	}
	parts = append(parts, person[last:])
	var out []Person
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		p := Person{Name: s}
		if m := altPart.FindStringSubmatch(s); m != nil {
			p.Name = m[1]
			p.Title = m[3]
			if p.Title == "" {
				p.Title = m[2]
			}
		}
		p.Name = strings.TrimSpace(altPrefix.ReplaceAllString(p.Name, ""))
		p.Title = strings.TrimSpace(p.Title)
		r, _ := utf8.DecodeRuneInString(p.Name)
		if unicode.IsUpper(r) && len(strings.Split(p.Name, " ")) <= 4 {
			out = append(out, p)
		}
	}
	return out
}
