package store

import (
	"fmt"
	"strings"
)

// Asset is something an organisation looks for at a company, such as a product
// configurator, a careers page or an applicant tracking system. A company has it
// when it has a fact whose field is the asset's.
type Asset struct {
	Field string
	Hint  string
	// Must is a company being no fit without it, marked "(must)" after its name.
	Must bool
}

// AssetKey lets "Careers page", "careers-page" and "careers_page" name one field.
func AssetKey(field string) string {
	return Underscores.Replace(strings.ToLower(strings.TrimSpace(field)))
}

var Underscores = strings.NewReplacer(" ", "_", "-", "_")

// Assets reads LookFor, one "field: hint" per line; the hint is optional.
func (s OrgSettings) Assets() ([]Asset, error) {
	var out []Asset
	seen := map[string]bool{}
	for line := range strings.SplitSeq(s.LookFor, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		field, hint, _ := strings.Cut(line, ":")
		field, must := CutMust(field)
		key := AssetKey(field)
		if key == "" {
			return nil, fmt.Errorf("%q needs a name before the colon", strings.TrimSpace(line))
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Asset{Field: key, Hint: strings.TrimSpace(hint), Must: must})
	}
	return out, nil
}

func CutMust(field string) (string, bool) {
	trimmed := strings.TrimSpace(field)
	if len(trimmed) >= 6 && strings.EqualFold(trimmed[len(trimmed)-6:], "(must)") {
		return trimmed[:len(trimmed)-6], true
	}
	return trimmed, false
}
