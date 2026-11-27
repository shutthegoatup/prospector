package main

import (
	"crypto/subtle"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// NewBounceHook makes the organisation's webhook secret, replacing any it had, and
// gives it back the once.
func (s *outreachStore) NewBounceHook(org int64) (string, error) {
	secret := randomToken(32)
	row := BounceHook{OrgID: org, SecretHash: hashToken(secret), CreatedAt: time.Now()}
	err := s.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "org_id"}}, DoUpdates: clause.AssignmentColumns([]string{"secret_hash", "created_at"})}).Create(&row).Error
	return secret, err
}

func (s *outreachStore) DropBounceHook(org int64) error {
	return s.db.Delete(&BounceHook{}, "org_id = ?", org).Error
}

func (s *outreachStore) HasBounceHook(org int64) bool {
	var n int64
	return s.db.Model(&BounceHook{}).Where("org_id = ?", org).Count(&n).Error == nil && n > 0
}

// BounceHookOrg is the organisation a webhook's sign-in is for: its slug and secret.
func (s *outreachStore) BounceHookOrg(slug, secret string) (*Org, bool) {
	o, err := s.identity.OrgBySlug(slug)
	if err != nil || secret == "" {
		return nil, false
	}
	var h BounceHook
	if found := s.db.Where("org_id = ?", o.ID).Limit(1).Find(&h); found.Error != nil || found.RowsAffected == 0 {
		return nil, false
	}
	return o, subtle.ConstantTimeCompare([]byte(hashToken(secret)), []byte(h.SecretHash)) == 1
}

// MailFailed puts an address on the list. A bounce is kept beside any reason it already
// had, and says so on the last mail that went to it; a complaint is about a mail that
// arrived, which stays sent.
func (s *outreachStore) MailFailed(org int64, addr, reason, said string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if _, err := s.suppression.suppress(tx, org, addr, reason, bounceBy); err != nil {
			return err
		}
		if reason != SuppressBounced {
			return nil
		}
		if err := tx.Model(&Suppression{}).Where("org_id = ? and address = ?", org, addr).Update("bounced_at", time.Now()).Error; err != nil {
			return err
		}
		var last CampaignRecipient
		found := tx.Where("org_id = ? and address = ? and status = ?", org, addr, MailSent).Order("sent_at desc, id desc").Limit(1).Find(&last)
		if found.Error != nil || found.RowsAffected == 0 {
			return found.Error
		}
		return tx.Model(&CampaignRecipient{}).Where("org_id = ? and id = ?", org, last.ID).Updates(map[string]any{"status": MailFailed, "reason": said}).Error
	})
}
