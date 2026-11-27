package main

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

func nameFree(tx *gorm.DB, org, id int64, name string) error {
	var n int64
	if err := tx.Model(&Offering{}).Where("org_id = ? and id <> ? and lower(name) = ?", org, id, strings.ToLower(name)).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return conflict(fmt.Sprintf("there is already an offering called %q", name))
	}
	return nil
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

type VerdictRow struct {
	Verdict
	Company string
}
