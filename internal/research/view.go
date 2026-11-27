package research

import "strings"

func FirstLine(s string) string { return strings.SplitN(s, "\n", 2)[0] }

func prefixed(prefix, s string) string {
	if s == "" {
		return ""
	}
	return prefix + s
}
