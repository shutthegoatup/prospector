package store

import (
	"time"

	"gorm.io/gorm"
)

// Delivery states.
const (
	DeliveryPending   = "pending"
	DeliveryDelivered = "delivered"
	DeliveryFailed    = "failed"
	DeliveryCancelled = "cancelled"
)

// DeliveryWindow is how long a delivery is tried for before it is given up.
const DeliveryWindow = 72 * time.Hour

// WebhookDelivery is one event on its way to one endpoint. It names the event rather
// than holding what is sent, which is read from the event when it goes.
type WebhookDelivery struct {
	ID         int64  `gorm:"primaryKey"`
	OrgID      int64  `gorm:"index;not null"`
	EndpointID int64  `gorm:"index;not null"`
	EventID    int64  `gorm:"not null"`
	Type       string `gorm:"not null"`
	// MessageID is the webhook-id every attempt carries, so a receiver can tell one it has seen.
	MessageID   string    `gorm:"uniqueIndex;not null"`
	Status      string    `gorm:"index:idx_webhook_deliveries_due,priority:1;not null;default:'pending'"`
	NextAt      time.Time `gorm:"index:idx_webhook_deliveries_due,priority:2;not null"`
	RetryUntil  time.Time `gorm:"not null"`
	Attempts    int       `gorm:"not null;default:0"`
	LastCode    int       `gorm:"not null;default:0"`
	LastError   string    `gorm:"not null;default:''"`
	DeliveredAt *time.Time
	CreatedAt   time.Time `gorm:"not null"`
}

// queueWebhooks puts an event on its way to every endpoint that asked for it and whose
// key may read it, in the transaction that recorded it, so it goes only if that commits.
// With no such endpoint it writes nothing.
func queueWebhooks(tx *gorm.DB, e *Event) error {
	kind := EventTypeOf(e.Action)
	if kind == "" {
		return nil
	}
	return tx.Exec(`insert into webhook_deliveries (org_id, endpoint_id, event_id, type, message_id, status, next_at, retry_until, attempts, last_code, last_error, created_at)
		select w.org_id, w.id, ?, ?, 'msg_' || replace(gen_random_uuid()::text, '-', ''), ?, now(), now() + make_interval(secs => ?), 0, 0, '', now()
		from webhook_endpoints w join api_keys k on k.id = w.key_id and k.org_id = w.org_id
		where w.org_id = ? and w.disabled_at is null and k.revoked_at is null
			and position(' read ' in ' ' || k.scopes || ' ') > 0 and position(' ' || ? || ' ' in ' ' || w.events || ' ') > 0`,
		e.ID, kind, DeliveryPending, DeliveryWindow.Seconds(), e.OrgID, kind).Error
}
