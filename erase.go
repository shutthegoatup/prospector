package main

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErasePerson deletes a contact row and everything that says who it was. A row that
// names alternates ("A or B") goes whole. The company's history keeps that something
// happened and when, which the counts are built from, but not to whom.
func (s *Store) ErasePerson(org, id int64, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
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
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Delete(&Deal{}).Error; err != nil {
			return err
		}
		// Their page closes in every browser, and the record that they could open it goes.
		if err := dropCustomerGrants(tx, org, tx.Where("org_id = ? and contact_id = ?", org, id), false); err != nil {
			return err
		}
		if err := dropRecipients(tx, tx.Where("org_id = ? and contact_id = ?", org, id)); err != nil {
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
		emails := emailRe.FindAllString(strings.ToLower(addresses), -1)
		if len(emails) > 0 {
			gone = tx.Where("org_id = ? and (contact_id = ? or email in ?)", org, id, emails)
		}
		if err := forgetMailDomains(tx, emails); err != nil {
			return err
		}
		// The mails a campaign sent them stay counted, and say nothing of who they were.
		recipients := tx.Model(&CampaignRecipient{}).Where("org_id = ? and contact_id = ?", org, id)
		if len(emails) > 0 {
			recipients = tx.Model(&CampaignRecipient{}).Where("org_id = ? and (contact_id = ? or address in ?)", org, id, emails)
		}
		// One still waiting is not sent at all.
		if err := recipients.Updates(map[string]any{"contact_id": nil, "address": gorm.Expr("'erased:' || id"), "name": "",
			"status": gorm.Expr("case when status = ? then ? else status end", MailQueued, MailSkipped),
			"reason": gorm.Expr("case when status = ? then ? else reason end", MailQueued, "erased on request")}).Error; err != nil {
			return err
		}
		// After their mail is taken: a send claims its mail only once its unsubscribe link
		// is made, so a claim made first has a link this sees, and one made later fails.
		if err := forgetAddresses(tx, org, emails); err != nil {
			return err
		}
		// With what was said in answering them: their words, and ours to them.
		if err := tx.Where("org_id = ? and enquiry_id in (?)", org, gone.Session(&gorm.Session{}).Model(&Enquiry{}).Select("id")).Delete(&EnquiryMessage{}).Error; err != nil {
			return err
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
		return s.Log(tx, org, 0, c.CompanyID, actor, "erase", nil)
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
