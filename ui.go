package main

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Nav struct {
	*Req
	Active                                    string
	Today, Research, Conversations, Companies int
	// Enquiries is how many are waiting for an answer from us.
	Enquiries int
}

func (a *App) dueByStep(q *Req, now time.Time) map[string][]Contact {
	if q.due != nil {
		return q.due
	}
	out := map[string][]Contact{}
	q.due = out
	for _, s := range Steps {
		if s.Key == "next" {
			continue
		}
		out[s.Key], _ = a.s.Due(q.Org.ID, s.Key, now, q.Org.Settings)
	}
	convs, _ := a.s.Conversations(q.Org.ID)
	day := now.Format("2006-01-02")
	for _, list := range convs {
		for _, c := range list {
			if NeedsNextStep(c, day) {
				out["next"] = append(out["next"], c)
			}
		}
	}
	return out
}

func (a *App) nav(q *Req, active string) Nav {
	n := Nav{Req: q, Active: active}
	for _, list := range a.dueByStep(q, time.Now()) {
		n.Today += len(list)
	}
	n.Research = a.s.ResearchCount(q.Org.ID, "review")
	n.Enquiries = a.s.EnquiryCounts(q.Org.ID)["state:"+EnquiryOpen]
	convs, _ := a.s.Conversations(q.Org.ID)
	for k, list := range convs {
		if k != "won" && k != "lost" {
			n.Conversations += len(list)
		}
	}
	others, _ := a.s.OtherDeals(q.Org.ID)
	for k, list := range others {
		if k != "won" && k != "lost" {
			n.Conversations += len(list)
		}
	}
	var companies int64
	logged(a.s.in(q.Org.ID).Model(&Company{}).Count(&companies))
	n.Companies = int(companies)
	return n
}

// todayPageSize keeps the page light; the cards come best first, so the rest can wait.
const todayPageSize = 60

type TodayData struct {
	Filter    string
	Cards     []Card
	Shown     int
	Counts    map[string]int
	Total     int
	Review    int
	LaterCall int
	// Deals are the renewals and other further deals with no next step, or one that's due.
	// Like companies to review, they are worked elsewhere, so they aren't in Total.
	Deals int
	// Subs are the subscriptions with a day to cancel by, a renewal or an end coming soon.
	Subs int
	// Enquiries are the ones nobody has answered yet.
	Enquiries int
	// Steps are the onboarding steps late or due today, ours and the customers alike.
	Steps int
	Week  Stats
	Now   string
}

