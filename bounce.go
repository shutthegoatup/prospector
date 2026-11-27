package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"
)

// BounceHook is the secret Postmark signs its webhook in with, kept as its hash.
// Postmark doesn't sign what it posts, so the secret is all that says a bounce is real.
type BounceHook struct {
	OrgID      int64 `gorm:"primaryKey;autoIncrement:false"`
	SecretHash string
	CreatedAt  time.Time
}

const (
	SuppressBounced    = "bounced"
	SuppressComplained = "complained"
)

// bounceBy is who a bounced address is put on the list by.
const bounceBy = "Postmark"

var (
	errBounced    = errors.New("mail to that address bounced: it doesn't exist or won't take mail")
	errComplained = errors.New("they marked a mail as spam")
)

// postmarkBounce is the part of Postmark's bounce and spam complaint webhooks that is used.
type postmarkBounce struct {
	RecordType string `json:"RecordType"`
	Type       string `json:"Type"`
	Email      string `json:"Email"`
}

// permanent is whether the bounce says the address won't take mail at all. A soft
// bounce (a full mailbox, a server down) is left alone: the address may recover.
func (b postmarkBounce) permanent() bool {
	switch b.Type {
	case "HardBounce", "Blocked", "Suppressed":
		return true
	}
	return false
}

// postmarkHook takes Postmark's bounce webhook. It signs in with basic auth: the
// organisation's slug, and the secret it was given.
func (a *App) postmarkHook(w http.ResponseWriter, r *http.Request) {
	slug, secret, _ := r.BasicAuth()
	o, ok := a.s.BounceHookOrg(slug, secret)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="prospector"`)
		http.Error(w, "sign in with your organisation's slug and its bounce webhook secret", http.StatusUnauthorized)
		return
	}
	var b postmarkBounce
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&b); err != nil || b.RecordType == "" {
		http.Error(w, "that isn't a Postmark webhook", http.StatusBadRequest)
		return
	}
	var reason, said string
	switch {
	case b.RecordType == "Bounce" && b.permanent():
		reason, said = SuppressBounced, "it bounced ("+b.Type+")"
	case b.RecordType == "SpamComplaint":
		reason = SuppressComplained
	default:
		w.WriteHeader(http.StatusNoContent)
		return
	}
	addr, err := bareAddress(b.Email)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := a.s.MailFailed(o.ID, addr, reason, said); err != nil {
		log.Printf("recording a bounce for %s: %v", o.Slug, err)
		http.Error(w, "the bounce couldn't be recorded; Postmark will try again", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
