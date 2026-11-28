package main

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/onboarding"
	"github.com/shutthegoatup/prospector/internal/privacy"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
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

// Badge, Local and Line are words for the member, in their language where Today is sent.
type Badge struct {
	Text  say.Sayer
	State string
}

type Local struct {
	Text  say.Sayer
	State string
}

type Line struct{ Cue, Say say.Sayer }

var months = map[time.Month]*say.Text{
	time.January: say.New("Jan"), time.February: say.New("Feb"), time.March: say.New("Mar"), time.April: say.New("Apr"),
	time.May: say.New("May"), time.June: say.New("Jun"), time.July: say.New("Jul"), time.August: say.New("Aug"),
	time.September: say.New("Sep"), time.October: say.New("Oct"), time.November: say.New("Nov"), time.December: say.New("Dec"),
}

// at is a moment as the server writes one for a person: "2 Jan 15:04".
func at(t time.Time) say.Sayer {
	return say.Errorf("%d %s %s", t.Day(), months[t.Month()], t.Format("15:04"))
}

// onDate is a day as the server writes one for a person: "2 Jan 2006".
func onDate(t time.Time) say.Sayer {
	return say.Errorf("%d %s %d", t.Day(), months[t.Month()], t.Year())
}

var weekdays = map[string]*say.Text{
	"Mon": say.New("Mon"), "Tue": say.New("Tue"), "Wed": say.New("Wed"), "Thu": say.New("Thu"),
	"Fri": say.New("Fri"), "Sat": say.New("Sat"), "Sun": say.New("Sun"),
}

type Card struct {
	Contact
	Name, Title, Status, Number, About, Tel, Text, Copy string
	State                                               string
	People                                              []privacy.Person
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

func titleOf(people []privacy.Person, name string) string {
	for _, p := range people {
		if p.Name == name {
			return p.Title
		}
	}
	return ""
}

// forPerson re-addresses every message to whoever is chosen in "Send to".
func forPerson(c Contact, people []privacy.Person) Contact {
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
	state, why := "ok", say.New("good time to call")
	switch {
	case day == "Sat" || day == "Sun":
		state, why = "stop", say.New("weekend")
	case mins < 9*60 || mins >= 17*60+30:
		state, why = "stop", say.New("outside office hours")
	case mins >= ls && mins < le:
		state, why = "warn", say.New("lunchtime")
	case day == "Fri" && mins >= 15*60:
		state, why = "warn", say.New("Friday afternoon")
	case mins < 9*60+30:
		state, why = "warn", say.New("just started the day")
	}
	city := strings.Replace(tz[strings.LastIndex(tz, "/")+1:], "_", " ", 1)
	return &Local{Text: say.Errorf("%s %s in %s: %s", weekdays[day], t.Format("15:04"), city, why), State: state}
}

func badges(c Contact, name string, now time.Time, noMail map[string]bool) []Badge {
	var b []Badge
	phone := deref(c.Phone)
	if l := localTime(deref(c.Tz), now); l != nil {
		b = append(b, Badge{l.Text, l.State})
	} else {
		b = append(b, Badge{say.New("Time zone unknown: set it below"), "warn"})
	}
	if strings.HasPrefix(strings.TrimSpace(phone), "+44") {
		b = append(b, Badge{say.New("UK: check CTPS before calling"), "warn"})
	}
	if switchboard.MatchString(phone) {
		b = append(b, Badge{say.Errorf("Switchboard: ask for %s", name), ""})
	}
	if pressLine.MatchString(phone) && !directLine.MatchString(phone) {
		b = append(b, Badge{say.New("Press line: ask who to speak to, don't pitch"), "warn"})
	}
	if strings.Contains(deref(c.Flags), "INFERRED") {
		b = append(b, Badge{say.New("Email may be a guess: verify"), "warn"})
	}
	if dead := deadAddress(deref(c.Email), noMail); dead != "" {
		b = append(b, Badge{say.Errorf("%s takes no mail: find another address", dead[strings.IndexByte(dead, '@')+1:]), "warn"})
	}
	if role := roleAddress(deref(c.Email)); role != "" && !strings.HasPrefix(c.Key, "org:") {
		b = append(b, Badge{say.Errorf("%s is a shared inbox: ask for %s's own", role, name), ""})
	}
	if phoneLooksWrong(phone) {
		b = append(b, Badge{say.New("Phone number isn't one any country uses: check it"), "warn"})
	}
	if deref(c.Email) == "" {
		b = append(b, Badge{say.New("No email on file"), ""})
	}
	return b
}

func callScript(c Contact, name string, v request.Voice) []Line {
	maker := strings.TrimSpace(afterDot.ReplaceAllString(parenthesis.ReplaceAllString(deref(c.Maker), " "), ""))
	said := ""
	if parts := strings.Split(deref(c.Note), "\n\n"); len(parts) > 1 {
		said = parts[1]
		if r, size := utf8.DecodeRuneInString(said); size > 0 {
			said = string(unicode.ToUpper(r)) + said[size:]
		}
	}
	first := deref(c.First)
	var lines []Line
	if switchboard.MatchString(deref(c.Phone)) || pressLine.MatchString(deref(c.Phone)) {
		lines = append(lines, Line{say.New("If someone else answers"),
			say.Errorf("Hi, could I speak to %s, please? It's %s from %s.", name, v.CallerFull(), v.Org)})
	}
	// What the connection note said is the member's own words.
	var why say.Sayer = say.New(said)
	if c.LinkedinAdded != nil {
		why = say.New("I added you on LinkedIn the other day.")
		if said != "" {
			why = say.Errorf("I added you on LinkedIn the other day. %s", said)
		}
	}
	return append(lines,
		Line{say.New("When they pick up"), say.Errorf("Hi %s, it's %s from %s. Have you got a couple of minutes?", first, v.CallerFirst(), v.Org)},
		Line{say.New("Why you're calling"), why},
		// The pitch is the organisation's own.
		Line{say.New("What you do"), say.New(crm.Fill(v.PitchFor(c.OfferingID), first, maker, v.Org))},
		Line{say.New("Ask"), say.Errorf("Who looks after that at %s? Is that you?", maker)},
		Line{say.New("If they're interested"), say.New("Would 20 minutes next week be worth it?")},
		Line{say.New("Voicemail"), say.Errorf("Hi %s, it's %s from %s. I sent you a note on LinkedIn. I'll drop you a short email so you've got my details. Thanks, bye.",
			first, v.CallerFull(), v.Org)},
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
func MakeCard(step string, raw Contact, v request.Voice, now time.Time, noMail map[string]bool) Card {
	people := privacy.Alternates(deref(raw.Person))
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
		card.Copy += v.Signed()
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
func Today(byStep map[string][]Contact, v request.Voice, now time.Time, noMail map[string]bool) ([]Card, map[string]int) {
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

// safeHref lets only web links through, so a stored javascript: link (from an agent
// or a teammate) can never run when clicked.
func safeHref(u string) string {
	if crm.HttpURL.MatchString(strings.TrimSpace(u)) {
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

func openLabel(c Contact) *say.Text {
	if safeHref(deref(c.Linkedin)) == "#" {
		return say.New("Search + copy")
	}
	return say.New("Open + copy")
}

func actorLabel(actor string) say.Sayer {
	if actor == onboarding.CustomerActor {
		return say.New("the customer")
	}
	if a, ok := strings.CutPrefix(actor, "agent:"); ok {
		return say.Errorf("agent · %s", a)
	}
	return say.New(actor)
}
