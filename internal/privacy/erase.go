package privacy

import (
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/marketing"
	"github.com/shutthegoatup/prospector/internal/onboarding"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// ErasePerson deletes a contact row and everything that says who it was. A row that
// names alternates ("A or B") goes whole. The company's history keeps that something
// happened and when, which the counts are built from, but not to whom.
func (s *PrivacyStore) ErasePerson(org, id int64, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var c store.Contact
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
			return store.Missing(err, "no contact %d", id)
		}
		if strings.HasPrefix(c.Key, "org:") {
			return say.New("that row stands for a company, not a person")
		}
		// Agents' notes are free text that can't be searched for a name, so every note on a
		// task their row was part of goes, as below does every note about their company.
		var touched []int64
		if err := tx.Model(&store.Event{}).Where("org_id = ? and contact_id = ? and task_id <> 0", org, id).
			Distinct().Pluck("task_id", &touched).Error; err != nil {
			return err
		}
		if len(touched) > 0 {
			if err := tx.Where("org_id = ? and task_id in ?", org, touched).Delete(&store.TaskNote{}).Error; err != nil {
				return err
			}
		}
		if err := forgetEvents(tx.Where("org_id = ? and contact_id = ?", org, id)); err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Delete(&store.Fact{}).Error; err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Delete(&store.Deal{}).Error; err != nil {
			return err
		}
		// Their page closes in every browser, and the record that they could open it goes.
		if err := onboarding.DropCustomerGrants(tx, org, tx.Where("org_id = ? and contact_id = ?", org, id), false); err != nil {
			return err
		}
		if err := identity.DropRecipients(tx, tx.Where("org_id = ? and contact_id = ?", org, id)); err != nil {
			return err
		}
		// An address or profile suggested for them identifies them too, unless someone
		// dismissed it as not theirs.
		var suggested []store.Suggestion
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Find(&suggested).Error; err != nil {
			return err
		}
		var addresses strings.Builder
		addresses.WriteString(store.Deref(c.Email))
		hashes := store.Identifiers(org, store.DisplayName(c), store.Deref(c.Maker), store.Deref(c.Email), store.Deref(c.Linkedin))
		for _, sg := range suggested {
			if sg.Decision == prospecting.SuggestionDismissed {
				continue
			}
			switch sg.Field {
			case "email":
				addresses.WriteString(" " + sg.Value)
				hashes = append(hashes, store.Identifiers(org, "", "", sg.Value, "")...)
			case "linkedin":
				hashes = append(hashes, store.Identifiers(org, "", "", "", sg.Value)...)
			}
		}
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Delete(&store.Suggestion{}).Error; err != nil {
			return err
		}
		// What they wrote to us goes too, whether or not it was matched to them.
		gone := tx.Where("org_id = ? and contact_id = ?", org, id)
		emails := store.EmailRe.FindAllString(strings.ToLower(addresses.String()), -1)
		if len(emails) > 0 {
			gone = tx.Where("org_id = ? and (contact_id = ? or email in ?)", org, id, emails)
		}
		if err := forgetMailDomains(tx, emails); err != nil {
			return err
		}
		// The mails a campaign sent them stay counted, and say nothing of who they were.
		recipients := tx.Model(&store.CampaignRecipient{}).Where("org_id = ? and contact_id = ?", org, id)
		if len(emails) > 0 {
			recipients = tx.Model(&store.CampaignRecipient{}).Where("org_id = ? and (contact_id = ? or address in ?)", org, id, emails)
		}
		// One still waiting is not sent at all.
		if err := recipients.Updates(map[string]any{"contact_id": nil, "address": gorm.Expr("'erased:' || id"), "name": "",
			"status": gorm.Expr("case when status = ? then ? else status end", marketing.MailQueued, marketing.MailSkipped),
			"reason": gorm.Expr("case when status = ? then ? else reason end", marketing.MailQueued, "erased on request")}).Error; err != nil {
			return err
		}
		// After their mail is taken: a send claims its mail only once its unsubscribe link
		// is made, so a claim made first has a link this sees, and one made later fails.
		if err := forgetAddresses(tx, org, emails); err != nil {
			return err
		}
		// With what was said in answering them: their words, and ours to them.
		if err := tx.Where("org_id = ? and enquiry_id in (?)", org, gone.Session(&gorm.Session{}).Model(&store.Enquiry{}).Select("id")).Delete(&store.EnquiryMessage{}).Error; err != nil {
			return err
		}
		if err := gone.Delete(&store.Enquiry{}).Error; err != nil {
			return err
		}
		// Agents' notes are free text that can't be searched for a name, so every note
		// about their company goes, and every note on a task that touched it.
		if c.CompanyID != nil {
			tasks := tx.Model(&store.Event{}).Select("task_id").Where("org_id = ? and company_id = ? and task_id > 0", org, *c.CompanyID)
			if err := tx.Where("org_id = ? and (company_id = ? or task_id in (?))", org, *c.CompanyID, tasks).Delete(&store.TaskNote{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Delete(&c).Error; err != nil {
			return err
		}
		for _, h := range hashes {
			row := store.Erasure{OrgID: org, Hash: h, At: time.Now()}
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
	var events []store.Event
	if err := matching.Find(&events).Error; err != nil {
		return err
	}
	tx := matching.Session(&gorm.Session{NewDB: true})
	for _, e := range events {
		if err := tx.Model(&store.Event{}).Where("id = ?", e.ID).
			Updates(map[string]any{"contact_id": 0, "value": research.KeptValue(e)}).Error; err != nil {
			return err
		}
	}
	return nil
}

// forgetMailDomains drops what is kept about the domains of an erased person's
// addresses: a domain can be their own name. It is asked about again if anyone else uses it.
func forgetMailDomains(tx *gorm.DB, emails []string) error {
	var domains []string
	for _, e := range emails {
		if d := marketing.MailDomainOf(e); d != "" {
			domains = append(domains, d)
		}
	}
	if len(domains) == 0 {
		return nil
	}
	return tx.Where("domain in ?", domains).Delete(&store.MailDomain{}).Error
}

// forgetAddresses drops what is held against addresses being erased. The erasure's own
// hash of each keeps them from being mailed again.
func forgetAddresses(tx *gorm.DB, org int64, addresses []string) error {
	if len(addresses) == 0 {
		return nil
	}
	// As the erasure's own hash does: a dot at either end is the sentence's, not the address's.
	for _, a := range addresses {
		addresses = append(addresses, strings.Trim(a, "."))
	}
	if err := tx.Where("org_id = ? and address in ?", org, addresses).Delete(&store.Suppression{}).Error; err != nil {
		return err
	}
	return tx.Where("org_id = ? and address in ?", org, addresses).Delete(&store.UnsubscribeLink{}).Error
}
