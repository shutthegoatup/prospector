package privacy

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/shutthegoatup/prospector/internal/store"
)

// Draft is one contact parsed from leads/outreach/*.md. Parsing runs wherever the repo
// is checked out; the upsert runs on the server, so a hosted server can be fed from a laptop.
type Draft struct {
	Key      string  `json:"key"`
	File     string  `json:"file"`
	Section  string  `json:"section"`
	Fit      string  `json:"fit"`
	Maker    string  `json:"maker"`
	Person   string  `json:"person"`
	First    string  `json:"first"`
	Linkedin string  `json:"linkedin"`
	Email    string  `json:"email"`
	Note     string  `json:"note"`
	Subject  string  `json:"subject"`
	Body     string  `json:"body"`
	Followup string  `json:"followup"`
	Flags    string  `json:"flags"`
	Hold     *string `json:"hold"`
	Sources  string  `json:"sources"`
	Phone    string  `json:"phone"`
}

type SentLogEntry struct {
	Date string `json:"date"`
	Line string `json:"line"`
}

type ImportPayload struct {
	Drafts    []Draft        `json:"drafts"`
	Companies []CompanyRow   `json:"companies"`
	SentLog   []SentLogEntry `json:"sent_log"`
}

var (
	urlRe = regexp.MustCompile("https?://[^\\s)>\\]`|,]+")
	// Python's version has a lookbehind; RE2 doesn't, so the guard char is matched and dropped.
	phoneRe     = regexp.MustCompile(`(?:^|[^\p{L}\p{N}_/.=-])(\+\d{1,3}[\d ()-]{6,}\d)`)
	headingRe   = regexp.MustCompile(`^(##+) (.+)$`)
	noteRe      = regexp.MustCompile(`\*\*LinkedIn note \(\d+\)\*\*\n> (.+)`)
	altRe       = regexp.MustCompile(`^Version for ([^(]+)`)
	backtickAt  = regexp.MustCompile("`([^`]+@[^`]+)`")
	subjectRe   = regexp.MustCompile(`\*\*Subject:\*\* (.+)`)
	emailBodyRe = regexp.MustCompile(`\*\*Subject:\*\*[^\n]*\n((?:>.*\n?)+)`)
	followRe    = regexp.MustCompile(`\*\*Follow-up\*\*\n((?:>.*\n?)+)`)
	flagsRe     = regexp.MustCompile(`(?s)\*\*Flags:?\*\*:?(.*)`)
	holdFitRe   = regexp.MustCompile(`HOLD|WAIT`)
	linkCellRe  = regexp.MustCompile(`\((https://[^)]+)\)`)
	primaryRe   = regexp.MustCompile(`(?m)^\| Primary \| ([^|]+?) \| ([^|]+?) \|`)
	hiLineRe    = regexp.MustCompile(`(?m)^> (Hi .+)`)
	makerHeadRe = regexp.MustCompile(`^#\s*\d+\w?\s*·\s*`)
	addressRe   = regexp.MustCompile("`([^`\\s]+@[^`\\s]+)`")
	quoteRunRe  = regexp.MustCompile(`(?m)((?:^>.*\n?)+)`)
	hiCommaRe   = regexp.MustCompile(`^(Hi [^,]+), `)
	gladRe      = regexp.MustCompile(` ((?:Would be glad|Glad) to connect[^\n]*)\n?$`)
	sentDateRe  = regexp.MustCompile(`^## (\d{4}-\d\d-\d\d)`)
	notSentRe   = regexp.MustCompile(`^- \*\*(On hold|Held back|Waiting)`)

	digitStart = regexp.MustCompile(`^\d`)
)

func evidence(text, ownDomain string) (string, string) {
	var urls, phones []string
	for _, u := range urlRe.FindAllString(text, -1) {
		u = strings.TrimRight(u, ".;:'\"")
		if !strings.Contains(u, "linkedin.com/search") && (ownDomain == "" || !strings.Contains(u, ownDomain)) && !slices.Contains(urls, u) {
			urls = append(urls, u)
		}
	}
	for _, m := range phoneRe.FindAllStringSubmatch(text, -1) {
		p := strings.Join(strings.Fields(m[1]), " ")
		if !slices.Contains(phones, p) {
			phones = append(phones, p)
		}
	}
	return strings.Join(urls, "\n"), strings.Join(phones, " / ")
}

