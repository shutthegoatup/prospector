package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"
)

// AgreementEvent is one thing that happened to an agreement once it went out: sent, a
// link opened, a code entered, signed, sealed. Rows are only ever added. Each carries
// the hash of the one before it, so a row changed or taken out afterwards shows. The
// certificate in a sealed PDF is drawn from these.
type AgreementEvent struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	OrgID       int64     `gorm:"index;not null" json:"-"`
	AgreementID int64     `gorm:"uniqueIndex:idx_agreement_event_chain,priority:1;not null" json:"agreement_id"`
	Round       int       `gorm:"not null;default:1" json:"round"`
	At          time.Time `gorm:"not null" json:"at"`
	Kind        string    `gorm:"not null;default:''" json:"kind"`
	Actor       string    `gorm:"not null;default:''" json:"actor"`
	RecipientID *int64    `json:"recipient_id"`
	Detail      string    `gorm:"not null;default:''" json:"detail"`
	IP          string    `gorm:"not null;default:''" json:"ip"`
	UserAgent   string    `gorm:"not null;default:''" json:"user_agent"`
	// Prev is the hash of the event before it on the same agreement, "" for the first.
	// One agreement can't have two events after the same one, so two writers racing to
	// add the next can't fork the chain: one of them is refused.
	Prev string `gorm:"uniqueIndex:idx_agreement_event_chain,priority:2;not null;default:''" json:"prev"`
	Hash string `gorm:"not null;default:''" json:"hash"`
}

const (
	EventSent         = "sent"
	EventLinkSent     = "link_sent"
	EventOpened       = "opened"
	EventCodeSent     = "code_sent"
	EventCodeEntered  = "code_entered"
	EventCodeWrong    = "code_wrong"
	EventDownloaded   = "downloaded"
	EventConsented    = "consented"
	EventSigned       = "signed"
	EventDeclined     = "declined"
	EventChangesAsked = "changes_asked"
	EventVoided       = "voided"
	EventExpired      = "expired"
	EventSealed       = "sealed"
	EventCopySent     = "copy_sent"
)

var errBrokenChain = errors.New("the agreement's record has been changed since it was written")

// eventHash is the SHA-256 of everything an event says, the hash before it included.
func eventHash(e AgreementEvent) string {
	recipient := int64(0)
	if e.RecipientID != nil {
		recipient = *e.RecipientID
	}
	// A fixed list in a fixed order: what is hashed can't change with how the struct does.
	b, _ := json.Marshal([]string{
		strconv.FormatInt(e.OrgID, 10), strconv.FormatInt(e.AgreementID, 10), strconv.Itoa(e.Round),
		e.At.UTC().Format(time.RFC3339Nano), e.Kind, e.Actor, strconv.FormatInt(recipient, 10),
		e.Detail, e.IP, e.UserAgent, e.Prev,
	})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// recordAgreementEvent adds an event after the last one on its agreement, inside the
// transaction that made it happen.
func recordAgreementEvent(tx *gorm.DB, e AgreementEvent) (*AgreementEvent, error) {
	// The agreement's row is the chain's lock: whoever adds next waits for this one, so
	// two writers can't both read the same last event and fork it.
	if err := tx.Exec("update agreements set updated_at = updated_at where org_id = ? and id = ?", e.OrgID, e.AgreementID).Error; err != nil {
		return nil, err
	}
	var last AgreementEvent
	err := tx.Where("org_id = ? and agreement_id = ?", e.OrgID, e.AgreementID).Order("id desc").Limit(1).Find(&last).Error
	if err != nil {
		return nil, err
	}
	e.ID, e.Prev = 0, last.Hash
	// Microseconds are what every database keeps, so the time hashed is the time read back.
	e.At = e.At.UTC().Truncate(time.Microsecond)
	if e.At.IsZero() {
		e.At = time.Now().UTC().Truncate(time.Microsecond)
	}
	e.Hash = eventHash(e)
	if err := tx.Create(&e).Error; err != nil {
		return nil, fmt.Errorf("recording what happened to agreement %d: %w", e.AgreementID, err)
	}
	return &e, nil
}

// AgreementEvents are an agreement's events in the order they happened, checked against
// their hashes.
func (s *Store) AgreementEvents(org, id int64) ([]AgreementEvent, error) {
	out := []AgreementEvent{}
	if err := s.in(org).Where("agreement_id = ?", id).Order("id").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, checkChain(out)
}

// checkChain is whether events, in order, each follow the one before and say what their
// hash says they did.
func checkChain(events []AgreementEvent) error {
	prev := ""
	for _, e := range events {
		if e.Prev != prev || eventHash(e) != e.Hash {
			return fmt.Errorf("agreement %d, event %d: %w", e.AgreementID, e.ID, errBrokenChain)
		}
		prev = e.Hash
	}
	return nil
}