func (a *App) today(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	now := time.Now()
	voice := q.Voice()
	offerings, err := a.s.Offerings(q.Org.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	voice.Pitches = map[int64]string{}
	for _, o := range offerings {
		voice.Pitches[o.ID] = o.Pitch
	}
	cards, counts := Today(a.dueByStep(q, now), voice, now, a.s.noMailDomains())
	d := TodayData{Filter: r.URL.Query().Get("step"), Counts: counts, Total: len(cards),
		Review: a.s.ResearchCount(q.Org.ID, "review"), Week: a.s.Week(q.Org.ID, now), Now: now.Format("Mon 2 Jan · 15:04")}
	others, err := a.s.OtherDeals(q.Org.ID)
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	for _, list := range others {
		for _, o := range list {
			if o.needsNextStep(now.Format("2006-01-02")) {
				d.Deals++
			}
		}
	}
	subs, err := a.s.Subscriptions(q.Org.ID, 0, now.Format(dayLayout))
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	d.Enquiries = a.s.EnquiryCounts(q.Org.ID)["state:"+EnquiryOpen]
	onboard, err := a.s.Onboardings(q.Org.ID, 0, now.Format(dayLayout))
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	for _, ob := range onboard {
		d.Steps += ob.Late + ob.DueToday
	}
	for _, sub := range subs {
		if sub.Soon(now.Format(dayLayout)) {
			d.Subs++
		}
	}
	for _, c := range cards {
		if c.Step == "call" && c.State != "ok" {
			d.LaterCall++
		}
		if d.Filter == "" || d.Filter == c.Step {
			d.Shown++
			if len(d.Cards) < todayPageSize {
				d.Cards = append(d.Cards, c)
			}
		}
	}
	ids := make([]int64, len(d.Cards))
	for i, c := range d.Cards {
		ids[i] = c.ID
	}
	marks := a.s.AgentMarks(q.Org.ID, ids)
	for i := range d.Cards {
		d.Cards[i].AgentMarks = marks[d.Cards[i].ID]
	}
	a.page(w, r, "today", TodayView(q, d))
}

// ResearchData is the Research page: the searches agents are on, the queue they fill,
// and, when one search is chosen, what it has come to.
type ResearchData struct {
	Filter string
	Rows   []ResearchRow
	// Ranked is the queue in order of fit rather than newest first.
	Ranked bool
	Page   Page
	Counts map[string]int
	// Searches are the open tasks, newest first, with their agents' runs.
	Searches []SearchView
	// Search is the one the queue is narrowed to; nil for all of it.
	Search *SearchView
	Notes  []NoteView
	// Agents are Prospector's agents ready to run; LookFor what Settings says agents look for.
	Agents  []Agent
	LookFor []Asset
}

// SearchView is a task seen from Research: what it found and who is on it.
type SearchView struct {
	TaskView
	Going []AgentRun
	Ended []AgentRun
}

const searchesShown = 5

func (a *App) searchView(org int64, t TaskView) SearchView {
	v := SearchView{TaskView: t}
	runs, err := a.s.AgentRuns(org, t.ID, 3)
	if err != nil {
		log.Print(err)
	}
	for _, run := range runs {
		if run.Going() {
			v.Going = append(v.Going, run)
		} else {
			v.Ended = append(v.Ended, run)
		}
	}
	return v
}

func (a *App) research(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	now := time.Now()
	d := ResearchData{Filter: r.URL.Query().Get("f")}
	if d.Filter == "" {
		d.Filter = "review"
	}
	var task int64
	if id, err := strconv.ParseInt(r.URL.Query().Get("task"), 10, 64); err == nil && id > 0 {
		t, err := a.s.Task(q.Org.ID, id, now)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		task = t.ID
		v := a.searchView(q.Org.ID, *t)
		d.Search = &v
		notes, _, err := a.s.TaskNotesPage(q.Org.ID, t.ID, 1)
		if err != nil {
			log.Print(err)
		}
		d.Notes = notes[:min(len(notes), 3)]
	}
	var err error
	if task != 0 && d.Filter == "review" {
		d.Rows, d.Page, d.Ranked, err = a.s.rankedResearch(q.Org.ID, q.Org.Settings, task, pageParam(r))
	} else {
		d.Rows, d.Page, err = a.s.researchFor(q.Org.ID, d.Filter, pageParam(r), task)
	}
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	d.Counts = a.s.researchCountsFor(q.Org.ID, task)
	open, _, err := a.s.Tasks(q.Org.ID, false, 1, now)
	if err != nil {
		log.Print(err)
	}
	for _, t := range open[:min(len(open), searchesShown)] {
		d.Searches = append(d.Searches, a.searchView(q.Org.ID, t))
	}
	if agents, err := a.s.Agents(q.Org.ID); err == nil {
		for _, c := range agents {
			if c.Ready() {
				d.Agents = append(d.Agents, c)
			}
		}
	}
	d.LookFor, _ = q.Org.Settings.Assets()
	a.page(w, r, "research", ResearchView(q, d))
}

func (a *App) conversations(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	convs, err := a.s.Conversations(q.Org.ID)
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	deals, err := a.s.Deals(q.Org.ID)
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	members, err := a.s.Members(q.Org.ID)
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	others, err := a.s.OtherDeals(q.Org.ID)
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	after := AfterWin{Started: map[int64]bool{}, Running: map[int64]bool{}}
	if plan, err := q.Org.Settings.Onboarding(); err == nil {
		after.Planned = len(plan)
	}
	started, err := a.s.Onboardings(q.Org.ID, 0, today())
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	for _, ob := range started {
		after.Started[ob.CompanyID] = true
		if !ob.Live() {
			after.Running[ob.CompanyID] = true
		}
	}
	a.page(w, r, "conversations", ConversationsView(q, Board{convs, deals, others, members, after}, time.Now().Format("2006-01-02")))
}

type CompanyFilter struct{ Q, Segment, Stage string }

func pageParam(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("page"))
	return n
}

// panelPage is which page of one list a page shows, where it shows several: each has
// its own name in the address.
func panelPage(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return n
}

