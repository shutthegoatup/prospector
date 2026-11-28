package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
	"github.com/shutthegoatup/prospector/internal/tickets"
)

const (
	enquiryBodyLimit = 64 << 10
)

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
func (a *App) intakeOrigin(w http.ResponseWriter, r *http.Request) *store.Org {
	origin := r.Header.Get("Origin")
	org, err := a.s.OrgBySlug(r.PathValue("org"))
	if err != nil || origin == "" || !org.Settings.TakesEnquiriesFrom(origin) {
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
	var in tickets.EnquiryIn
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, enquiryBodyLimit)).Decode(&in); err != nil {
		fail(w, r, say.New("the enquiry couldn't be read"), http.StatusBadRequest)
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
		if errors.Is(err, tickets.ErrEnquiryEmail) {
			code = http.StatusBadRequest
		}
		fail(w, r, err, code)
		return
	}
	writeJSON(w, ok{true})
}
