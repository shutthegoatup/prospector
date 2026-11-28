package privacy

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
	"github.com/shutthegoatup/prospector/internal/tz"
)

// Import merges a parsed repo into one organisation. Text from the markdown refreshes,
// but a date or edit recorded in the tracker is never cleared. It is refused whole if it
// would bring in an address on the never-contact list; one already stored may stay, so
// listing someone who opted out doesn't break every later import.
func (s *PrivacyStore) Import(org int64, p *ImportPayload, actor string) (int64, error) {
	o, err := s.Identity.OrgByID(org)
	if err != nil {
		return 0, err
	}
	var blocked []string
	stored := func(fresh string, have *string) bool {
		return strings.EqualFold(strings.TrimSpace(fresh), strings.TrimSpace(store.Deref(have)))
	}
	for _, d := range p.Drafts {
		if err := crm.ErasedIn(s.DB, org, d.Person, d.Maker, d.Email, d.Linkedin); err != nil {
			if !errors.Is(err, store.ErrErased) {
				return 0, err
			}
			blocked = append(blocked, fmt.Sprintf("%s in %s was erased on request; take them out of the file", d.Key, d.File))
		}
		email, link := s.CRM.Allowed(o, d.Email, ""), s.CRM.Allowed(o, "", d.Linkedin)
		if email == nil && link == nil {
			continue
		}
		var have store.Contact
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
		if err := LinkCompanies(tx, org, p.Companies); err != nil {
			return err
		}
		var missing []store.Contact
		if err := tx.Where("org_id = ? and tz is null", org).Find(&missing).Error; err != nil {
			return err
		}
		for _, c := range missing {
			if zone := tz.Guess(store.Deref(c.Phone), store.Deref(c.Maker)); zone != nil {
				if err := tx.Model(&c).Update("tz", zone).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Model(&store.Contact{}).Where("org_id = ?", org).Count(&total).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, nil, actor, store.ActImport, new(strconv.FormatInt(total, 10)))
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
	var c store.Contact
	err := tx.Where("org_id = ? and key = ?", org, d.Key).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c = store.Contact{OrgID: org, Key: d.Key, File: d.File, Section: new(d.Section), Fit: new(d.Fit), Maker: new(d.Maker),
			Person: new(d.Person), First: new(d.First), Linkedin: new(d.Linkedin), Email: new(d.Email),
			Note: new(d.Note), Subject: new(d.Subject), Body: new(d.Body), Followup: new(d.Followup),
			Flags: new(d.Flags), Hold: d.Hold, Sources: store.NonEmpty(d.Sources), Phone: store.NonEmpty(d.Phone)}
		return tx.Create(&c).Error
	}
	if err != nil {
		return err
	}
	if c.Sources == nil {
		c.Sources = store.NonEmpty(d.Sources)
	}
	if c.Phone == nil {
		c.Phone = store.NonEmpty(d.Phone)
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
		var rows []store.Contact
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
	var people []store.Contact
	if err := tx.Where("org_id = ? and key not like 'org:%'", org).Find(&people).Error; err != nil {
		return "", err
	}
	var makers []string
	for _, p := range people {
		makers = append(makers, strings.ToLower(store.Deref(p.Maker)))
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
		if strings.Contains(known, CompanyStem(name)) {
			continue
		}
		hold := CompetitorHold(r)
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
		var c store.Contact
		err := tx.Where("org_id = ? and key = ?", org, "org:"+name).First(&c).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c = store.Contact{OrgID: org, Key: "org:" + name, File: "prospects.csv", Fit: new(r["priority"]), Maker: new(name),
				Person: new(""), First: new(""), Linkedin: &linkedin, Flags: new(strings.Join(flags, "\n")),
				Sources: store.NonEmpty(sources), Hold: hold}
			if err := tx.Create(&c).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		c.Fit, c.Flags = new(r["priority"]), new(strings.Join(flags, "\n"))
		if c.Sources == nil {
			c.Sources = store.NonEmpty(sources)
		}
		if h := store.Deref(c.Hold); c.Hold == nil || h == "competitor" {
			c.Hold = hold
		}
		if err := tx.Save(&c).Error; err != nil {
			return err
		}
	}
	var orgs []store.Contact
	if err := tx.Where("org_id = ? and key like 'org:%'", org).Find(&orgs).Error; err != nil {
		return err
	}
	for _, o := range orgs {
		if strings.Contains(known, CompanyStem(store.Deref(o.Maker))) {
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
