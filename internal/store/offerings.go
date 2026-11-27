package store

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
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

const OfferingNameMax = 60

func (o *Offering) Check() error {
	o.Name = strings.TrimSpace(o.Name)
	if o.Name == "" {
		return errors.New("an offering needs a name")
	}
	if utf8.RuneCountInString(o.Name) > OfferingNameMax {
		return fmt.Errorf("an offering's name is %d characters at most", OfferingNameMax)
	}
	_, err := OrgSettings{LookFor: o.LookFor}.Assets()
	return err
}
