package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// Enquiry is something a person sent us: a website form, or a product telling us
// about a sign-up. It is kept as it arrived, and pointed at the contact and company
// it matches when there is one. Its text is the sender's own and is never trusted.
type Enquiry struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	OrgID     int64     `gorm:"index;not null" json:"-"`
	Kind      string    `gorm:"not null;default:'other'" json:"kind"`
	Name      string    `json:"name"`
	Email     string    `gorm:"index" json:"email"`
	Company   string    `json:"company"`
	Phone     string    `json:"phone"`
	Message   string    `json:"message"`
	Site      string    `json:"site"`
	Page      string    `json:"page"`
	Via       string    `gorm:"not null;default:'form'" json:"via"`
	Spam      bool      `gorm:"not null;default:false" json:"spam"`
	ContactID *int64    `gorm:"index" json:"contact_id"`
	CompanyID *int64    `gorm:"index" json:"company_id"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
	// State is where answering it stands; empty for one that arrived before enquiries
	// were tracked, which nobody is asked to answer now.
	State string `gorm:"not null;default:''" json:"state"`
	// Owner is the teammate answering it.
	Owner string `gorm:"not null;default:''" json:"owner"`
	// Note is ours, about answering it. Nothing sends it, but it is held against whoever
	// wrote in, so it is in their export when they ask what is held on them.
	Note string `gorm:"not null;default:''" json:"note"`
}

const (
	EnquiryOpen    = "open"
	EnquiryWaiting = "waiting"
	EnquiryClosed  = "closed"
)

var EnquiryStates = []struct{ Key, Label string }{
	{EnquiryOpen, "To answer"}, {EnquiryWaiting, "Waiting on them"}, {EnquiryClosed, "Closed"},
}

func enquiryStateLabel(state string) string {
	for _, s := range EnquiryStates {
		if s.Key == state {
			return s.Label
		}
	}
	return ""
}

// SetEnquiry changes where answering an enquiry stands, who has it, or our note on it.
func (s *Store) SetEnquiry(org, id int64, field, value, actor string) error {
	value = strings.TrimSpace(value)
	return s.db.Transaction(func(tx *gorm.DB) error {
		var e Enquiry
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&e).Error; err != nil {
			return missing(err, "no enquiry %d", id)
		}
		if e.Spam {
			return errors.New("that was marked as spam, so there is nothing to answer")
		}
		logged := value
		switch field {
		case "state":
			if enquiryStateLabel(value) == "" {
				return fmt.Errorf("unknown state %q", value)
			}
		case "owner":
			if value != "" && value != e.Owner {
				var n int64
				if err := tx.Table("memberships").Joins("join users on users.id = memberships.user_id").
					Where("memberships.org_id = ? and users.name = ?", org, value).Count(&n).Error; err != nil {
					return err
				}
				if n == 0 {
					return fmt.Errorf("%q isn't a member of this organisation", value)
				}
			}
		case "note":
			value, logged = clip(value, 2000), ""
		default:
			return fmt.Errorf("an enquiry has no %q to set", field)
		}
		if err := tx.Model(&e).Update(field, value).Error; err != nil {
			return err
		}
		// As when it arrived: history is a company's, so one matched to none has none.
		if e.CompanyID == nil {
			return nil
		}
		var contact int64
		if e.ContactID != nil {
			contact = *e.ContactID
		}
		return s.log(tx, org, contact, e.CompanyID, actor, "enquiry:"+field, nonEmpty(logged))
	})
}

var EnquiryKinds = []struct{ Key, Label string }{
	{"sales", "Sales"}, {"demo", "Demo"}, {"signup", "Sign-up"}, {"support", "Help"}, {"feature", "Feature request"}, {"other", "Other"},
}

func enquiryKind(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	for _, known := range EnquiryKinds {
		if known.Key == k {
			return k
		}
	}
	return "other"
}

func kindLabel(k string) string {
	for _, known := range EnquiryKinds {
		if known.Key == k {
			return known.Label
		}
	}
	return k
}

type EnquiryIn struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Company string `json:"company"`
	Phone   string `json:"phone"`
	Message string `json:"message"`
	Page    string `json:"page"`
	Site    string `json:"site"`
	// Trap is a field no person sees. A form filler that completes it is kept, but as spam.
	Trap string `json:"trap"`
}

const (
	enquiryBodyLimit = 64 << 10
	enquiryRepeat    = 10 * time.Minute
)

// clip keeps an over-long field's start rather than refusing the enquiry.
func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}

var errEnquiryEmail = errors.New("an email address is needed, so we can answer")

// AddEnquiry stores one enquiry. The same thing sent again within a few minutes is
// the same enquiry, so a form can retry until it hears back without making copies.
func (s *Store) AddEnquiry(org int64, via string, in EnquiryIn) (*Enquiry, error) {
	e := Enquiry{OrgID: org, Kind: enquiryKind(in.Kind), Name: clip(in.Name, 200), Email: strings.ToLower(clip(in.Email, 320)),
		Company: clip(in.Company, 200), Phone: clip(in.Phone, 50), Message: clip(in.Message, 10000),
		Site: clip(in.Site, 200), Page: clip(in.Page, 500), Via: via, Spam: strings.TrimSpace(in.Trap) != ""}
	if !e.Spam {
		e.State = EnquiryOpen
	}
	if !emailRe.MatchString(e.Email) || strings.ContainsAny(e.Email, " \t\r\n") {
		return nil, errEnquiryEmail
	}
	return &e, s.db.Transaction(func(tx *gorm.DB) error {
		if err := holdOrg(tx, org); err != nil {
			return err
		}
		var same Enquiry
		err := tx.Where("org_id = ? and email = ? and kind = ? and message = ? and created_at > ?",
			org, e.Email, e.Kind, e.Message, time.Now().Add(-enquiryRepeat)).First(&same).Error
		if err == nil {
			e = same
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var c Contact
		if tx.Where("org_id = ? and lower(email) = ? and key not like 'org:%'", org, e.Email).First(&c).Error == nil {
			e.ContactID, e.CompanyID = &c.ID, c.CompanyID
		}
		if _, domain, _ := strings.Cut(e.Email, "@"); e.CompanyID == nil {
			var co Company
			if tx.Where("org_id = ? and lower(domain) = ?", org, domain).First(&co).Error == nil {
				e.CompanyID = &co.ID
			}
		}
		if err := tx.Create(&e).Error; err != nil {
			return err
		}
		if e.Spam || e.CompanyID == nil {
			return nil
		}
		var contact int64
		if e.ContactID != nil {
			contact = *e.ContactID
		}
		return s.log(tx, org, contact, e.CompanyID, via, "enquiry", str(kindLabel(e.Kind)))
	})
}

// DeleteEnquiry removes one for good: spam, or a person asking for what they sent to go.
func (s *Store) DeleteEnquiry(org, id int64) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Where("org_id = ? and id = ?", org, id).Delete(&Enquiry{})
		if res.Error == nil && res.RowsAffected == 0 {
			return noSuch("no enquiry %d", id)
		}
		if res.Error != nil {
			return res.Error
		}
		return tx.Where("org_id = ? and enquiry_id = ?", org, id).Delete(&EnquiryMessage{}).Error
	})
}

type EnquiryFilter struct {
	Kind string
	Spam bool
	// State narrows to one state of answering, when set.
	State string
}

// enquirySpamParam is the filter's spam choice as the page's link carries it.
func enquirySpamParam(f EnquiryFilter) string {
	if f.Spam {
		return "1"
	}
	return ""
}

const enquiryPageSize = 50

// Enquiries lists one page of the enquiries a filter matches, newest first.
func (s *Store) Enquiries(org int64, f EnquiryFilter, page int) ([]Enquiry, Page, error) {
	matching := func() *gorm.DB {
		q := s.in(org).Model(&Enquiry{}).Where("spam = ?", f.Spam)
		if f.Kind != "" {
			q = q.Where("kind = ?", f.Kind)
		}
		if f.State != "" {
			q = q.Where("state = ?", f.State)
		}
		return q
	}
	var total int64
	if err := matching().Count(&total).Error; err != nil {
		return nil, Page{}, err
	}
	pg := pageOf(page, enquiryPageSize, int(total))
	var rows []Enquiry
	return rows, pg, matching().Order("created_at desc, id desc").Limit(pg.Size).Offset(pg.offset()).Find(&rows).Error
}

// EnquiryCounts is how many enquiries there are of each kind, with spam under "spam"
// and each state of answering under "state:" and its name.
func (s *Store) EnquiryCounts(org int64) map[string]int {
	var rows []struct {
		Kind, State string
		Spam        bool
		N           int
	}
	logged(s.in(org).Model(&Enquiry{}).Select("kind, state, spam, count(*) as n").Group("kind, state, spam").Scan(&rows))
	out := map[string]int{}
	for _, r := range rows {
		if r.Spam {
			out["spam"] += r.N
			continue
		}
		out[r.Kind] += r.N
		out[""] += r.N
		if r.State != "" {
			out["state:"+r.State] += r.N
		}
	}
	return out
}

// enquiryOrigins reads the setting: the sites whose forms may post, one per line.
func enquiryOrigins(setting string) ([]string, error) {
	var out []string
	for _, line := range strings.FieldsFunc(setting, func(r rune) bool { return r == '\n' || r == ',' || r == ' ' || r == '\r' }) {
		u, err := url.Parse(line)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return nil, fmt.Errorf("%q isn't a site address: it should look like https://www.example.com", line)
		}
		out = append(out, strings.ToLower(u.Scheme+"://"+u.Host))
	}
	return out, nil
}

func (set OrgSettings) takesEnquiriesFrom(origin string) bool {
	sites, _ := enquiryOrigins(set.EnquirySites)
	for _, site := range sites {
		if site == strings.ToLower(origin) {
			return true
		}
	}
	return false
}

const (
	enquiriesPerSender = 5
	enquiriesPerOrg    = 300
	enquiryWindow      = time.Hour
)

// enquiryGuard bounds what the open form route will store. Behind a proxy every
// request shares an address, so it counts per sender email and per organisation.
type enquiryGuard struct {
	mu   sync.Mutex
	seen map[string]*enquiryCount
	now  func() time.Time
}

type enquiryCount struct {
	n     int
	since time.Time
}

func (g *enquiryGuard) allow(key string, limit int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if g.now != nil {
		now = g.now()
	}
	if len(g.seen) >= maxTracked {
		for k, c := range g.seen {
			if now.Sub(c.since) > enquiryWindow {
				delete(g.seen, k)
			}
		}
	}
	c := g.seen[key]
	if c == nil && len(g.seen) >= maxTracked {
		return false
	}
	if c == nil || now.Sub(c.since) > enquiryWindow {
		if g.seen == nil {
			g.seen = map[string]*enquiryCount{}
		}
		c = &enquiryCount{since: now}
		g.seen[key] = c
	}
	c.n++
	return c.n <= limit
}

const intakePattern = "/o/{org}/intake"

// intakeOrigin answers for a site allowed to post this organisation's enquiries, and
// refuses any other. A browser enforces it; anything else could claim an origin, which
// is why the route can only add enquiries and is bounded by enquiryGuard.
func (a *App) intakeOrigin(w http.ResponseWriter, r *http.Request) *Org {
	origin := r.Header.Get("Origin")
	org, err := a.s.OrgBySlug(r.PathValue("org"))
	if err != nil || origin == "" || !org.Settings.takesEnquiriesFrom(origin) {
		http.Error(w, "this site isn't set up to send enquiries here", http.StatusForbidden)
		return nil
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Add("Vary", "Origin")
	return org
}

func (a *App) intakePreflight(w http.ResponseWriter, r *http.Request) {
	if a.intakeOrigin(w, r) == nil {
		return
	}
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "content-type")
	w.Header().Set("Access-Control-Max-Age", "86400")
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) intake(w http.ResponseWriter, r *http.Request) {
	org := a.intakeOrigin(w, r)
	if org == nil {
		return
	}
	var in EnquiryIn
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, enquiryBodyLimit)).Decode(&in); err != nil {
		fail(w, errors.New("the enquiry couldn't be read"), http.StatusBadRequest)
		return
	}
	in.Site = r.Header.Get("Origin")
	orgKey := fmt.Sprintf("%d", org.ID)
	if !a.enquiries.allow(orgKey+"\x00"+loginKey(in.Email), enquiriesPerSender) || !a.enquiries.allow(orgKey, enquiriesPerOrg) {
		// The longest it can be: the count starts again an hour after its first enquiry.
		w.Header().Set("Retry-After", strconv.Itoa(int(enquiryWindow.Seconds())))
		http.Error(w, "too many enquiries just now; please email us instead", http.StatusTooManyRequests)
		return
	}
	if _, err := a.s.AddEnquiry(org.ID, "form", in); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, errEnquiryEmail) {
			code = http.StatusBadRequest
		}
		fail(w, err, code)
		return
	}
	writeJSON(w, ok{true})
}

func (a *App) uiSetEnquiry(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID    int64  `json:"id"`
		Field string `json:"field"`
		Value string `json:"value"`
	}
	if decode(w, r, &in) {
		q := reqOf(r)
		done(w, a.s.SetEnquiry(q.Org.ID, in.ID, in.Field, in.Value, q.Actor()))
	}
}

func (a *App) uiDeleteEnquiry(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID int64 `json:"id"`
	}
	if decode(w, r, &in) {
		done(w, a.s.DeleteEnquiry(reqOf(r).Org.ID, in.ID))
	}
}

type EnquiriesData struct {
	Filter    EnquiryFilter
	Rows      []Enquiry
	Page      Page
	Counts    map[string]int
	Companies map[int64]string
	Members   []Member
	// Talking are the contacts, among those who sent these, already on the Conversations board.
	Talking map[int64]bool
	// Threads is what has been said in answering each, and MailOn whether a reply can be sent from here.
	Threads map[int64][]EnquiryMessage
	MailOn  bool
}

func (a *App) enquiriesPage(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	f := EnquiryFilter{Kind: r.URL.Query().Get("kind"), Spam: r.URL.Query().Get("spam") != "", State: r.URL.Query().Get("state")}
	rows, pg, err := a.s.Enquiries(q.Org.ID, f, pageParam(r))
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	var ids []int64
	for _, e := range rows {
		if e.CompanyID != nil {
			ids = append(ids, *e.CompanyID)
		}
	}
	names := map[int64]string{}
	if len(ids) > 0 {
		var companies []Company
		logged(a.s.in(q.Org.ID).Where("id in ?", ids).Find(&companies))
		for _, c := range companies {
			names[c.ID] = c.Name
		}
	}
	members, err := a.s.Members(q.Org.ID)
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	talking := map[int64]bool{}
	var senders []int64
	for _, e := range rows {
		if e.ContactID != nil {
			senders = append(senders, *e.ContactID)
		}
	}
	if len(senders) > 0 {
		var on []int64
		if err := a.s.in(q.Org.ID).Model(&Contact{}).Where("id in ?", senders).Where(inConversation).Pluck("id", &on).Error; err != nil {
			fail(w, err, http.StatusInternalServerError)
			return
		}
		for _, id := range on {
			talking[id] = true
		}
	}
	threads, err := a.s.EnquiryThreads(q.Org.ID, enquiryIDs(rows))
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	via, _ := a.mailFor(q.Org.ID)
	a.page(w, r, "enquiries", EnquiriesView(q, EnquiriesData{Filter: f, Rows: rows, Page: pg, Counts: a.s.EnquiryCounts(q.Org.ID), Companies: names, Members: members, Talking: talking, Threads: threads, MailOn: via != nil}))
}

// ConverseEnquiry puts whoever sent an enquiry on the Conversations board: writing in is
// a reply to nothing we sent, but it is them talking to us, which is what the board is
// for. Someone not held yet is added first, under the never-contact and erasure rules
// that adding anyone by hand goes through.
func (s *Store) ConverseEnquiry(o *Org, id int64, actor string) (*Contact, error) {
	var e Enquiry
	if err := s.in(o.ID).Where("id = ?", id).First(&e).Error; err != nil {
		return nil, missing(err, "no enquiry %d", id)
	}
	if e.Spam {
		return nil, errors.New("that was marked as spam, so there is nobody to talk to")
	}
	var c Contact
	if e.ContactID != nil {
		if err := s.in(o.ID).Where("id = ?", *e.ContactID).First(&c).Error; err != nil {
			return nil, missing(err, "no contact %d", *e.ContactID)
		}
	} else {
		// Whoever holds that address now: they may have been added since this arrived,
		// by hand or from another of their enquiries.
		found := s.in(o.ID).Where("lower(email) = ? and key not like 'org:%'", e.Email).Order("id").Limit(1).Find(&c)
		if found.Error != nil {
			return nil, found.Error
		}
		if found.RowsAffected == 0 {
			company, err := s.enquiryCompany(o.ID, e)
			if err != nil {
				return nil, err
			}
			added, err := s.QuickAddPerson(o, QuickAdd{Name: senderName(e), Company: company, Email: e.Email}, actor)
			if err != nil {
				return nil, err
			}
			c = *added
		}
		// Matched now, so their export and erasure take this enquiry with them.
		if err := s.in(o.ID).Model(&Enquiry{}).Where("id = ?", e.ID).
			Updates(map[string]any{"contact_id": c.ID, "company_id": c.CompanyID}).Error; err != nil {
			return nil, err
		}
	}
	// Someone put on the never-contact list since they were matched stays off the board.
	if err := s.allowed(o, deref(c.Email), deref(c.Linkedin)); err != nil {
		return nil, err
	}
	if c.Declined != nil {
		return nil, errors.New("they are marked as not interested, which keeps them off the board")
	}
	if c.Replied == nil {
		if err := s.Mark(o.ID, c.ID, "replied", today(), actor); err != nil {
			return nil, err
		}
	}
	if deref(c.CallNotes) == "" && e.Message != "" {
		// What they asked, on the card. It is their own text, shown as text like the rest.
		if err := s.Update(o.ID, c.ID, "call_notes", "Wrote in: "+clip(e.Message, 500), actor); err != nil {
			return nil, err
		}
	}
	return &c, nil
}

// senderName is the name on an enquiry as a contact's: one line, short enough to be a
// name. The form's sender chose every character, and a contact's name is read by
// agents, so a paragraph of text isn't taken as one.
func senderName(e Enquiry) string {
	name := clip(strings.Join(strings.Fields(e.Name), " "), 80)
	if name == "" {
		name, _, _ = strings.Cut(e.Email, "@")
	}
	return name
}

// enquiryCompany is the company to put an enquiry's sender under. The one their
// address matched is used. The name they typed is used only when no company here has
// it: a form can claim to be from anyone, and that claim mustn't attach a stranger to
// a company already held. Otherwise they are added with none.
func (s *Store) enquiryCompany(org int64, e Enquiry) (string, error) {
	if e.CompanyID != nil {
		var co Company
		if err := s.in(org).Where("id = ?", *e.CompanyID).First(&co).Error; err != nil {
			return "", missing(err, "no company %d", *e.CompanyID)
		}
		return co.Name, nil
	}
	typed := clip(strings.Join(strings.Fields(e.Company), " "), 80)
	if typed == "" {
		return "", nil
	}
	var held int64
	if err := s.in(org).Model(&Company{}).Where("lower(name) = ?", strings.ToLower(typed)).Count(&held).Error; err != nil {
		return "", err
	}
	if held > 0 {
		return "", nil
	}
	return typed, nil
}

func (a *App) uiConverseEnquiry(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID int64 `json:"id"`
	}
	if decode(w, r, &in) {
		q := reqOf(r)
		_, err := a.s.ConverseEnquiry(q.Org, in.ID, q.Actor())
		done(w, err)
	}
}
