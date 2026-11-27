package privacy

import (
	"strings"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/store"
)

func CompanyStem(maker string) string {
	return strings.Trim(strings.ToLower(prospecting.FirstField(maker)), "(")
}

func CompetitorHold(r CompanyRow) *string {
	if strings.Contains(r["fit_reason"], "COMPETITOR") && !strings.Contains(r["fit_reason"], "PARTNER") {
		return store.Str("competitor")
	}
	return nil
}

// LinkCompanies gives every prospects.csv row and every contact's maker a company,
// and points each contact at it.
func LinkCompanies(tx *gorm.DB, org int64, rows []CompanyRow) error {
	byStem := map[string]int64{}
	for _, r := range rows {
		c, created, err := prospecting.EnsureCompany(tx, org, store.Company{Name: r["name"], Country: r["country"], Segment: r["segment"],
			Fit: r["priority"], Stage: prospecting.CompanyQualified,
			Why: r["fit_reason"], Domain: store.DomainOf(r["website"]), FoundBy: "import", Hold: CompetitorHold(r)})
		if err != nil {
			return err
		}
		if _, err := research.AddConfiguratorFacts(tx, org, c.ID, r["configurator_url"], "", r["configurator_built_by"], "import"); err != nil {
			return err
		}
		if !created {
			if err := tx.Model(c).Updates(map[string]any{"fit": r["priority"], "segment": r["segment"], "why": r["fit_reason"]}).Error; err != nil {
				return err
			}
		}
		byStem[CompanyStem(r["name"])] = c.ID
	}
	var people []store.Contact
	if err := tx.Where("org_id = ? and company_id is null", org).Find(&people).Error; err != nil {
		return err
	}
	for _, p := range people {
		maker := store.Deref(p.Maker)
		id, ok := byStem[CompanyStem(maker)]
		if !ok {
			if store.CompanyName(maker) == "" {
				continue
			}
			c, _, err := prospecting.EnsureCompany(tx, org, store.Company{Name: store.CompanyName(maker), Stage: prospecting.CompanyQualified, FoundBy: "import"})
			if err != nil {
				return err
			}
			id = c.ID
			byStem[CompanyStem(maker)] = id
		}
		if err := tx.Model(&p).Update("company_id", id).Error; err != nil {
			return err
		}
	}
	return nil
}
