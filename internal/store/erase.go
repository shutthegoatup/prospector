package store

import "time"

// Erasure is what stays behind when a person is erased: hashes of what identified
// them, so an import, a restore or an agent can't bring them back. The hash is salted
// with the organisation, so the same address erased in two can't be matched up.
type Erasure struct {
	ID    int64  `gorm:"primaryKey"`
	OrgID int64  `gorm:"uniqueIndex:idx_erasure;not null"`
	Hash  string `gorm:"uniqueIndex:idx_erasure;not null"`
	At    time.Time
}
