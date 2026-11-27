package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Offering is something an organisation sells besides what its settings describe, with
// its own look-for fields and wording. A company is judged for each one on its own, so
// one ruled out for this can still be worth pursuing for that.
//
// The organisation's own offering, number 0, is never stored: its wording is in Settings
// and its verdict on a company is the company's own stage, as before offerings existed.
type Offering struct {
	ID           int64  `gorm:"primaryKey" json:"id"`
	OrgID        int64  `gorm:"uniqueIndex:idx_offering_name,priority:1;not null" json:"-"`
	Name         string `gorm:"uniqueIndex:idx_offering_name,priority:2;not null" json:"name"`
	LookFor      string `gorm:"not null;default:''" json:"look_for"`
	Pitch        string `gorm:"not null;default:''" json:"pitch"`
	NoteTemplate string `gorm:"not null;default:''" json:"note_template"`
	// HasRoles: companies are judged for it by their roles, each pursued or junked.
	HasRoles  bool      `gorm:"not null;default:false" json:"has_roles"`
	UpdatedBy string    `gorm:"not null;default:''" json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Verdict is where a company stands for one offering other than the organisation's own.
type Verdict struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	OrgID      int64     `gorm:"uniqueIndex:idx_verdict,priority:1;not null" json:"-"`
	OfferingID int64     `gorm:"uniqueIndex:idx_verdict,priority:2;not null" json:"offering_id"`
	CompanyID  int64     `gorm:"uniqueIndex:idx_verdict,priority:3;index;not null" json:"company_id"`
	Stage      string    `gorm:"not null;default:'review'" json:"stage"`
	Fit        string    `gorm:"not null;default:''" json:"fit"`
	Reason     string    `gorm:"not null;default:''" json:"reason"`
	By         string    `gorm:"column:decided_by;not null;default:''" json:"by"`
	UpdatedAt  time.Time `json:"updated_at"`
}

const offeringNameMax = 60

func (o *Offering) check() error {
	o.Name = strings.TrimSpace(o.Name)
	if o.Name == "" {
		return errors.New("an offering needs a name")
	}
	if utf8.RuneCountInString(o.Name) > offeringNameMax {
		return fmt.Errorf("an offering's name is %d characters at most", offeringNameMax)
	}
	_, err := OrgSettings{LookFor: o.LookFor}.Assets()
	return err
}

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
		return conflict(fmt.Sprintf("there is already an offering called %q", name))
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
