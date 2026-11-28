package marketing

import (
	"crypto/subtle"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/shutthegoatup/prospector/internal/store"
)

// postmark is the provider the bounce hook's keys are for.
const postmark = "postmark"

// NewBounceHook adds a secret for the organisation's bounce webhook and gives it back
// the once. One it had already goes on signing in for store.HookKeyOverlap, so bounces
// Postmark posts while its settings are changed over aren't refused.
func (s *OutreachStore) NewBounceHook(org int64, actor string) (string, error) {
	secret := store.RandomToken(32)
	now := time.Now()
	return secret, s.DB.Transaction(func(tx *gorm.DB) error {
		if err := store.HoldOrg(tx, org); err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and provider = ? and expires_at <= ?", org, postmark, now).Delete(&store.HookKey{}).Error; err != nil {
			return err
		}
		// A hook made before the key set has only its bounce_hooks row: it joins the set,
		// so it gets its day of overlap too.
		var n int64
		if err := tx.Model(&store.HookKey{}).Where("org_id = ? and provider = ?", org, postmark).Count(&n).Error; err != nil {
			return err
		}
		var old store.BounceHook
		if found := tx.Where("org_id = ?", org).Limit(1).Find(&old); found.Error != nil {
			return found.Error
		} else if n == 0 && found.RowsAffected > 0 {
			if err := tx.Create(&store.HookKey{OrgID: org, Provider: postmark, Kid: store.RandomToken(8), Hash: old.SecretHash, CreatedAt: old.CreatedAt}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&store.HookKey{}).Where("org_id = ? and provider = ? and expires_at is null", org, postmark).
			Update("expires_at", now.Add(store.HookKeyOverlap)).Error; err != nil {
			return err
		}
		if err := tx.Create(&store.HookKey{OrgID: org, Provider: postmark, Kid: store.RandomToken(8), Hash: store.HashToken(secret), CreatedAt: now}).Error; err != nil {
			return err
		}
		// The release before the key set reads bounce_hooks, so it is kept in step for a
		// rollback to find the secret made here.
		row := store.BounceHook{OrgID: org, SecretHash: store.HashToken(secret), CreatedAt: now}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "org_id"}}, DoUpdates: clause.AssignmentColumns([]string{"secret_hash", "created_at"})}).Create(&row).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, nil, actor, store.ActWebhookNew, nil)
	})
}

// DropBounceHook stops taking bounces: every secret the hook had stops signing in.
func (s *OutreachStore) DropBounceHook(org int64, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		keys := tx.Where("org_id = ? and provider = ?", org, postmark).Delete(&store.HookKey{})
		if keys.Error != nil {
			return keys.Error
		}
		old := tx.Delete(&store.BounceHook{}, "org_id = ?", org)
		if old.Error != nil || keys.RowsAffected+old.RowsAffected == 0 {
			return old.Error
		}
		return s.Log(tx, org, 0, nil, actor, store.ActWebhookDrop, nil)
	})
}

func (s *OutreachStore) HasBounceHook(org int64) bool {
	var n int64
	return s.DB.Model(&store.BounceHook{}).Where("org_id = ?", org).Count(&n).Error == nil && n > 0 ||
		s.DB.Model(&store.HookKey{}).Where("org_id = ? and provider = ? and (expires_at is null or expires_at > ?)", org, postmark, time.Now()).
			Count(&n).Error == nil && n > 0
}

// BounceHookOrg is the organisation a webhook's sign-in is for: its slug, and one of its
// live secrets. Each is compared in full, so how long the check takes says nothing of
// which came close.
func (s *OutreachStore) BounceHookOrg(slug, secret string) (*store.Org, bool) {
	o, err := s.Identity.OrgBySlug(slug)
	if err != nil || secret == "" {
		return nil, false
	}
	now, given := time.Now(), []byte(store.HashToken(secret))
	var keys []store.HookKey
	if s.DB.Where("org_id = ? and provider = ?", o.ID, postmark).Find(&keys).Error != nil {
		return nil, false
	}
	match := 0
	for _, k := range keys {
		if k.Live(now) {
			match |= subtle.ConstantTimeCompare(given, []byte(k.Hash))
		}
	}
	// A hook made before the key set has only its bounce_hooks row.
	if len(keys) == 0 {
		var h store.BounceHook
		if found := s.DB.Where("org_id = ?", o.ID).Limit(1).Find(&h); found.Error == nil && found.RowsAffected > 0 {
			match = subtle.ConstantTimeCompare(given, []byte(h.SecretHash))
		}
	}
	return o, match == 1
}

// MailFailed puts an address on the list. A bounce is kept beside any reason it already
// had, and says so on the last mail that went to it; a complaint is about a mail that
// arrived, which stays sent.
func (s *OutreachStore) MailFailed(org int64, addr, reason, said string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := s.Suppression.SuppressIn(tx, org, addr, reason, bounceBy); err != nil {
			return err
		}
		if reason != SuppressBounced {
			return nil
		}
		if err := tx.Model(&store.Suppression{}).Where("org_id = ? and address = ?", org, addr).Update("bounced_at", time.Now()).Error; err != nil {
			return err
		}
		var last store.CampaignRecipient
		found := tx.Where("org_id = ? and address = ? and status = ?", org, addr, MailSent).Order("sent_at desc, id desc").Limit(1).Find(&last)
		if found.Error != nil || found.RowsAffected == 0 {
			return found.Error
		}
		return tx.Model(&store.CampaignRecipient{}).Where("org_id = ? and id = ?", org, last.ID).Updates(map[string]any{"status": MailFailed, "reason": said}).Error
	})
}
