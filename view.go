package main

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Steps are Today's filters, in the order the chips show.
var Steps = []struct{ Key, Label, Card string }{
	{"call", "Calls", "Call"},
	{"accepted", "Messages", "Message"},
	{"email", "Emails", "Email"},
	{"followup", "Follow-ups", "Follow-up"},
	{"linkedin", "LinkedIn adds", "LinkedIn add"},
	{"next", "Next steps", "Next step"},
}

// Voice is who is calling and signing, from the org's settings and the member's own.
type Voice struct {
	Caller, Org, Pitch, Signature string
	// Pitches are the offerings' own pitch lines, by offering, where one was written.
	Pitches map[int64]string
}

func (v Voice) pitchFor(offering int64) string {
	if p := v.Pitches[offering]; p != "" {
		return p
	}
	return v.Pitch
}

func (v Voice) callerFirst() string {
	if f := firstField(v.Caller); f != "" {
		return f
	}
	return "[YOUR NAME]"
}

func (v Voice) callerFull() string {
	if v.Caller != "" {
		return v.Caller
	}
	return "[YOUR NAME]"
}

func (v Voice) signed() string {
	if v.Signature == "" {
		return ""
	}
	return "\n\n" + v.Signature
}

type Badge struct{ Text, State string }

type Local struct{ Text, State, Why string }

type Line struct{ Cue, Say string }

type Card struct {
	Contact
	Name, Title, Status, Number, About, Tel, Text, Copy string
	State                                               string
	People                                              []Person
	Badges                                              []Badge
	Script                                              []Line
	SourceList                                          []string
	Next                                                int64
	Step                                                string
	Held                                                bool
	AgentMarks                                          map[string]string
}

var (
	switchboard = regexp.MustCompile(`(?i)switchboard|head office|main line|main office|HQ`)
	pressLine   = regexp.MustCompile(`(?i)\bpress\b`)
	directLine  = regexp.MustCompile(`(?i)direct`)
	parenthesis = regexp.MustCompile(`\s*\(.*?\)\s*`)
	afterDot    = regexp.MustCompile(` · .*`)
	numberCut   = regexp.MustCompile(`\s*[(/]`)
	aboutRe     = regexp.MustCompile(`\((.*)\)`)
	urlInAbout  = regexp.MustCompile(`;?\s*https?:\S+`)
	notTel      = regexp.MustCompile(`[^+\d]`)
)

func titleOf(people []Person, name string) string {
	for _, p := range people {
		if p.Name == name {
			return p.Title
		}
	}
	return ""
}

// forPerson re-addresses every message to whoever is chosen in "Send to".
func forPerson(c Contact, people []Person) Contact {
	to, first := deref(c.SendTo), deref(c.First)
	if to == "" || first == "" {
		return c
	}
	person := to
	if t := titleOf(people, to); t != "" {
		person += ", " + t
	}
	c.Person = &person
	toFirst := strings.Fields(to)[0]
	if toFirst == first {
		return c
	}
	word := regexp.MustCompile(`\b` + regexp.QuoteMeta(first) + `\b`)
	swap := func(p *string) *string {
		if p == nil {
			return nil
		}
		return str(word.ReplaceAllLiteralString(*p, toFirst))
	}
	c.First, c.Note, c.Body, c.Followup = &toFirst, swap(c.Note), swap(c.Body), swap(c.Followup)
	return c
}

var lunchLate = map[string]bool{"Europe/Rome": true, "Europe/Madrid": true, "Europe/Paris": true, "Europe/Monaco": true}

// localTime says whether now is a good time to ring someone in tz.
func localTime(tz string, now time.Time) *Local {
	if tz == "" {
		return nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil
	}
	t := now.In(loc)
	mins, day := t.Hour()*60+t.Minute(), t.Format("Mon")
	ls, le := 12*60, 13*60
	if lunchLate[tz] {
		ls, le = 12*60+30, 14*60+30
	}
	l := &Local{State: "ok", Why: "good time to call"}
	switch {
	case day == "Sat" || day == "Sun":
		l.State, l.Why = "stop", "weekend"
	case mins < 9*60 || mins >= 17*60+30:
		l.State, l.Why = "stop", "outside office hours"
	case mins >= ls && mins < le:
		l.State, l.Why = "warn", "lunchtime"
	case day == "Fri" && mins >= 15*60:
		l.State, l.Why = "warn", "Friday afternoon"
	case mins < 9*60+30:
		l.State, l.Why = "warn", "just started the day"
	}
	city := tz[strings.LastIndex(tz, "/")+1:]
	l.Text = fmt.Sprintf("%s %s in %s", day, t.Format("15:04"), strings.Replace(city, "_", " ", 1))
	return l
}

