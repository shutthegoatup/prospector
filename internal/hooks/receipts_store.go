package hooks

import (
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/store"
)

// ReceiptStore remembers which events providers posted.
type ReceiptStore struct{ *store.Core }

// Receive runs handle for an event once, however often it is posted: it says so with
// seen when the event was taken before. The receipt is written in a transaction held
// open while handle runs, so the same event posted twice at once waits for the first,
// and a handle that fails leaves no receipt, so the provider's next try is taken.
func (s *ReceiptStore) Receive(org int64, provider, event string, handle func() error) (seen bool, err error) {
	now := time.Now()
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(`insert into hook_receipts (org_id, provider, event_id, received_at) values (?, ?, ?, ?)
			on conflict (org_id, provider, event_id) do nothing`, org, provider, event, now)
		if res.Error != nil {
			return res.Error
		}
		if seen = res.RowsAffected == 0; seen {
			return nil
		}
		if err := handle(); err != nil {
			return err
		}
		return tx.Where("org_id = ? and provider = ? and received_at < ?", org, provider, now.Add(-store.HookReceiptsKept)).Delete(&store.HookReceipt{}).Error
	})
	return seen && err == nil, err
}
