package crm

import (
	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/store"
)

func ErasedIn(tx *gorm.DB, org int64, person, maker, email, link string) error {
	ids := store.Identifiers(org, person, maker, email, link)
	if len(ids) == 0 {
		return nil
	}
	var n int64
	if err := tx.Model(&store.Erasure{}).Where("org_id = ? and hash in ?", org, ids).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return store.ErrErased
	}
	return nil
}
