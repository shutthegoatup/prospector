package crm

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/shutthegoatup/prospector/internal/store"
)

// HeldOffering reads an offering with its row held, so it can't be removed or changed
// under the caller before the transaction ends.
func HeldOffering(tx *gorm.DB, org, id int64) (*store.Offering, error) {
	if err := store.Hold(tx, &store.Offering{}, org, id); err != nil {
		return nil, err
	}
	var o store.Offering
	if err := tx.Where("org_id = ? and id = ?", org, id).First(&o).Error; err != nil {
		return nil, store.Missing(err, "no offering %d", id)
	}
	return &o, nil
}

// PutUpForReview brings a company found for an offering into that offering's review,
// unless it has already been judged for it, and says where it stands.
func PutUpForReview(tx *gorm.DB, org, offering, company int64, actor string) (string, error) {
	if _, err := HeldOffering(tx, org, offering); err != nil {
		return "", err
	}
	v := store.Verdict{OrgID: org, OfferingID: offering, CompanyID: company, Stage: CompanyReview, By: actor, UpdatedAt: time.Now()}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&v).Error; err != nil {
		return "", err
	}
	var stage string
	return stage, tx.Model(&store.Verdict{}).Select("stage").Where("org_id = ? and offering_id = ? and company_id = ?", org, offering, company).Scan(&stage).Error
}

// qualifyFor qualifies a company for an offering when someone there is picked for it,
// unless a person has already said otherwise or its roles there decide. The offering is held by the caller.
func qualifyFor(tx *gorm.DB, org int64, o *store.Offering, company int64, actor string) error {
	if _, err := PutUpForReview(tx, org, o.ID, company, actor); err != nil {
		return err
	}
	if byRoles, err := JudgedByRoles(tx, org, o, company); err != nil || byRoles {
		return err
	}
	res := tx.Model(&store.Verdict{}).Where("org_id = ? and offering_id = ? and company_id = ? and stage = ?", org, o.ID, company, CompanyReview).
		Updates(map[string]any{"stage": CompanyQualified, "decided_by": actor, "updated_at": time.Now()})
	if res.Error != nil || res.RowsAffected == 0 {
		return res.Error
	}
	return tx.Create(&store.Event{OrgID: org, CompanyID: &company, User: actor, Action: "verdict:" + CompanyQualified, Value: &o.Name, At: time.Now()}).Error
}

func FoundForOwn(tx *gorm.DB, org, company int64) error {
	return tx.Model(&store.Company{}).Where("org_id = ? and id = ? and found_for <> 0", org, company).Update("found_for", 0).Error
}
