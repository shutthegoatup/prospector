package research

import (
	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/store"
)

// companyAt finds the company an agent means by its website when the name is new,
// so one company found under two spellings stays one row. A site that several
// companies share (a group's brands, a marketplace) identifies none of them.
func companyAt(tx *gorm.DB, org int64, domain string) (*store.Company, error) {
	if domain == "" {
		return nil, nil
	}
	var found []store.Company
	if err := tx.Where("org_id = ? and lower(domain) = ?", org, domain).Limit(2).Find(&found).Error; err != nil || len(found) != 1 {
		return nil, err
	}
	return &found[0], nil
}
