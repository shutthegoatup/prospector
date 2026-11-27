package store

import "time"

// MailDomain is what DNS last said about whether a domain takes mail. Only a
// definite answer is kept: a lookup that timed out says nothing either way.
type MailDomain struct {
	Domain    string    `gorm:"primaryKey"`
	TakesMail bool      `gorm:"not null;default:false"`
	CheckedAt time.Time `gorm:"not null"`
}
