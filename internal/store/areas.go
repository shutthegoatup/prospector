package store

import (
	"slices"
	"strings"
)

const AreaAny = "any"

func (m *Membership) Works(area string) bool {
	if m == nil {
		return false
	}
	if area == AreaAny {
		return m.Role == "owner" || strings.TrimSpace(m.Areas) != ""
	}
	return area == "" || m.Role == "owner" || slices.Contains(strings.Fields(m.Areas), area)
}