// spaced breaks a one-line LinkedIn note into the paragraphs it is pasted as.
func spaced(note string) string {
	note = hiCommaRe.ReplaceAllString(note, "$1,\n\n")
	note = strings.Replace(note, " I work on ", "\n\nI work on ", 1)
	return gladRe.ReplaceAllString(note, "\n\n$1")
}

func quoted(block string) string {
	var out []string
	for l := range strings.SplitSeq(strings.TrimSpace(block), "\n") {
		switch {
		case strings.HasPrefix(l, "> "):
			out = append(out, l[2:])
		case l == ">":
			out = append(out, "")
		default:
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

type sendRow struct{ fit, maker, person, link, email string }

func sendList(text string) []sendRow {
	var rows []sendRow
	for line := range strings.SplitSeq(text, "\n") {
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if len(cells) != 5 || cells[0] == "Fit" || cells[0] == "---" || strings.Trim(cells[0], "-: ") == "" {
			continue
		}
		r := sendRow{fit: cells[0], maker: cells[1], person: cells[2]}
		if m := linkCellRe.FindStringSubmatch(cells[3]); m != nil {
			r.link = m[1]
		}
		if m := backtickAt.FindStringSubmatch(cells[4]); m != nil {
			r.email = m[1]
		}
		rows = append(rows, r)
	}
	return rows
}

type mdSection struct{ level, head, body string }

// sections splits on "##"-or-deeper headings, as Python's ^(##+) ([^\n]+)\n(.*?)(?=^##+ |\Z) did.
func sections(text string) []mdSection {
	var out []mdSection
	lines := strings.SplitAfter(text, "\n")
	cur := -1
	for _, line := range lines {
		if m := headingRe.FindStringSubmatch(strings.TrimSuffix(line, "\n")); m != nil && strings.HasSuffix(line, "\n") {
			out = append(out, mdSection{level: m[1], head: m[2]})
			cur = len(out) - 1
			continue
		}
		if cur >= 0 {
			out[cur].body += line
		}
	}
	return out
}

func group(re *regexp.Regexp, s string) (string, bool) {
	if m := re.FindStringSubmatch(s); m != nil {
		return m[1], true
	}
	return "", false
}

func parseSectioned(name, text, ownDomain string) []Draft {
	rows := sendList(text)
	var out []Draft
	parent := ""
	for _, s := range sections(text) {
		if s.level == "##" {
			parent = s.head
		}
		note, ok := group(noteRe, s.body)
		if !ok {
			continue
		}
		first := strings.TrimSpace(strings.ReplaceAll(strings.SplitN(note, ",", 2)[0], "Hi ", ""))
		row := sendRow{maker: parent}
		if parts := strings.Split(parent, "·"); len(parts) > 1 {
			row.maker = strings.TrimSpace(parts[1])
		}
		for _, r := range rows {
			if strings.Contains(parent, strings.TrimRight(store.FirstField(r.maker), ".")) && strings.Contains(r.person, first) {
				row = r
				break
			}
		}
		if alt, ok := group(altRe, s.head); ok {
			row.person = strings.TrimSpace(alt)
			if e, ok := group(backtickAt, s.body); ok {
				row.email = e
			}
		}
		d := Draft{File: name, Section: strings.TrimSpace(s.head), Fit: row.fit, Maker: row.maker, Person: row.person,
			First: first, Linkedin: row.link, Email: row.email, Note: spaced(note)}
		d.Subject, _ = group(subjectRe, s.body)
		if b, ok := group(emailBodyRe, s.body); ok {
			d.Body = quoted(b)
		}
		if f, ok := group(followRe, s.body); ok {
			d.Followup = quoted(f)
		}
		if f, ok := group(flagsRe, s.body); ok {
			d.Flags = strings.TrimSpace(f)
		}
		if strings.Contains(s.head, "HOLD") || holdFitRe.MatchString(row.fit) || strings.HasPrefix(first, "{") {
			d.Hold = new("file")
		}
		d.Sources, d.Phone = evidence(s.body, ownDomain)
		out = append(out, d)
	}
	return out
}

// section returns the body under "## <title>…" up to the next "## " heading.
func section(text, title string) string {
	lines := strings.SplitAfter(text, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "## "+title) && strings.HasSuffix(l, "\n") {
			var b strings.Builder
			for _, l := range lines[i+1:] {
				if strings.HasPrefix(l, "## ") {
					break
				}
				b.WriteString(l)
			}
			return b.String()
		}
	}
	return ""
}

// parseNumbered handles the one-prospect-per-file drafts (01-acme.md and so on).
func parseNumbered(name, text, ownDomain string) []Draft {
	primary := primaryRe.FindStringSubmatch(text)
	note, ok := group(hiLineRe, section(text, "LinkedIn note"))
	if primary == nil || !ok || strings.HasPrefix(primary[1], "(") {
		return nil
	}
	person := strings.TrimSpace(primary[1])
	maker := strings.TrimSpace(strings.SplitN(makerHeadRe.ReplaceAllString(strings.SplitN(text, "\n", 2)[0], ""), " · ", 2)[0])
	emailPart := section(text, "Email")
	words := append(strings.Fields(person), strings.Fields(maker)[:min(1, len(strings.Fields(maker)))]...)
	d := Draft{File: name, Section: maker, Maker: maker, Person: person, First: store.FirstField(person),
		Linkedin: "https://www.linkedin.com/search/results/people/?keywords=" + strings.Join(words, "%20"),
		Note:     spaced(note)}
	d.Subject, _ = group(subjectRe, emailPart)
	d.Email, _ = group(addressRe, section(text, "Channel"))
	if f, ok := group(quoteRunRe, section(text, "Follow-up")); ok {
		d.Followup = quoted(f)
	}
	if b, ok := group(quoteRunRe, emailPart); ok {
		d.Body = quoted(b)
	}
	flags := section(text, "Risks/flags")
	if flags == "" {
		flags = section(text, "Flags")
	}
	d.Flags = strings.TrimSpace(flags)
	d.Sources, d.Phone = evidence(text, ownDomain)
	return []Draft{d}
}

// ReadRepo parses a folder laid out as leads/outreach/*.md, leads/prospects.csv and
// leads/outreach/sent-log.md. Links to ownDomain (your own site) are left out of sources.
func ReadRepo(repo, ownDomain string) (*ImportPayload, error) {
	p := &ImportPayload{}
	paths, err := filepath.Glob(filepath.Join(repo, "leads/outreach/*.md"))
	if err != nil {
		return nil, err
	}
	slices.Sort(paths)
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		name := filepath.Base(path)
		var drafts []Draft
		if digitStart.MatchString(name) {
			drafts = parseNumbered(name, string(b), ownDomain)
		} else {
			drafts = parseSectioned(name, string(b), ownDomain)
		}
		for _, d := range drafts {
			d.Key = store.PersonKey(d.Person)
			if d.Key == "" {
				d.Key = d.First + " @ " + d.Maker
			}
			p.Drafts = append(p.Drafts, d)
		}
	}
	f, err := os.Open(filepath.Join(repo, "leads/prospects.csv"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	for _, rec := range records[1:] {
		c := CompanyRow{}
		for i, h := range records[0] {
			c[h] = rec[i]
		}
		p.Companies = append(p.Companies, c)
	}
	if b, err := os.ReadFile(filepath.Join(repo, "leads/outreach/sent-log.md")); err == nil {
		date := ""
		for line := range strings.SplitSeq(string(b), "\n") {
			if m := sentDateRe.FindStringSubmatch(line); m != nil {
				date = m[1]
			} else if date != "" && strings.HasPrefix(line, "- **") && !notSentRe.MatchString(line) {
				p.SentLog = append(p.SentLog, SentLogEntry{Date: date, Line: line})
			}
		}
	}
	return p, nil
}
