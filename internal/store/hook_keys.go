package store

import "time"

// HookKeyOverlap is how long a replaced key still signs in, so a provider posting with
// it meanwhile isn't refused while its settings are changed over.
const HookKeyOverlap = 24 * time.Hour

// HookKey is one secret a provider posts to /hooks/{provider} with, an organisation's.
// Like the keys the server seals with, each has its own id and several can be live at
// once: a new one is added and the one it replaces lives on until ExpiresAt. A secret
// that is only ever compared is kept as its hash.
type HookKey struct {
	ID        int64      `gorm:"primaryKey"`
	OrgID     int64      `gorm:"index:idx_hook_keys_for,priority:1;not null"`
	Provider  string     `gorm:"index:idx_hook_keys_for,priority:2;not null"`
	Kid       string     `gorm:"not null"`
	Hash      string     `gorm:"not null"`
	CreatedAt time.Time  `gorm:"not null"`
	ExpiresAt *time.Time `gorm:"index"`
}

// Live is whether the key still signs in at now.
func (k HookKey) Live(now time.Time) bool { return k.ExpiresAt == nil || now.Before(*k.ExpiresAt) }
