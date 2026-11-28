package store

import "time"

// HookReceiptsKept is how long what a provider posted is remembered, past which a
// provider no longer sends it again.
const HookReceiptsKept = 30 * 24 * time.Hour

// HookReceipt says one event a provider posted was taken, by the id the provider gave
// it, so the same event posted again is taken once. Only the id is kept.
type HookReceipt struct {
	ID         int64     `gorm:"primaryKey"`
	OrgID      int64     `gorm:"uniqueIndex:idx_hook_receipts_event,priority:1;not null"`
	Provider   string    `gorm:"uniqueIndex:idx_hook_receipts_event,priority:2;not null"`
	EventID    string    `gorm:"uniqueIndex:idx_hook_receipts_event,priority:3;not null"`
	ReceivedAt time.Time `gorm:"index;not null"`
}