func badges(c Contact, name string, now time.Time, noMail map[string]bool) []Badge {
	var b []Badge
	phone := deref(c.Phone)
	if l := localTime(deref(c.Tz), now); l != nil {
		b = append(b, Badge{l.Text + ": " + l.Why, l.State})
	} else {
		b = append(b, Badge{"Time zone unknown: set it below", "warn"})
	}
	if strings.HasPrefix(strings.TrimSpace(phone), "+44") {
		b = append(b, Badge{"UK: check CTPS before calling", "warn"})
	}
	if switchboard.MatchString(phone) {
		b = append(b, Badge{"Switchboard: ask for " + name, ""})
	}
	if pressLine.MatchString(phone) && !directLine.MatchString(phone) {
		b = append(b, Badge{"Press line: ask who to speak to, don't pitch", "warn"})
	}
	if strings.Contains(deref(c.Flags), "INFERRED") {
		b = append(b, Badge{"Email may be a guess: verify", "warn"})
	}
	if dead := deadAddress(deref(c.Email), noMail); dead != "" {
		b = append(b, Badge{dead[strings.IndexByte(dead, '@')+1:] + " takes no mail: find another address", "warn"})
	}
	if role := roleAddress(deref(c.Email)); role != "" && !strings.HasPrefix(c.Key, "org:") {
		b = append(b, Badge{role + " is a shared inbox: ask for " + name + "'s own", ""})
	}
	if phoneLooksWrong(phone) {
		b = append(b, Badge{"Phone number isn't one any country uses: check it", "warn"})
	}
	if deref(c.Email) == "" {
		b = append(b, Badge{"No email on file", ""})
	}
	return b
}

func callScript(c Contact, name string, v Voice) []Line {
	maker := strings.TrimSpace(afterDot.ReplaceAllString(parenthesis.ReplaceAllString(deref(c.Maker), " "), ""))
	said := ""
	if parts := strings.Split(deref(c.Note), "\n\n"); len(parts) > 1 {
		said = parts[1]
		if r, size := utf8.DecodeRuneInString(said); size > 0 {
			said = string(unicode.ToUpper(r)) + said[size:]
		}
	}
	first := deref(c.First)
	from := v.callerFull() + " from " + v.Org
	var lines []Line
	if switchboard.MatchString(deref(c.Phone)) || pressLine.MatchString(deref(c.Phone)) {
		lines = append(lines, Line{"If someone else answers", "Hi, could I speak to " + name + ", please? It's " + from + "."})
	}
	why := said
	if c.LinkedinAdded != nil {
		why = strings.TrimSpace("I added you on LinkedIn the other day. " + said)
	}
	return append(lines,
		Line{"When they pick up", "Hi " + first + ", it's " + v.callerFirst() + " from " + v.Org + ". Have you got a couple of minutes?"},
		Line{"Why you're calling", why},
		Line{"What you do", fill(v.pitchFor(c.OfferingID), first, maker, v.Org)},
		Line{"Ask", "Who looks after that at " + maker + "? Is that you?"},
		Line{"If they're interested", "Would 20 minutes next week be worth it?"},
		Line{"Voicemail", "Hi " + first + ", it's " + from + ". I sent you a note on LinkedIn. I'll drop you a short email so you've got my details. Thanks, bye."},
	)
}

func status(c Contact) string {
	var parts []string
	for _, p := range []struct {
		label string
		v     *string
	}{{"On hold: ", c.Hold}, {"added ", c.LinkedinAdded}, {"accepted ", c.Accepted}, {"emailed ", c.EmailSent},
		{"followed up ", c.FollowupSent}, {"called ", c.Called}, {"replied ", c.Replied}, {"declined ", c.Declined}} {
		if p.v != nil && *p.v != "" {
			parts = append(parts, p.label+*p.v)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, " · ")
	}
	return "Not contacted yet"
}

var stateRank = map[string]int{"ok": 0, "warn": 1, "stop": 2}

