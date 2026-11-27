package crm

import (
	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/store"
)

// JudgedByRoles says whether a company's verdict for an offering follows its roles there,
// so a person can't set it by hand.
func JudgedByRoles(tx *gorm.DB, org int64, o *store.Offering, company int64) (bool, error) {
	if !o.HasRoles {
		return false, nil
	}
	var n int64
	err := tx.Model(&store.Role{}).Where("org_id = ? and offering_id = ? and company_id = ?", org, o.ID, company).Count(&n).Error
	return n > 0, err
}
