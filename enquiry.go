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
	if !emailRe.MatchString(e.Email) || strings.ContainsAny(e.Email, " \t\r\n") {
		return nil, errEnquiryEmail
	}
	return &e, s.db.Transaction(func(tx *gorm.DB) error {
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
	res := s.in(org).Where("id = ?", id).Delete(&Enquiry{})
	if res.Error == nil && res.RowsAffected == 0 {
		return noSuch("no enquiry %d", id)
	}
	return res.Error
}

type EnquiryFilter struct {
	Kind string
	Spam bool
}

const enquiryPageSize = 200

func (s *Store) Enquiries(org int64, f EnquiryFilter) ([]Enquiry, error) {
	q := s.in(org).Where("spam = ?", f.Spam)
	if f.Kind != "" {
		q = q.Where("kind = ?", f.Kind)
	}
	var rows []Enquiry
	return rows, q.Order("created_at desc, id desc").Limit(enquiryPageSize).Find(&rows).Error
}

// EnquiryCounts is how many enquiries there are of each kind, with spam under "spam".
func (s *Store) EnquiryCounts(org int64) map[string]int {
	var rows []struct {
		Kind string
		Spam bool
		N    int
	}
	s.in(org).Model(&Enquiry{}).Select("kind, spam, count(*) as n").Group("kind, spam").Scan(&rows)
	out := map[string]int{}
	for _, r := range rows {
		if r.Spam {
			out["spam"] += r.N
			continue
		}
		out[r.Kind] += r.N
		out[""] += r.N
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
	Counts    map[string]int
	Companies map[int64]string
}

func (a *App) enquiriesPage(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	f := EnquiryFilter{Kind: r.URL.Query().Get("kind"), Spam: r.URL.Query().Get("spam") != ""}
	rows, err := a.s.Enquiries(q.Org.ID, f)
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
		a.s.in(q.Org.ID).Where("id in ?", ids).Find(&companies)
		for _, c := range companies {
			names[c.ID] = c.Name
		}
	}
	a.page(w, r, "enquiries", EnquiriesView(q, EnquiriesData{Filter: f, Rows: rows, Counts: a.s.EnquiryCounts(q.Org.ID), Companies: names}))
}
