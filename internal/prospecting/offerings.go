package prospecting

import (
	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/store"
)

// OfferingStages is where each of these companies stands for an offering; one not yet
// judged for it is up for review.
func OfferingStages(tx *gorm.DB, org, offering int64, companies []int64) (map[int64]string, error) {
	out := map[int64]string{}
	for _, id := range companies {
		out[id] = CompanyReview
	}
	if len(companies) == 0 {
		return out, nil
	}
	var rows []store.Verdict
	if err := tx.Where("org_id = ? and offering_id = ? and company_id in ?", org, offering, companies).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, v := range rows {
		out[v.CompanyID] = v.Stage
	}
	return out, nil
}
