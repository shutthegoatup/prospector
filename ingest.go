package main

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/tz"
)

// Import merges a parsed repo into one organisation. Text from the markdown refreshes,
// but a date or edit recorded in the tracker is never cleared. It is refused whole if it
// would bring in an address on the never-contact list; one already stored may stay, so
// listing someone who opted out doesn't break every later import.
func (s *Store) Import(org int64, p *ImportPayload) (int64, error) {
	o, err := s.OrgByID(org)
	if err != nil {
		return 0, err
	}
	var blocked []string
	stored := func(fresh string, have *string) bool {
		return strings.EqualFold(strings.TrimSpace(fresh), strings.TrimSpace(deref(have)))
	}
	for _, d := range p.Drafts {
		if err := erased(s.DB, org, d.Person, d.Maker, d.Email, d.Linkedin); err != nil {
			if !errors.Is(err, errErased) {
				return 0, err
			}
			blocked = append(blocked, fmt.Sprintf("%s in %s was erased on request; take them out of the file", d.Key, d.File))
		}
		email, link := s.Allowed(o, d.Email, ""), s.Allowed(o, "", d.Linkedin)
		if email == nil && link == nil {
			continue
		}
		var have Contact
		known := s.InOrg(org).Where("key = ?", d.Key).First(&have).Error == nil
		if email != nil && !(known && stored(d.Email, have.Email)) { //nolint:staticcheck // unless it's already stored
			blocked = append(blocked, fmt.Sprintf("%s in %s (%v)", d.Key, d.File, email))
		}
		if link != nil && !(known && stored(d.Linkedin, have.Linkedin)) { //nolint:staticcheck // unless it's already stored
			blocked = append(blocked, fmt.Sprintf("%s in %s (%v)", d.Key, d.File, link))
		}
	}
	if len(blocked) > 0 {
		return 0, say.Errorf("nothing imported: %s", strings.Join(blocked, "; "))
	}
	var total int64
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		for _, d := range p.Drafts {
			if err := upsertDraft(tx, org, d); err != nil {
				return err
			}
		}
		if err := seedSentLog(tx, org, p.SentLog); err != nil {
			return err
		}
		if err := importCompanies(tx, org, p.Companies); err != nil {
			return err
		}
		if err := linkCompanies(tx, org, p.Companies); err != nil {
			return err
		}
		var missing []Contact
		if err := tx.Where("org_id = ? and tz is null", org).Find(&missing).Error; err != nil {
			return err
		}
		for _, c := range missing {
			if zone := tz.Guess(deref(c.Phone), deref(c.Maker)); zone != nil {
				if err := tx.Model(&c).Update("tz", zone).Error; err != nil {
					return err
				}
			}
		}
		return tx.Model(&Contact{}).Where("org_id = ?", org).Count(&total).Error
	})
	return total, err
}

func prefer(fresh string, old *string) *string {
	if fresh != "" {
		return &fresh
	}
	return old
}

func upsertDraft(tx *gorm.DB, org int64, d Draft) error {
	var c Contact
	err := tx.Where("org_id = ? and key = ?", org, d.Key).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c = Contact{OrgID: org, Key: d.Key, File: d.File, Section: str(d.Section), Fit: str(d.Fit), Maker: str(d.Maker),
			Person: str(d.Person), First: str(d.First), Linkedin: str(d.Linkedin), Email: str(d.Email),
			Note: str(d.Note), Subject: str(d.Subject), Body: str(d.Body), Followup: str(d.Followup),
			Flags: str(d.Flags), Hold: d.Hold, Sources: nonEmpty(d.Sources), Phone: nonEmpty(d.Phone)}
		return tx.Create(&c).Error
	}
	if err != nil {
		return err
	}
	if c.Sources == nil {
		c.Sources = nonEmpty(d.Sources)
	}
	if c.Phone == nil {
		c.Phone = nonEmpty(d.Phone)
	}
	c.Fit, c.Maker, c.First = prefer(d.Fit, c.Fit), prefer(d.Maker, c.Maker), prefer(d.First, c.First)
	c.Linkedin, c.Email, c.Note = prefer(d.Linkedin, c.Linkedin), prefer(d.Email, c.Email), prefer(d.Note, c.Note)
	c.Subject, c.Body, c.Followup = prefer(d.Subject, c.Subject), prefer(d.Body, c.Body), prefer(d.Followup, c.Followup)
	c.Flags, c.File = prefer(d.Flags, c.Flags), d.File
	if c.Hold == nil || *c.Hold == "file" {
		c.Hold = d.Hold
	}
	return tx.Save(&c).Error
}

// seedSentLog: people listed under a date in sent-log.md were added on LinkedIn that day.
func seedSentLog(tx *gorm.DB, org int64, entries []SentLogEntry) error {
	for _, e := range entries {
		var rows []Contact
		if err := tx.Where("org_id = ? and linkedin_added is null", org).Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			if strings.Contains(e.Line, r.Key) {
				if err := tx.Model(&r).Update("linkedin_added", e.Date).Error; err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func knownMakers(tx *gorm.DB, org int64) (string, error) {
	var people []Contact
	if err := tx.Where("org_id = ? and key not like 'org:%'", org).Find(&people).Error; err != nil {
		return "", err
	}
	var makers []string
	for _, p := range people {
		makers = append(makers, strings.ToLower(deref(p.Maker)))
	}
	return strings.Join(makers, " | "), nil
}

// importCompanies keeps the Python tracker's "org:" placeholder contacts so the
// snapshot stays the same file; the companies table is filled by linkCompanies.
func importCompanies(tx *gorm.DB, org int64, companies []CompanyRow) error {
	known, err := knownMakers(tx, org)
	if err != nil {
		return err
	}
	for _, r := range companies {
		name := r["name"]
		if strings.Contains(known, companyStem(name)) {
			continue
		}
		hold := competitorHold(r)
		var flags []string
		for _, x := range []string{r["segment"], r["fit_reason"], labelled("Buyer: ", r["buyer_role"]),
			labelled("Configurator built by: ", r["configurator_built_by"])} {
			if x != "" {
				flags = append(flags, x)
			}
		}
		sources := strings.Join(urlRe.FindAllString(r["website"]+" "+r["configurator_url"], -1), "\n")
		words := strings.Fields(name)
		linkedin := "https://www.linkedin.com/search/results/people/?keywords=" +
			strings.Join(append(words[:min(2, len(words))], "marketing"), "%20")
		var c Contact
		err := tx.Where("org_id = ? and key = ?", org, "org:"+name).First(&c).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c = Contact{OrgID: org, Key: "org:" + name, File: "prospects.csv", Fit: str(r["priority"]), Maker: str(name),
				Person: str(""), First: str(""), Linkedin: &linkedin, Flags: str(strings.Join(flags, "\n")),
				Sources: nonEmpty(sources), Hold: hold}
			if err := tx.Create(&c).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		c.Fit, c.Flags = str(r["priority"]), str(strings.Join(flags, "\n"))
		if c.Sources == nil {
			c.Sources = nonEmpty(sources)
		}
		if h := deref(c.Hold); c.Hold == nil || h == "competitor" {
			c.Hold = hold
		}
		if err := tx.Save(&c).Error; err != nil {
			return err
		}
	}
	var orgs []Contact
	if err := tx.Where("org_id = ? and key like 'org:%'", org).Find(&orgs).Error; err != nil {
		return err
	}
	for _, o := range orgs {
		if strings.Contains(known, companyStem(deref(o.Maker))) {
			if err := tx.Delete(&o).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func labelled(label, v string) string {
	if v == "" {
		return ""
	}
	return label + v
}
