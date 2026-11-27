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
