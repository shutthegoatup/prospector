package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// NewBounceHook makes the organisation's webhook secret, replacing any it had, and
// gives it back the once.
func (s *Store) NewBounceHook(org int64) (string, error) {
	secret := randomToken(32)
	row := BounceHook{OrgID: org, SecretHash: hashToken(secret), CreatedAt: time.Now()}
	err := s.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "org_id"}}, DoUpdates: clause.AssignmentColumns([]string{"secret_hash", "created_at"})}).Create(&row).Error
	return secret, err
}

func (s *Store) DropBounceHook(org int64) error {
	return s.db.Delete(&BounceHook{}, "org_id = ?", org).Error
}

func (s *Store) HasBounceHook(org int64) bool {
	var n int64
	return s.db.Model(&BounceHook{}).Where("org_id = ?", org).Count(&n).Error == nil && n > 0
}

// BounceHookOrg is the organisation a webhook's sign-in is for: its slug and secret.
func (s *Store) BounceHookOrg(slug, secret string) (*Org, bool) {
	o, err := s.OrgBySlug(slug)
	if err != nil || secret == "" {
		return nil, false
	}
	var h BounceHook
	if found := s.db.Where("org_id = ?", o.ID).Limit(1).Find(&h); found.Error != nil || found.RowsAffected == 0 {
		return nil, false
	}
	return o, subtle.ConstantTimeCompare([]byte(hashToken(secret)), []byte(h.SecretHash)) == 1
}

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

// MailFailed puts an address on the list. A bounce is kept beside any reason it already
// had, and says so on the last mail that went to it; a complaint is about a mail that
// arrived, which stays sent.
func (s *Store) MailFailed(org int64, addr, reason, said string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if _, err := s.suppress(tx, org, addr, reason, bounceBy); err != nil {
			return err
		}
		if reason != SuppressBounced {
			return nil
		}
		if err := tx.Model(&Suppression{}).Where("org_id = ? and address = ?", org, addr).Update("bounced_at", time.Now()).Error; err != nil {
			return err
		}
		var last CampaignRecipient
		found := tx.Where("org_id = ? and address = ? and status = ?", org, addr, MailSent).Order("sent_at desc, id desc").Limit(1).Find(&last)
		if found.Error != nil || found.RowsAffected == 0 {
			return found.Error
		}
		return tx.Model(&CampaignRecipient{}).Where("org_id = ? and id = ?", org, last.ID).Updates(map[string]any{"status": MailFailed, "reason": said}).Error
	})
}