// panelLink is the address for page n of the list called name, keeping the page's
// other list, if it has one, where it is.
func panelLink(path, name string, n int, otherName string, other Page) string {
	past := func(n int) string {
		if n > 1 {
			return strconv.Itoa(n)
		}
		return ""
	}
	return pageLink(path, 0, name, past(n), otherName, past(other.N))
}

// pageHref is the address of page n of the list called name, keeping everything else
// the address says: where the page's other lists are, and its filters.
func pageHref(path string, query url.Values, name string, n int) string {
	v := url.Values{}
	for key, values := range query {
		v[key] = values
	}
	v.Del(name)
	if n > 1 {
		v.Set(name, strconv.Itoa(n))
	}
	if len(v) == 0 {
		return path
	}
	return path + "?" + v.Encode()
}

// pageSlice is page n of a list already read whole.
func pageSlice[T any](all []T, n, size int) ([]T, Page) {
	pg := pageOf(n, size, len(all))
	from := min(pg.offset(), len(all))
	return all[from:min(from+size, len(all))], pg
}

func pageLink(path string, page int, filters ...string) string {
	v := url.Values{}
	for i := 0; i+1 < len(filters); i += 2 {
		if filters[i+1] != "" {
			v.Set(filters[i], filters[i+1])
		}
	}
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	if len(v) == 0 {
		return path
	}
	return path + "?" + v.Encode()
}

func (a *App) companies(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	f := CompanyFilter{r.URL.Query().Get("q"), r.URL.Query().Get("segment"), r.URL.Query().Get("stage")}
	rows, pg, err := a.s.Companies(q.Org.ID, f.Q, f.Segment, f.Stage, pageParam(r))
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	a.page(w, r, "companies", CompaniesView(q, f, rows, pg, a.s.Segments(q.Org.ID)))
}

type CompanyPage struct {
	Company Company
	Facts   []Fact
	People  []Contact
	Pending map[int64][]PendingDetail
	History []HistoryItem
	Notes   []NoteView
	// HistoryPage and NotesPage are where each of those two lists is, and Touches counts
	// the whole history, not the page of it on show.
	HistoryPage, NotesPage Page
	Touches                int
	Subs                   []SubscriptionRow
	Agreements             []Agreement
	Onboard                []OnboardingRow
	Planned                int
	Day                    string
}

