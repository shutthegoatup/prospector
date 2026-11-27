package research

import (
	"net/url"
	"strings"
)

func NormalURL(raw string) string {
	raw = strings.TrimRight(raw, ".,;:!?*_`")
	// A closing bracket belongs to the address only if it opened one, as in
	// .../Acme_(company); otherwise it closes the sentence around it.
	for strings.HasSuffix(raw, ")") && strings.Count(raw, ")") > strings.Count(raw, "(") {
		raw = strings.TrimRight(strings.TrimSuffix(raw, ")"), ".,;:!?*_`")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	u.Fragment, u.Host = "", strings.ToLower(u.Host)
	return strings.TrimRight(u.String(), "/")
}
