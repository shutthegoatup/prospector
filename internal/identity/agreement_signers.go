package identity

import (
	"slices"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/store"
)

// DropRecipients takes the recipients a query matches off their drafts, with their
// fields, when the person or member they stand for goes.
func DropRecipients(tx *gorm.DB, matching *gorm.DB) error {
	var ids []int64
	if err := matching.Model(&store.AgreementRecipient{}).Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
		return err
	}
	// On an agreement that has gone out, who it went to is part of its record: they are
	// detached from the person or member, never taken off.
	var kept []int64
	if err := tx.Table("agreement_recipients").Select("agreement_recipients.id").
		Joins("join agreements on agreements.id = agreement_recipients.agreement_id and agreements.org_id = agreement_recipients.org_id").
		Where("agreement_recipients.id in ? and agreements.status <> ?", ids, AgreementDraft).Pluck("agreement_recipients.id", &kept).Error; err != nil {
		return err
	}
	if len(kept) > 0 {
		if err := tx.Model(&store.AgreementRecipient{}).Where("id in ?", kept).Updates(map[string]any{"contact_id": nil, "user_id": nil}).Error; err != nil {
			return err
		}
	}
	gone := slices.DeleteFunc(ids, func(id int64) bool { return slices.Contains(kept, id) })
	if len(gone) == 0 {
		return nil
	}
	if err := tx.Where("recipient_id in ?", gone).Delete(&store.AgreementField{}).Error; err != nil {
		return err
	}
	return tx.Where("id in ?", gone).Delete(&store.AgreementRecipient{}).Error
}