// MakeCard turns one contact due at one step into what the page shows.
func MakeCard(step string, raw Contact, v Voice, now time.Time, noMail map[string]bool) Card {
	people := alternates(deref(raw.Person))
	c := forPerson(raw, people)
	name := displayName(c)
	title := titleOf(people, name)
	if title == "" {
		title = c.Title
	}
	card := Card{Contact: c, Step: step, Name: name, Title: title, People: people,
		Badges: badges(c, name, now, noMail), Held: c.Hold != nil, Status: status(c)}
	if l := localTime(deref(c.Tz), now); l != nil {
		card.State = l.State
	}
	for _, u := range strings.Split(deref(c.Sources), "\n") {
		if u != "" {
			card.SourceList = append(card.SourceList, u)
		}
	}
	switch step {
	case "call":
		phone := deref(c.Phone)
		card.Number = strings.TrimSpace(numberCut.Split(phone, 2)[0])
		card.Tel = notTel.ReplaceAllString(card.Number, "")
		if m := aboutRe.FindStringSubmatch(phone); m != nil {
			card.About = urlInAbout.ReplaceAllString(m[1], "")
		}
		card.Script = callScript(c, name, v)
	case "email":
		card.Text = deref(c.Body)
	case "followup":
		card.Text = deref(c.Followup)
	case "accepted":
		card.Text = deref(c.Message)
		if card.Text == "" {
			card.Text = deref(c.Body)
		}
	case "linkedin":
		card.Text = deref(c.Note)
	}
	card.Copy = card.Text
	if step == "email" || step == "followup" {
		card.Copy += v.signed()
	}
	return card
}

// todayRank puts next steps and calls that can be made now first, then messages,
// emails, follow-ups and adds, then calls outside office hours.
func todayRank(c Card) int {
	switch c.Step {
	case "next":
		return 0
	case "call":
		if c.State == "ok" {
			return 1
		}
		return 7 + stateRank[c.State]
	case "accepted":
		return 2
	case "email":
		return 3
	case "followup":
		return 4
	case "linkedin":
		return 5
	}
	return 9
}

// Today merges every step's queue into one list, with counts per step.
func Today(byStep map[string][]Contact, v Voice, now time.Time, noMail map[string]bool) ([]Card, map[string]int) {
	var cards []Card
	counts := map[string]int{}
	for _, s := range Steps {
		for _, c := range byStep[s.Key] {
			cards = append(cards, MakeCard(s.Key, c, v, now, noMail))
		}
		counts[s.Key] = len(byStep[s.Key])
	}
	sort.SliceStable(cards, func(i, j int) bool { return todayRank(cards[i]) < todayRank(cards[j]) })
	for i := range cards {
		for _, n := range cards[i+1:] {
			if n.Step == cards[i].Step {
				cards[i].Next = n.ID
				break
			}
		}
	}
	return cards, counts
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

// safeHref lets only web links through, so a stored javascript: link (from an agent
// or a teammate) can never run when clicked.
func safeHref(u string) string {
	if httpURL.MatchString(strings.TrimSpace(u)) {
		return strings.TrimSpace(u)
	}
	return "#"
}

// profileHref is where "Open + copy" goes: the stored profile, or a people search for
// someone added without one.
func profileHref(c Contact) string {
	if u := safeHref(deref(c.Linkedin)); u != "#" {
		return u
	}
	name := strings.SplitN(deref(c.Person), ",", 2)[0]
	words := strings.Join(strings.Fields(name+" "+deref(c.Maker)), " ")
	if words == "" {
		return "#"
	}
	return "https://www.linkedin.com/search/results/people/?keywords=" + strings.ReplaceAll(url.QueryEscape(words), "+", "%20")
}

func openLabel(c Contact) string {
	if safeHref(deref(c.Linkedin)) == "#" {
		return "Search + copy"
	}
	return "Open + copy"
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	switch {
	case word == "person":
		word = "people"
	case strings.HasSuffix(word, "y") && !strings.ContainsAny(word[len(word)-2:len(word)-1], "aeiou"):
		word = word[:len(word)-1] + "ies"
	default:
		word += "s"
	}
	return fmt.Sprintf("%d %s", n, word)
}

func actorLabel(actor string) string {
	if actor == customerActor {
		return "the customer"
	}
	if a, ok := strings.CutPrefix(actor, "agent:"); ok {
		return "agent · " + a
	}
	return actor
}
