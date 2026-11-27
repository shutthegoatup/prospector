package main

import (
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/shutthegoatup/prospector/internal/say"
)

// heldOffering reads an offering with its row held, so it can't be removed or changed
// under the caller before the transaction ends.
func heldOffering(tx *gorm.DB, org, id int64) (*Offering, error) {
	if err := hold(tx, &Offering{}, org, id); err != nil {
		return nil, err
	}
	var o Offering
	if err := tx.Where("org_id = ? and id = ?", org, id).First(&o).Error; err != nil {
		return nil, missing(err, "no offering %d", id)
	}
	return &o, nil
}

func nameFree(tx *gorm.DB, org, id int64, name string) error {
	var n int64
	if err := tx.Model(&Offering{}).Where("org_id = ? and id <> ? and lower(name) = ?", org, id, strings.ToLower(name)).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return conflict{say.Errorf("there is already an offering called %q", name)}
	}
	return nil
}

// putUpForReview brings a company found for an offering into that offering's review,
// unless it has already been judged for it, and says where it stands.
func putUpForReview(tx *gorm.DB, org, offering, company int64, actor string) (string, error) {
	if _, err := heldOffering(tx, org, offering); err != nil {
		return "", err
	}
	v := Verdict{OrgID: org, OfferingID: offering, CompanyID: company, Stage: CompanyReview, By: actor, UpdatedAt: time.Now()}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&v).Error; err != nil {
		return "", err
	}
	var stage string
	return stage, tx.Model(&Verdict{}).Select("stage").Where("org_id = ? and offering_id = ? and company_id = ?", org, offering, company).Scan(&stage).Error
}

// qualifyFor qualifies a company for an offering when someone there is picked for it,
// unless a person has already said otherwise or its roles there decide. The offering is held by the caller.
func qualifyFor(tx *gorm.DB, org int64, o *Offering, company int64, actor string) error {
	if _, err := putUpForReview(tx, org, o.ID, company, actor); err != nil {
		return err
	}
	if byRoles, err := judgedByRoles(tx, org, o, company); err != nil || byRoles {
		return err
	}
	res := tx.Model(&Verdict{}).Where("org_id = ? and offering_id = ? and company_id = ? and stage = ?", org, o.ID, company, CompanyReview).
		Updates(map[string]any{"stage": CompanyQualified, "decided_by": actor, "updated_at": time.Now()})
	if res.Error != nil || res.RowsAffected == 0 {
		return res.Error
	}
	return tx.Create(&Event{OrgID: org, CompanyID: &company, User: actor, Action: "verdict:" + CompanyQualified, Value: &o.Name, At: time.Now()}).Error
}

// forOffering puts a company an agent works on for a task up for review by the task's
// offering, if it is for one. The company is held already, so the offering comes second
// here as everywhere. Work for the organisation's own brings it into the organisation's
// own review, if it was found for another offering.
func forOffering(tx *gorm.DB, org int64, who Actor, company int64) error {
	if who.Offering == 0 {
		return foundForOwn(tx, org, company)
	}
	_, err := putUpForReview(tx, org, who.Offering, company, who.Name)
	return err
}

func foundForOwn(tx *gorm.DB, org, company int64) error {
	return tx.Model(&Company{}).Where("org_id = ? and id = ? and found_for <> 0", org, company).Update("found_for", 0).Error
}

// offeringStages is where each of these companies stands for an offering; one not yet
// judged for it is up for review.
func offeringStages(tx *gorm.DB, org, offering int64, companies []int64) (map[int64]string, error) {
	out := map[int64]string{}
	for _, id := range companies {
		out[id] = CompanyReview
	}
	if len(companies) == 0 {
		return out, nil
	}
	var rows []Verdict
	if err := tx.Where("org_id = ? and offering_id = ? and company_id in ?", org, offering, companies).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, v := range rows {
		out[v.CompanyID] = v.Stage
	}
	return out, nil
}

type VerdictRow struct {
	Verdict
	Company string
}
