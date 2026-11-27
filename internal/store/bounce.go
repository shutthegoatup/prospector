package store

import "time"

// BounceHook is the secret Postmark signs its webhook in with, kept as its hash.
// Postmark doesn't sign what it posts, so the secret is all that says a bounce is real.
type BounceHook struct {
	OrgID      int64 `gorm:"primaryKey;autoIncrement:false"`
	SecretHash string
	CreatedAt  time.Time
}
