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

type EnquiryFilter struct {
	Kind string
	Spam bool
	// State narrows to one state of answering, when set.
	State string
}

const enquiryPageSize = 50

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

// enquiriesAround is what is shown beside a page of enquiries: the companies they
// matched, which senders are on the Conversations board, and what has been said since.
type enquiriesAround struct {
	Companies map[int64]string
	Talking   map[int64]bool
	Threads   map[int64][]EnquiryMessage
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
