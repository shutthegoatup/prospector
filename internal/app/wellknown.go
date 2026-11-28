package app

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shutthegoatup/prospector/api"
)

// The plain-text files crawlers and agents look for at a site's root. They describe
// the software, never an organisation's data, so they are public.

func (a *App) wellKnownRoutes(mux *http.ServeMux) {
	spec, err := api.Spec()
	if err != nil {
		log.Fatalf("the API spec: %v", err)
	}
	doc := apiDocOf(spec)
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		plainText(w, robotsTxt(a.baseURL(r)))
	})
	mux.HandleFunc("GET /sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = fmt.Fprint(w, sitemapXML(a.baseURL(r)))
	})
	mux.HandleFunc("GET /llms.txt", func(w http.ResponseWriter, r *http.Request) {
		plainText(w, llmsTxt(doc, a.baseURL(r)))
	})
	mux.HandleFunc("GET /.well-known/security.txt", func(w http.ResponseWriter, r *http.Request) {
		if a.securityContact == "" {
			http.NotFound(w, r)
			return
		}
		plainText(w, securityTxt(a.securityContact, a.baseURL(r), time.Now()))
	})
}

func plainText(w http.ResponseWriter, body string) {
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprint(w, body)
}

// robotsTxt keeps crawlers out of what only a signed-in member can read. The
// customer and unsubscribe links stay fetchable: they say noindex themselves, which a
// crawler barred from them would never see.
func robotsTxt(base string) string {
	return "User-agent: *\n" +
		"Allow: /\n" +
		"Disallow: /o/\n" +
		"Disallow: /login/\n" +
		"Disallow: /password\n" +
		"Disallow: /reset/\n" +
		"\n" +
		"Sitemap: " + base + "/sitemap.xml\n"
}

func sitemapXML(base string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, path := range []string{"/", "/api"} {
		fmt.Fprintf(&b, "  <url><loc>%s%s</loc></url>\n", xmlEscape(base), path)
	}
	b.WriteString("</urlset>\n")
	return b.String()
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
}

// llmsTxt follows llmstxt.org: a title, a one-line summary, then lists of links. The
// operations are drawn from the spec so the file can't fall behind it.
func llmsTxt(doc APIDoc, base string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", doc.Title)
	b.WriteString("> A self-hosted, account-based prospecting workbench for small teams selling to a few hundred named accounts. " +
		"People and agents research companies, keeping a source link for every fact; people work one daily queue from first touch to deal. " +
		"Agents research and draft over MCP; only people send.\n\n")
	for _, p := range doc.Intro {
		b.WriteString(p + "\n\n")
	}
	b.WriteString("Each organisation has its own address under `/o/{org}/`, `{org}` being its slug. An agent connects over MCP at " +
		"`" + base + "/o/{org}/mcp` with the same API key.\n\n")
	b.WriteString("## Docs\n\n")
	fmt.Fprintf(&b, "- [API reference](%s/api): every operation, the scope it needs, and its request and answer\n", base)
	fmt.Fprintf(&b, "- [OpenAPI spec](%s/api/openapi.json): the same contract as OpenAPI 3, to generate a client from\n", base)
	for _, g := range doc.Groups {
		if len(g.Ops) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n\n", g.Name)
		for _, o := range g.Ops {
			fmt.Fprintf(&b, "- [%s %s](%s/api#%s)", o.Method, o.Path, base, opAnchor(o))
			if o.Summary != "" {
				b.WriteString(": " + strings.Join(strings.Fields(o.Summary), " "))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n## Optional\n\n")
	fmt.Fprintf(&b, "- [Source and README](%s): what it does, how to run it, and the configuration\n", repoURL)
	fmt.Fprintf(&b, "- [Releases](%s/releases)\n", repoURL)
	return b.String()
}

// securityTxt is RFC 9116's file. Expires is meant to be under a year away and kept
// moving, so it is counted from today rather than written down.
func securityTxt(contact, base string, now time.Time) string {
	expires := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, 180)
	return "Contact: " + contact + "\n" +
		"Expires: " + expires.Format(time.RFC3339) + "\n" +
		"Preferred-Languages: en\n" +
		"Canonical: " + base + "/.well-known/security.txt\n"
}

// securityContactFrom checks PROSPECTOR_SECURITY_CONTACT is a URI RFC 9116 accepts as a contact.
func securityContactFrom(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	ok := err == nil && (u.Scheme == "mailto" && u.Opaque != "" || u.Scheme == "https" && u.Host != "" || u.Scheme == "tel" && u.Opaque != "")
	if !ok {
		return "", fmt.Errorf("PROSPECTOR_SECURITY_CONTACT should be a mailto:, https:// or tel: address, not %q", raw)
	}
	return raw, nil
}

const repoURL = "https://github.com/shutthegoatup/prospector"
