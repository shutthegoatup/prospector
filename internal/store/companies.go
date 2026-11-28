package store

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/shutthegoatup/prospector/internal/say"
)

func DomainOf(u string) string {
	p, err := url.Parse(strings.TrimSpace(u))
	if err != nil || p.Host == "" {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(p.Host), "www.")
}

// BareDomain reads "https://www.Example.com/about" and "example.com" alike.
func BareDomain(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if d := DomainOf(v); d != "" {
		return d
	}
	v, _, _ = strings.Cut(v, "/")
	return strings.TrimPrefix(v, "www.")
}

// Conversation stages, in board order.
var Stages = []struct{ Key, Label string }{
	{"replied", "Replied"}, {"meeting", "Meeting"}, {"proposal", "Proposal"}, {"won", "Won"}, {"lost", "Lost"},
}

// Listed reports the never-contact entry an email address or link falls under, if any.
// Each entry is one thing: a LinkedIn profile, an email address, or a domain. Nothing
// matches by loose substring, because the list also hides people from the queues and
// a stray word must not hide the wrong ones.
func (set OrgSettings) Listed(email, link string) error {
	email, profile := EmailKey(email), LinkedinPath(link)
	for _, raw := range strings.FieldsFunc(strings.ToLower(set.NeverContact), func(r rune) bool { return r == ',' || r == '\n' || r == ' ' }) {
		entry := strings.TrimRight(strings.TrimPrefix(SchemeRe.ReplaceAllString(strings.TrimSpace(raw), ""), "www."), "/")
		if entry == "" {
			continue
		}
		var hit bool
		switch want := LinkedinPath(entry); {
		case want != "":
			hit = profile == want || strings.HasPrefix(profile, want+"/")
		case strings.Contains(entry, "@"):
			hit = email == entry
		default:
			host, _, _ := strings.Cut(entry, "/")
			hit = email != "" && (strings.HasSuffix(email, "@"+host) || strings.HasSuffix(email, "."+host))
		}
		if hit {
			return say.Errorf("%s is on the never-contact list", raw)
		}
	}
	return nil
}

var SchemeRe = regexp.MustCompile(`^[a-z]+://`)

// NeverDomains are the list's domain entries, the ones that cover a whole company.
func (set OrgSettings) NeverDomains() []string {
	var out []string
	for _, raw := range strings.FieldsFunc(strings.ToLower(set.NeverContact), func(r rune) bool { return r == ',' || r == '\n' || r == ' ' }) {
		entry := strings.TrimRight(strings.TrimPrefix(SchemeRe.ReplaceAllString(strings.TrimSpace(raw), ""), "www."), "/")
		if entry == "" || LinkedinPath(entry) != "" || strings.Contains(entry, "@") {
			continue
		}
		host, _, _ := strings.Cut(entry, "/")
		out = append(out, host)
	}
	return out
}

// CoversCompany is whether a company's web domain, or one it is under, is on the list.
// The company is kept and built on; only contacting it is barred.
func (set OrgSettings) CoversCompany(domain string) bool {
	domain = BareDomain(domain)
	if domain == "" {
		return false
	}
	for _, host := range set.NeverDomains() {
		if domain == host || strings.HasSuffix(domain, "."+host) {
			return true
		}
	}
	return false
}

// LinkedinPath is what identifies a LinkedIn page whatever the subdomain: "in/hannah-pryce".
func LinkedinPath(link string) string {
	_, path, found := strings.Cut(strings.ToLower(strings.TrimSpace(link)), "linkedin.com/")
	if !found {
		return ""
	}
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	return strings.TrimRight(path, "/")
}

var countryInName = regexp.MustCompile(`\s*\(.*\)\s*$`)

// CompanyName strips the "(UK, group)" tags the drafts put after maker names.
func CompanyName(maker string) string {
	return strings.TrimSpace(countryInName.ReplaceAllString(maker, ""))
}