func (a *App) company(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	c, err := a.s.CompanyByID(q.Org.ID, idParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p := CompanyPage{Company: *c}
	// Half a company's page would read as the whole of it, so any part failing fails the page.
	var failed [9]error
	p.Onboard, failed[6] = a.s.Onboardings(q.Org.ID, c.ID, today())
	if failed[6] == nil {
		failed[6] = a.customerControls(q.Org.ID, p.Onboard)
	}
	if plan, err := q.Org.Settings.Onboarding(); err == nil {
		p.Planned = len(plan)
	}
	p.Day = today()
	p.Subs, failed[5] = a.s.Subscriptions(q.Org.ID, c.ID, p.Day)
	p.Agreements, failed[8] = a.s.Agreements(q.Org.ID, c.ID)
	p.Facts, failed[0] = a.s.Facts(q.Org.ID, c.ID)
	p.People, failed[1] = a.s.People(q.Org.ID, c.ID)
	p.Pending, failed[2] = a.s.Pending(q.Org.ID, p.People)
	p.History, p.HistoryPage, failed[3] = a.s.History(q.Org.ID, c.ID, panelPage(r, "history"))
	p.Touches, failed[7] = a.s.Touches(q.Org.ID, c.ID)
	notes, notesPage, err := a.s.CompanyNotesPage(q.Org.ID, c.ID, panelPage(r, "notes"))
	p.NotesPage, failed[4] = notesPage, err
	if err := errors.Join(failed[:]...); err != nil {
		log.Printf("company %d: %v", c.ID, err)
		http.Error(w, "the company couldn't be read", http.StatusInternalServerError)
		return
	}
	for _, n := range notes {
		p.Notes = append(p.Notes, NoteView{TaskNote: n})
	}
	a.page(w, r, "companies", CompanyView(q, p))
}

func (a *App) addPage(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	v := r.URL.Query()
	name, role, company := ParseLinkedIn(v.Get("name"), v.Get("headline"))
	if c := v.Get("company"); c != "" {
		company = c
	}
	in := QuickAdd{Linkedin: strings.SplitN(v.Get("url"), "?", 2)[0], Name: name, Title: role, Company: company}
	popup := v.Get("popup") != ""
	if popup {
		w.Header().Set("content-type", "text/html; charset=utf-8")
		AddPopup(q, in).Render(r.Context(), w)
		return
	}
	a.page(w, r, "add", AddView(q, in, bookmarklet(a.baseURL(r), q)))
}

// bookmarklet sends the LinkedIn profile you're looking at to the add form. It reads only
// the page's title and headline, on your click.
func bookmarklet(site string, q *Req) string {
	base := site + q.Base() + "add?popup=1"
	return `javascript:(()=>{const h=(document.querySelector('.text-body-medium')||{}).innerText||'';` +
		`window.open('` + base + `&url='+encodeURIComponent(location.href)+'&name='+encodeURIComponent(document.title)+'&headline='+encodeURIComponent(h),'prospect','width=520,height=760')})()`
}

func (a *App) baseURL(r *http.Request) string {
	if a.base != "" {
		return a.base
	}
	scheme := "http"
	if a.secure(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

type SettingsData struct {
	Members []Member
	Me      Membership
	Covered []string
	Mail    MailView
	SSO     *SSO
	// Callback is where a provider sends people back; empty until PROSPECTOR_URL is set.
	Callback string
	// Sealing is whether the server has a key to keep a client secret under.
	Sealing bool
	Intake  string
	// Suppressed are the newest addresses not to be mailed, and MoreSuppressed whether
	// there are others.
	Suppressed     []Suppression
	MoreSuppressed bool
}

func (a *App) settings(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	members, _ := a.s.Members(q.Org.ID)
	d := SettingsData{Members: members, Me: *q.Member, Covered: a.s.NeverContactCovers(q.Org), Intake: a.baseURL(r) + q.Base() + "intake",
		Mail: a.mailView(q.Org.ID)}
	d.Sealing = a.s.keys != nil
	var next string
	var err error
	if d.Suppressed, next, err = a.s.Suppressions(q.Org.ID, PageIn{}); err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	d.MoreSuppressed = next != ""
	if d.SSO, _ = a.s.SSO(q.Org.ID); d.SSO == nil {
		d.SSO = &SSO{}
	}
	if a.base != "" {
		d.Callback = a.callbackURL()
	}
	a.page(w, r, "settings", SettingsView(q, d))
}

type AgentsData struct {
	Base     string
	Own      OwnAgentsView
	Keys     []APIKey
	Activity []AgentEvent
	// ActivityPage is where the list of activity is.
	ActivityPage Page
	Runs         []Run
	// RunsPaged is a page of runs other than the latest; OlderRuns links the page after
	// it, and is empty on the last.
	RunsPaged bool
	OlderRuns string
	Tasks     map[int64]string
	Now       time.Time
	Scores    []AgentScore
}

const runsShown = 12

// olderRuns is the address of the runs before the last on this page, keeping the
// page of activity.
func olderRuns(q *Req, before time.Time, r *http.Request) string {
	if before.IsZero() {
		return ""
	}
	return pageLink(q.Base()+"agents", 0, "activity", r.URL.Query().Get("activity"), "runs_before", strconv.FormatInt(before.UnixNano(), 10))
}

func (a *App) agents(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	keys, _ := a.s.Keys(q.Org.ID)
	var before time.Time
	if n, err := strconv.ParseInt(r.URL.Query().Get("runs_before"), 10, 64); err == nil && n > 0 {
		before = time.Unix(0, n)
	}
	runs, tasks, older := a.s.Runs(q.Org.ID, before, runsShown)
	activity, activityPage := a.s.AgentActivity(q.Org.ID, panelPage(r, "activity"))
	a.page(w, r, "agents", AgentsView(q, AgentsData{Base: a.baseURL(r) + q.Base(), Own: a.ownAgents(q.Org.ID), Keys: keys, Activity: activity, ActivityPage: activityPage,
		Scores: a.s.AgentScores(q.Org.ID), Runs: runs, Tasks: tasks, Now: time.Now(), RunsPaged: !before.IsZero(), OlderRuns: olderRuns(q, older, r)}))
}

func done(w http.ResponseWriter, err error) {
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) uiMark(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID   int64  `json:"id"`
		Step string `json:"step"`
	}
	if decode(w, r, &in) {
		q := reqOf(r)
		done(w, a.s.Mark(q.Org.ID, in.ID, in.Step, today(), q.Actor()))
	}
}

func (a *App) uiVoice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CallerName string `json:"caller_name"`
		Signature  string `json:"signature"`
	}
	if decode(w, r, &in) {
		q := reqOf(r)
		done(w, a.s.SaveVoice(q.Org.ID, q.User.ID, in.CallerName, in.Signature))
	}
}

