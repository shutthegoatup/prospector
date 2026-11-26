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
	ID           int64     `gorm:"primaryKey" json:"id"`
	OrgID        int64     `gorm:"uniqueIndex:idx_offering_name,priority:1;not null" json:"-"`
	Name         string    `gorm:"uniqueIndex:idx_offering_name,priority:2;not null" json:"name"`
	LookFor      string    `gorm:"not null;default:''" json:"look_for"`
	Pitch        string    `gorm:"not null;default:''" json:"pitch"`
	NoteTemplate string    `gorm:"not null;default:''" json:"note_template"`
	UpdatedBy    string    `gorm:"not null;default:''" json:"updated_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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

func (s *Store) Offerings(org int64) ([]Offering, error) {
	var out []Offering
	return out, s.in(org).Order("lower(name), id").Find(&out).Error
}

func (s *Store) Offering(org, id int64) (*Offering, error) {
	var o Offering
	if err := s.in(org).Where("id = ?", id).First(&o).Error; err != nil {
		return nil, missing(err, "no offering %d", id)
	}
	return &o, nil
}

// offeringOf is the offering with this id, or nil for the organisation's own.
func (s *Store) offeringOf(org, id int64) (*Offering, error) {
	if id == 0 {
		return nil, nil
	}
	return s.Offering(org, id)
}

func (s *Store) AddOffering(org int64, o Offering, actor string) (*Offering, error) {
	if err := o.check(); err != nil {
		return nil, err
	}
	o.ID, o.OrgID, o.UpdatedBy = 0, org, actor
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := holdOrg(tx, org); err != nil {
			return err
		}
		if err := nameFree(tx, org, 0, o.Name); err != nil {
			return err
		}
		if err := tx.Create(&o).Error; err != nil {
			return err
		}
		return s.log(tx, org, 0, nil, actor, "offering:added", &o.Name)
	})
	return &o, err
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

// ChangeOffering applies change to the offering as it is now, and saves it.
func (s *Store) ChangeOffering(org, id int64, change func(*Offering), actor string) (*Offering, error) {
	var o *Offering
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := holdOrg(tx, org); err != nil {
			return err
		}
		var err error
		if o, err = heldOffering(tx, org, id); err != nil {
			return err
		}
		change(o)
		if err := o.check(); err != nil {
			return err
		}
		if err := nameFree(tx, org, o.ID, o.Name); err != nil {
			return err
		}
		o.UpdatedBy = actor
		if err := tx.Model(o).Select("name", "look_for", "pitch", "note_template", "updated_by", "updated_at").Updates(o).Error; err != nil {
			return err
		}
		return s.log(tx, org, 0, nil, actor, "offering:changed", &o.Name)
	})
	return o, err
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

// RemoveOffering deletes an offering and every verdict made for it; the history keeps
// what was decided. People being reached for it are then reached for the organisation's
// own, and companies found only for it come up in the organisation's own review.
func (s *Store) RemoveOffering(org, id int64, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		// Companies are held before the offering, as everywhere; holding the organisation
		// keeps any more from being found for it meanwhile.
		if err := holdOrg(tx, org); err != nil {
			return err
		}
		if err := tx.Model(&Company{}).Where("org_id = ? and found_for = ?", org, id).Update("found_for", 0).Error; err != nil {
			return err
		}
		o, err := heldOffering(tx, org, id)
		if err != nil {
			return err
		}
		var open int64
		if err := tx.Model(&Task{}).Where("org_id = ? and offering_id = ? and closed_at is null", org, id).Count(&open).Error; err != nil {
			return err
		}
		if open > 0 {
			return conflict(fmt.Sprintf("%d open tasks are finding companies for %s: close them first", open, o.Name))
		}
		if err := tx.Where("org_id = ? and offering_id = ?", org, id).Delete(&Verdict{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Contact{}).Where("org_id = ? and offering_id = ?", org, id).Update("offering_id", 0).Error; err != nil {
			return err
		}
		if err := tx.Delete(o).Error; err != nil {
			return err
		}
		return s.log(tx, org, 0, nil, actor, "offering:removed", &o.Name)
	})
}

// JudgeCompany says where a company stands for an offering; offering 0 is the
// organisation's own, whose verdict is the company's stage.
func (s *Store) JudgeCompany(org, offering, company int64, stage, fit, reason, actor string) error {
	if offering == 0 {
		if strings.TrimSpace(reason) != "" {
			return errors.New("a reason is kept with a verdict for an offering; for the organisation's own, say why in fit")
		}
		return s.SetCompanyStage(org, company, stage, fit, actor)
	}
	if stage != CompanyQualified && stage != CompanyRejected && stage != CompanyReview {
		return fmt.Errorf("unknown stage %q", stage)
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		o, err := heldOffering(tx, org, offering)
		if err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&Company{}).Where("org_id = ? and id = ?", org, company).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return noSuch("no company %d", company)
		}
		v := Verdict{OrgID: org, OfferingID: offering, CompanyID: company, Stage: stage, Fit: fit,
			Reason: strings.TrimSpace(reason), By: actor, UpdatedAt: time.Now()}
		update := []string{"stage", "reason", "decided_by", "updated_at"}
		if fit != "" {
			update = append(update, "fit")
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "org_id"}, {Name: "offering_id"}, {Name: "company_id"}},
			DoUpdates: clause.AssignmentColumns(update),
		}).Create(&v).Error; err != nil {
			return err
		}
		said := o.Name
		if v.Reason != "" {
			said += ": " + v.Reason
		}
		return s.log(tx, org, 0, &company, actor, "verdict:"+stage, &said)
	})
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
// unless a person has already said otherwise. The offering is held by the caller.
func qualifyFor(tx *gorm.DB, org int64, o *Offering, company int64, actor string) error {
	if _, err := putUpForReview(tx, org, o.ID, company, actor); err != nil {
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

// ToReview is how many companies wait for review, counting each offering's queue.
func (s *Store) ToReview(org int64) int {
	var n int64
	logged(s.in(org).Model(&Verdict{}).Where("stage = ?", CompanyReview).Count(&n))
	return s.ResearchCount(org, "review") + int(n)
}

// withVerdicts shows Research rows as they stand for an offering: its stage, and its
// grade where a person gave one.
func (s *Store) withVerdicts(org, offering int64, rows []ResearchRow) error {
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	var verdicts []Verdict
	if err := s.in(org).Where("offering_id = ? and company_id in ?", offering, ids).Find(&verdicts).Error; err != nil {
		return err
	}
	by := map[int64]Verdict{}
	for _, v := range verdicts {
		by[v.CompanyID] = v
	}
	for i := range rows {
		v, ok := by[rows[i].ID]
		rows[i].Stage = CompanyReview
		if ok {
			rows[i].Stage = v.Stage
		}
		if v.Fit != "" {
			rows[i].Fit = v.Fit
		}
	}
	return nil
}

func (s *Store) standing(org, company int64) ([]OfferingStanding, error) {
	var out []OfferingStanding
	return out, s.db.Model(&Verdict{}).Select("offerings.name as offering, verdicts.stage, verdicts.reason").
		Joins("join offerings on offerings.id = verdicts.offering_id and offerings.org_id = verdicts.org_id").
		Where("verdicts.org_id = ? and verdicts.company_id = ?", org, company).Order("lower(offerings.name)").Scan(&out).Error
}

type VerdictRow struct {
	Verdict
	Company string
}

func (s *Store) verdictRows(org int64) *gorm.DB {
	return s.db.Model(&Verdict{}).Select("verdicts.*, companies.name as company").
		Joins("join companies on companies.id = verdicts.company_id and companies.org_id = verdicts.org_id").
		Joins("join offerings on offerings.id = verdicts.offering_id and offerings.org_id = verdicts.org_id").
		Where("verdicts.org_id = ?", org)
}

// Verdicts are where a company stands for each offering it has been judged for.
func (s *Store) Verdicts(org, company int64) ([]VerdictRow, error) {
	var n int64
	if err := s.in(org).Model(&Company{}).Where("id = ?", company).Count(&n).Error; err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, noSuch("no company %d", company)
	}
	var out []VerdictRow
	return out, s.verdictRows(org).Where("verdicts.company_id = ?", company).Order("verdicts.offering_id").Scan(&out).Error
}

// Judged lists an offering's verdicts at one stage, or at every stage when stage is
// empty, latest first; a company ruled out by mistake is found here and put back.
func (s *Store) Judged(org, offering int64, stage string) ([]VerdictRow, error) {
	if _, err := s.Offering(org, offering); err != nil {
		return nil, err
	}
	q := s.verdictRows(org).Where("verdicts.offering_id = ?", offering)
	if stage != "" {
		q = q.Where("verdicts.stage = ?", stage)
	}
	var out []VerdictRow
	return out, q.Order("verdicts.updated_at desc, verdicts.id desc").Scan(&out).Error
}
