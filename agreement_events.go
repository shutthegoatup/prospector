package main

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/signing"
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
	e.Hash = signing.EventHash(e)
	if err := tx.Create(&e).Error; err != nil {
		return nil, fmt.Errorf("recording what happened to agreement %d: %w", e.AgreementID, err)
	}
	return &e, nil
}
