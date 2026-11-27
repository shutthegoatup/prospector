package store

import "time"

// EnquiryMessage is one turn in answering an enquiry: a reply we emailed from here, or
// what they said back, which someone pasted in because their answer arrives in a
// mailbox and not here. It belongs to its enquiry and goes wherever that goes.
type EnquiryMessage struct {
	ID        int64 `gorm:"primaryKey" json:"id"`
	OrgID     int64 `gorm:"index;not null" json:"-"`
	EnquiryID int64 `gorm:"index;not null" json:"enquiry_id"`
	// Ours is true for a reply we sent, false for what they said.
	Ours bool   `gorm:"not null;default:false" json:"ours"`
	Text string `gorm:"not null;default:''" json:"text"`
	By   string `gorm:"not null;default:''" json:"by"`
	// SentAt is when a reply of ours left. One still without it was never sent, and is
	// not part of the thread.
	SentAt    *time.Time `json:"sent_at"`
	CreatedAt time.Time  `json:"created_at"`
}
