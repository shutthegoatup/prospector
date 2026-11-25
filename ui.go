package main

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

type Nav struct {
	*Req
	Active                                    string
	Today, Research, Conversations, Companies int
}

func (a *App) dueByStep(q *Req, now time.Time) map[string][]Contact {
	out := map[string][]Contact{}
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
	n.Research = a.s.ResearchCounts(q.Org.ID)["review"]
	convs, _ := a.s.Conversations(q.Org.ID)
	for k, list := range convs {
		if k != "won" && k != "lost" {
			n.Conversations += len(list)
		}
	}
	var companies int64
	a.s.in(q.Org.ID).Model(&Company{}).Count(&companies)
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
	Week      Stats
	Now       string
}

func (a *App) today(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	now := time.Now()
	cards, counts := Today(a.dueByStep(q, now), q.Voice(), now)
	d := TodayData{Filter: r.URL.Query().Get("step"), Counts: counts, Total: len(cards),
		Review: a.s.ResearchCounts(q.Org.ID)["review"], Week: a.s.Week(q.Org.ID, now), Now: now.Format("Mon 2 Jan · 15:04")}
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

func (a *App) research(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	f := r.URL.Query().Get("f")
	if f == "" {
		f = "review"
	}
	rows, err := a.s.Research(q.Org.ID, f)
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	a.page(w, r, "research", ResearchView(q, f, rows, a.s.ResearchCounts(q.Org.ID)))
}

func (a *App) conversations(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	convs, err := a.s.Conversations(q.Org.ID)
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	a.page(w, r, "conversations", ConversationsView(q, convs, time.Now().Format("2006-01-02")))
}

type CompanyFilter struct{ Q, Segment, Stage string }

func (a *App) companies(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	f := CompanyFilter{r.URL.Query().Get("q"), r.URL.Query().Get("segment"), r.URL.Query().Get("stage")}
	rows, err := a.s.Companies(q.Org.ID, f.Q, f.Segment, f.Stage)
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	a.page(w, r, "companies", CompaniesView(q, f, rows, a.s.Segments(q.Org.ID)))
}

type CompanyPage struct {
	Company Company
	Facts   []Fact
	People  []Contact
	History []HistoryItem
	Notes   []NoteView
}

func (a *App) company(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	c, err := a.s.CompanyByID(q.Org.ID, idParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p := CompanyPage{Company: *c}
	p.Facts, _ = a.s.Facts(q.Org.ID, c.ID)
	p.People, _ = a.s.People(q.Org.ID, c.ID)
	p.History, _ = a.s.History(q.Org.ID, c.ID, 50)
	notes, _ := a.s.CompanyNotes(q.Org.ID, c.ID, 20)
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
}

func (a *App) settings(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	members, _ := a.s.Members(q.Org.ID)
	d := SettingsData{Members: members, Me: *q.Member, Covered: a.s.NeverContactCovers(q.Org), Intake: a.baseURL(r) + q.Base() + "intake",
		Mail: a.mailView(q.Org.ID)}
	d.Sealing = a.s.keys != nil
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
	Keys     []APIKey
	Activity []AgentEvent
	Runs     []Run
	Tasks    map[int64]string
	Now      time.Time
	Scores   []AgentScore
}

func (a *App) agents(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	keys, _ := a.s.Keys(q.Org.ID)
	runs, tasks := a.s.Runs(q.Org.ID, 12)
	a.page(w, r, "agents", AgentsView(q, AgentsData{Base: a.baseURL(r) + q.Base(), Keys: keys, Activity: a.s.AgentActivity(q.Org.ID, 20),
		Scores: a.s.AgentScores(q.Org.ID), Runs: runs, Tasks: tasks, Now: time.Now()}))
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

func (a *App) uiUpdate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID    int64  `json:"id"`
		Field string `json:"field"`
		Value string `json:"value"`
	}
	if decode(w, r, &in) {
		q := reqOf(r)
		done(w, a.s.Update(q.Org.ID, in.ID, in.Field, in.Value, q.Actor()))
	}
}

func (a *App) uiCompany(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID    int64  `json:"id"`
		Stage string `json:"stage"`
		Fit   string `json:"fit"`
	}
	if decode(w, r, &in) {
		q := reqOf(r)
		done(w, a.s.SetCompanyStage(q.Org.ID, in.ID, in.Stage, in.Fit, q.Actor()))
	}
}

func (a *App) uiPick(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID int64 `json:"id"`
	}
	if decode(w, r, &in) {
		q := reqOf(r)
		done(w, a.s.PickContact(q.Org.ID, q.Org, in.ID, q.Actor()))
	}
}

func (a *App) uiAdd(w http.ResponseWriter, r *http.Request) {
	var in QuickAdd
	if !decode(w, r, &in) {
		return
	}
	q := reqOf(r)
	c, err := a.s.QuickAddPerson(q.Org, in, q.Actor())
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	where := "is in the LinkedIn queue"
	if in.Added {
		where = "is marked added; their email comes up in " + plural(q.Org.Settings.withDefaults().EmailAfterDays, "day")
	}
	writeJSON(w, map[string]string{"message": displayName(*c) + " " + where + "."})
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

func (a *App) uiSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Color string `json:"color"`
		OrgSettings
	}
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		fail(w, errors.New("the organisation needs a name"), http.StatusBadRequest)
		return
	}
	if in.EmailAfterDays < 0 || in.FollowupAfterDays < 0 || in.EmailAfterDays > 60 || in.FollowupAfterDays > 60 {
		fail(w, errors.New("waits are between 0 and 60 days"), http.StatusBadRequest)
		return
	}
	if _, err := enquiryOrigins(in.EnquirySites); err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	if _, err := in.Assets(); err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	o := reqOf(r).Org
	o.Name, o.Color, o.Settings = strings.TrimSpace(in.Name), in.Color, in.OrgSettings
	err := a.s.SaveOrg(o)
	if err == nil {
		a.dropMCPServers()
	}
	done(w, err)
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

func (a *App) uiRevoke(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID int64 `json:"id"`
	}
	if decode(w, r, &in) {
		done(w, a.s.RevokeKey(reqOf(r).Org.ID, in.ID))
	}
}