func (a *App) uiMember(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
		Name  string `json:"name"`
		Role  string `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Role != "owner" && in.Role != "member" {
		in.Role = "member"
	}
	q := reqOf(r)
	u, err := a.s.UserByEmail(in.Email)
	password := ""
	if err != nil {
		password = randomToken(12)
		u, err = a.s.InviteUser(in.Email, in.Name, password)
	}
	if err == nil {
		err = a.s.AddMember(q.Org.ID, u.ID, in.Role, u.Name)
	}
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	msg := u.Email + " added to " + q.Org.Name + "."
	// Someone who never chose a password is sent the link again, so a lost one isn't the end.
	if password != "" || u.MustChangePassword {
		sent, err := a.invite(r.Context(), q, u, password != "")
		if sent {
			writeJSON(w, map[string]string{"message": msg + " They've been emailed a link to choose their password; it lasts 7 days."})
			return
		}
		if errors.Is(err, errTooSoon) {
			msg += " They were emailed a link to choose their password a moment ago, so no other was sent."
		} else if err != nil {
			msg += " The invitation couldn't be emailed (" + err.Error() + ")."
		}
	} else if !u.HasPassword() {
		msg += " They have no password: they sign in through a provider, so they can open this organisation only by one it accepts."
	}
	if password != "" {
		msg += " Their first password, shown once: " + password + " (they choose their own when they first sign in)"
	}
	writeJSON(w, map[string]string{"message": msg})
}

func (a *App) uiRemoveMember(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID int64 `json:"user_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	q := reqOf(r)
	if _, err := a.s.RemoveMember(q.Org.ID, in.UserID, q.Actor()); err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	if in.UserID == q.User.ID {
		writeJSON(w, map[string]string{"go": "/"})
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) uiExport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID int64 `json:"id"`
	}
	if !decode(w, r, &in) {
		return
	}
	q := reqOf(r)
	out, err := a.s.ExportPerson(q.Org.ID, in.ID, q.Actor())
	if err != nil {
		fail(w, err, http.StatusNotFound)
		return
	}
	w.Header().Set("cache-control", "no-store")
	writeJSON(w, out)
}

func (a *App) uiExportOrg(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if !decode(w, r, &in) {
		return
	}
	q := reqOf(r)
	out, err := a.s.ExportOrg(q.Org.ID, q.Actor())
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("cache-control", "no-store")
	writeJSON(w, out)
}

func (a *App) uiErase(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID int64 `json:"id"`
	}
	if decode(w, r, &in) {
		q := reqOf(r)
		done(w, a.s.ErasePerson(q.Org.ID, in.ID, q.Actor()))
	}
}

func (a *App) uiKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if !decode(w, r, &in) {
		return
	}
	q := reqOf(r)
	secret, _, err := a.s.CreateKey(q.Org.ID, in.Name, in.Scopes, q.Actor())
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"message": "Copy this key now; it isn't shown again: " + secret, "secret": secret})
}

func (a *App) uiKeyScopes(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID     int64    `json:"id"`
		Scopes []string `json:"scopes"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := a.s.SetKeyScopes(reqOf(r).Org.ID, in.ID, in.Scopes); err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"message": "Saved ✓ Agents connected over MCP have to reconnect."})
}

func (a *App) uiMemberRole(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID int64  `json:"user_id"`
		Role   string `json:"role"`
	}
	if decode(w, r, &in) {
		done(w, a.s.SetRole(reqOf(r).Org.ID, in.UserID, in.Role))
	}
}

func (a *App) uiRevoke(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID int64 `json:"id"`
	}
	if decode(w, r, &in) {
		done(w, a.s.RevokeKey(reqOf(r).Org.ID, in.ID))
	}
}
