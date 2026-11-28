package webhooks

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/shutthegoatup/prospector/internal/store"
)

// Due is a delivery claimed to be sent now, with what sending it needs.
type Due struct {
	store.WebhookDelivery
	Endpoint store.WebhookEndpoint
	Event    store.Event
	Slug     string
	// Live is whether it may still go: its endpoint there and not stopped, its key not
	// revoked and still able to read, its event not erased.
	Live bool
}

// ClaimDue claims up to n deliveries whose time has come, across every organisation,
// for lease: another server leaves them alone meanwhile, and one that stops before it
// settles them has them sent again after it.
func (s *WebhookStore) ClaimDue(now time.Time, lease time.Duration, n int) ([]Due, error) {
	var claimed []store.WebhookDelivery
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? and next_at <= ?", store.DeliveryPending, now).Order("next_at").Limit(n).Find(&claimed).Error; err != nil {
			return err
		}
		if len(claimed) == 0 {
			return nil
		}
		ids := make([]int64, len(claimed))
		for i, d := range claimed {
			ids[i] = d.ID
		}
		return tx.Model(&store.WebhookDelivery{}).Where("id in ?", ids).
			Updates(map[string]any{"next_at": now.Add(lease), "attempts": gorm.Expr("attempts + 1")}).Error
	})
	if err != nil || len(claimed) == 0 {
		return nil, err
	}
	out := make([]Due, 0, len(claimed))
	for _, d := range claimed {
		d.Attempts++
		due := Due{WebhookDelivery: d}
		var org store.Org
		err := s.DB.Where("org_id = ? and id = ?", d.OrgID, d.EndpointID).Limit(1).Find(&due.Endpoint).Error
		if err == nil {
			err = s.DB.Where("org_id = ? and id = ?", d.OrgID, d.EventID).Limit(1).Find(&due.Event).Error
		}
		if err == nil {
			err = s.DB.Where("id = ?", d.OrgID).Limit(1).Find(&org).Error
		}
		if err != nil {
			return nil, err
		}
		due.Slug = org.Slug
		var k store.APIKey
		keyOK := due.Endpoint.ID != 0 && s.DB.Where("org_id = ? and id = ? and revoked_at is null", d.OrgID, due.Endpoint.KeyID).Limit(1).Find(&k).Error == nil && k.ID != 0 && k.Has("read")
		due.Live = keyOK && due.Endpoint.DisabledAt == nil && due.Event.ID != 0
		out = append(out, due)
	}
	return out, nil
}

func (s *WebhookStore) settle(org, id int64, updates map[string]any) error {
	return s.DB.Model(&store.WebhookDelivery{}).Where("org_id = ? and id = ?", org, id).Updates(updates).Error
}

// Delivered records a delivery the endpoint took.
func (s *WebhookStore) Delivered(org, id int64, code int, at time.Time) error {
	return s.settle(org, id, map[string]any{"status": store.DeliveryDelivered, "last_code": code, "last_error": "", "delivered_at": at})
}

// Retry records an attempt that failed and when the next is.
func (s *WebhookStore) Retry(org, id int64, code int, why string, next time.Time) error {
	return s.settle(org, id, map[string]any{"last_code": code, "last_error": why, "next_at": next})
}

// Cancel drops a delivery that may no longer go.
func (s *WebhookStore) Cancel(org, id int64, why string) error {
	return s.settle(org, id, map[string]any{"status": store.DeliveryCancelled, "last_error": why})
}

// GiveUp fails a delivery for good and stops its endpoint, for why, which the owner sees.
func (s *WebhookStore) GiveUp(org, id, endpoint int64, code int, why string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&store.WebhookDelivery{}).Where("org_id = ? and id = ?", org, id).
			Updates(map[string]any{"status": store.DeliveryFailed, "last_code": code, "last_error": why}).Error; err != nil {
			return err
		}
		res := tx.Model(&store.WebhookEndpoint{}).Where("org_id = ? and id = ? and disabled_at is null", org, endpoint).
			Updates(map[string]any{"disabled_at": time.Now(), "disabled_why": why})
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		return s.Log(tx, org, 0, nil, "Prospector", store.ActEndpointSet, &why)
	})
}

// Deliveries are an endpoint's deliveries, newest first.
func (s *WebhookStore) Deliveries(org, endpoint int64) ([]store.WebhookDelivery, error) {
	if _, err := s.Webhook(org, endpoint); err != nil {
		return nil, err
	}
	var out []store.WebhookDelivery
	return out, s.InOrg(org).Where("endpoint_id = ?", endpoint).Order("id desc").Limit(500).Find(&out).Error
}
