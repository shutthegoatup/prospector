package main

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/tz"
)

func (s *prospectingStore) ContactByID(org, id int64) (*Contact, error) {
	var c Contact
	if err := s.InOrg(org).First(&c, id).Error; err != nil {
		return nil, missing(err, "no contact %d in this organisation", id)
	}
	return &c, nil
}

// Due returns one outreach step's queue for an organisation.
// People on the never-contact list are left out here rather than marked, so the list
// and the queue can't drift apart: listing someone hides them, unlisting brings them back.
func (s *prospectingStore) Due(org int64, step string, now time.Time, set OrgSettings) ([]Contact, error) {
	set = set.WithDefaults()
	var rows []Contact
	q := s.InOrg(org).Model(&Contact{}).Where(companyNotRejected)
	switch step {
	case "linkedin":
		q = q.Where(openContacts + " and linkedin_added is null and note is not null and note <> ''").Order("fit nulls first, file")
	case "accepted":
		q = q.Where(openContacts + " and accepted is not null and email_sent is null").Order("accepted")
	case "call":
		q = q.Where(openContacts + " and phone is not null and phone != '' and called is null").
			Order("linkedin_added is null, linkedin_added nulls first, fit nulls first")
	case "email":
		cutoff := now.AddDate(0, 0, -set.EmailAfterDays).Format("2006-01-02")
		q = q.Where(openContacts+" and email_sent is null and accepted is null and linkedin_added <= ?", cutoff).Order("linkedin_added")
	case "followup":
		cutoff := now.AddDate(0, 0, -set.FollowupAfterDays).Format("2006-01-02")
		q = q.Where(openContacts+" and followup_sent is null and email_sent <= ?", cutoff).Order("email_sent")
	default:
		return nil, fmt.Errorf("unknown step %q", step)
	}
	if err := q.Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	if strings.TrimSpace(set.NeverContact) == "" {
		return rows, nil
	}
	due := rows[:0]
	for _, c := range rows {
		if set.Listed(deref(c.Email), deref(c.Linkedin)) == nil {
			due = append(due, c)
		}
	}
	return due, nil
}

func (s *prospectingStore) Mark(org, id int64, step, date, actor string) error {
	return s.mark(org, id, step, date, Actor{Name: actor}, "")
}

func (s *prospectingStore) mark(org, id int64, step, date string, who Actor, note string) error {
	column, ok := Marks[step]
	if !ok {
		return fmt.Errorf("unknown step %q", step)
	}
	if err := markDate(date, time.Now()); err != nil {
		return err
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var c Contact
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
			return missing(err, "no contact %d", id)
		}
		if err := c.CanMark(); err != nil {
			return err
		}
		updates := map[string]any{column: date}
		if step == "replied" && c.Stage == "" {
			updates["stage"] = Stages[0].Key
		}
		if err := tx.Model(&c).Updates(updates).Error; err != nil {
			return err
		}
		value := date
		if note != "" {
			value += " · " + note
		}
		return s.LogAs(tx, org, id, c.CompanyID, who, "mark:"+step, &value)
	})
}

func (s *prospectingStore) Update(org, id int64, field, value, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if of, ok := strings.CutPrefix(field, "deal_"); ok {
			return s.deals.SetDeal(tx, org, id, of, value, actor)
		}
		return s.set(tx, org, id, field, value, actor)
	})
}

// ChangeContactFields makes several changes to a person, or none: one that is refused
// takes back those before it.
func (s *prospectingStore) ChangeContactFields(org, id int64, changes []Change, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		for _, c := range changes {
			if err := s.set(tx, org, id, c.Field, c.Value, actor); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *prospectingStore) set(tx *gorm.DB, org, id int64, field, value, actor string) error {
	if !Editable[field] {
		return fmt.Errorf("%q can't be edited", field)
	}
	value = strings.TrimSpace(value)
	var v any = nonEmpty(value)
	if stringFields[field] {
		v = value
	}
	var c Contact
	if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
		return missing(err, "no contact %d", id)
	}
	if field == "stage" {
		if err := c.CanEnterStage(value); err != nil {
			return err
		}
	}
	if field == "email" || field == "linkedin" {
		var o Org
		if err := tx.First(&o, org).Error; err != nil {
			return err
		}
		email, link := value, ""
		if field == "linkedin" {
			email, link = "", value
		}
		if err := s.Allowed(&o, email, link); err != nil {
			return err
		}
		if err := erased(tx, org, "", "", email, link); err != nil {
			return err
		}
	}
	if err := tx.Model(&c).Update(field, v).Error; err != nil {
		return err
	}
	if field == "phone" {
		if err := tx.Model(&c).Update("tz", tz.Guess(value, deref(c.Maker))).Error; err != nil {
			return err
		}
	}
	var logged *string
	if loggedValue[field] {
		logged = nonEmpty(value)
	}
	return s.Log(tx, org, id, c.CompanyID, actor, "set:"+field, logged)
}

// Find matches the CLI's name argument against person, maker or first name.
func (s *prospectingStore) Find(org int64, name string) ([]Contact, error) {
	var rows []Contact
	like := "%" + strings.ToLower(name) + "%"
	err := s.InOrg(org).Where("lower(person) like ? or lower(maker) like ? or first = ?", like, like, name).Find(&rows).Error
	return rows, err
}

func (s *prospectingStore) FindOne(org int64, name string) (*Contact, error) {
	rows, err := s.Find(org, name)
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		var who []string
		for _, r := range rows {
			who = append(who, deref(r.First)+" @ "+deref(r.Maker))
		}
		return nil, fmt.Errorf("%d matches for %q: %s", len(rows), name, strings.Join(who, "; "))
	}
	return &rows[0], nil
}
