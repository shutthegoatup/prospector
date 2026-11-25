package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Erasure is what stays behind when a person is erased: hashes of what identified
// them, so an import, a restore or an agent can't bring them back. The hash is salted
// with the organisation, so the same address erased in two can't be matched up.
type Erasure struct {
	ID    int64  `gorm:"primaryKey"`
	OrgID int64  `gorm:"uniqueIndex:idx_erasure;not null"`
	Hash  string `gorm:"uniqueIndex:idx_erasure;not null"`
	At    time.Time
}

var errErased = errors.New("this person was erased on request and can't be added again")

var emailRe = regexp.MustCompile(`[^\s,;:<>()"']+@[^\s,;:<>()"']+`)

// identifiers are the hashes for a name at a company, the addresses in an email field
// and a LinkedIn profile. A name alone would ban a namesake elsewhere, and only a
// profile counts as a link: every company placeholder shares one search link.
func identifiers(org int64, person, maker, email, link string) []string {
	var out []string
	add := func(kind, v string) {
		if v != "" {
			sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s", org, kind, v)))
			out = append(out, hex.EncodeToString(sum[:]))
		}
	}
	if name, co := strings.ToLower(personKey(person)), strings.ToLower(companyName(maker)); name != "" && co != "" {
		add("name", name+"\x00"+co)
	}
	for _, e := range emailRe.FindAllString(strings.ToLower(email), -1) {
		add("email", strings.Trim(e, "."))
	}
	if p := strings.Split(linkedinPath(link), "/"); len(p) >= 2 && p[0] == "in" {
		add("linkedin", p[1])
	}
	return out
}

func erased(tx *gorm.DB, org int64, person, maker, email, link string) error {
	ids := identifiers(org, person, maker, email, link)
	if len(ids) == 0 {
		return nil
	}
	var n int64
	if err := tx.Model(&Erasure{}).Where("org_id = ? and hash in ?", org, ids).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errErased
	}
	return nil
}

// ErasePerson deletes a contact row and everything that says who it was. A row that
// names alternates ("A or B") goes whole. The company's history keeps that something
// happened and when, which the counts are built from, but not to whom.
func (s *Store) ErasePerson(org, id int64, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var c Contact
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
			return missing(err, "no contact %d", id)
		}
		if strings.HasPrefix(c.Key, "org:") {
			return errors.New("that row stands for a company, not a person")
		}
		// Agents' notes are free text that can't be searched for a name, so every note on a
		// task their row was part of goes, as below does every note about their company.
		var touched []int64
		if err := tx.Model(&Event{}).Where("org_id = ? and contact_id = ? and task_id <> 0", org, id).
			Distinct().Pluck("task_id", &touched).Error; err != nil {
			return err
		}
		if len(touched) > 0 {
			if err := tx.Where("org_id = ? and task_id in ?", org, touched).Delete(&TaskNote{}).Error; err != nil {
				return err
			}
		}
		if err := forgetEvents(tx.Where("org_id = ? and contact_id = ?", org, id)); err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Delete(&Fact{}).Error; err != nil {
			return err
		}
		// An address or profile suggested for them identifies them too, unless someone
		// dismissed it as not theirs.
		var suggested []Suggestion
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Find(&suggested).Error; err != nil {
			return err
		}
		addresses := deref(c.Email)
		hashes := identifiers(org, displayName(c), deref(c.Maker), deref(c.Email), deref(c.Linkedin))
		for _, sg := range suggested {
			if sg.Decision == SuggestionDismissed {
				continue
			}
			switch sg.Field {
			case "email":
				addresses += " " + sg.Value
				hashes = append(hashes, identifiers(org, "", "", sg.Value, "")...)
			case "linkedin":
				hashes = append(hashes, identifiers(org, "", "", "", sg.Value)...)
			}
		}
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Delete(&Suggestion{}).Error; err != nil {
			return err
		}
		// What they wrote to us goes too, whether or not it was matched to them.
		gone := tx.Where("org_id = ? and contact_id = ?", org, id)
		if emails := emailRe.FindAllString(strings.ToLower(addresses), -1); len(emails) > 0 {
			gone = tx.Where("org_id = ? and (contact_id = ? or email in ?)", org, id, emails)
		}
		if err := gone.Delete(&Enquiry{}).Error; err != nil {
			return err
		}
		// Agents' notes are free text that can't be searched for a name, so every note
		// about their company goes, and every note on a task that touched it.
		if c.CompanyID != nil {
			tasks := tx.Model(&Event{}).Select("task_id").Where("org_id = ? and company_id = ? and task_id > 0", org, *c.CompanyID)
			if err := tx.Where("org_id = ? and (company_id = ? or task_id in (?))", org, *c.CompanyID, tasks).Delete(&TaskNote{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Delete(&c).Error; err != nil {
			return err
		}
		for _, h := range hashes {
			row := Erasure{OrgID: org, Hash: h, At: time.Now()}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return err
			}
		}
		return s.log(tx, org, 0, c.CompanyID, actor, "erase", nil)
	})
}

// forgetEvents scrubs the events a query matches and detaches them from their contact,
// so a later contact given the same id can't inherit them.
func forgetEvents(matching *gorm.DB) error {
	var events []Event
	if err := matching.Find(&events).Error; err != nil {
		return err
	}
	tx := matching.Session(&gorm.Session{NewDB: true})
	for _, e := range events {
		if err := tx.Model(&Event{}).Where("id = ?", e.ID).
			Updates(map[string]any{"contact_id": 0, "value": keptValue(e)}).Error; err != nil {
			return err
		}
	}
	return nil
}

// keptValue is what an event may still say once its person is gone: a stage, or the
// day of a mark. Everything else held their details or free text about them.
func keptValue(e Event) *string {
	v := deref(e.Value)
	switch {
	case e.Action == "set:stage" && validStage(v):
		return &v
	case strings.HasPrefix(e.Action, "mark:") && len(v) >= 10:
		if _, err := time.Parse("2006-01-02", v[:10]); err == nil {
			return str(v[:10])
		}
	}
	return nil
}
