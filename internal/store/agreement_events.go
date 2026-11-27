package store

import "time"

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
