package main

import (
	"cmp"
	"strings"

	"gorm.io/gorm"
)

// What a company's facts say of one thing the organisation looks for.

// otherFacts are the facts not already shown as the answer to something the
// organisation looks for, older answers among them.
func otherFacts(set OrgSettings, facts []Fact) []Fact {
	shown := map[int64]bool{}
	for _, a := range assetsFound(set, facts) {
		if a.Fact != nil {
			shown[a.Fact.ID] = true
		}
	}
	var out []Fact
	for _, f := range facts {
		if !shown[f.ID] {
			out = append(out, f)
		}
	}
	return out
}

// configuratorFacts are the configurator_url, platform and built_by a company could
// carry before each organisation named its own assets, as facts. They cite the
// configurator's page, or nothing when there is none.
func configuratorFacts(url, platform, builtBy string) []FactIn {
	url, platform, builtBy = strings.TrimSpace(url), strings.TrimSpace(platform), strings.TrimSpace(builtBy)
	source := ""
	if httpURL.MatchString(url) {
		source = url
	}
	var out []FactIn
	add := func(field, value string) {
		if value != "" {
			out = append(out, FactIn{Field: field, Value: value, SourceURL: source})
		}
	}
	add("configurator", cmp.Or(url, platform))
	add("platform", platform)
	add("built_by", builtBy)
	return out
}

// addConfiguratorFacts stores configuratorFacts for a company and, when there are
// any, makes sure its organisation looks for a configurator, so they show.
func addConfiguratorFacts(tx *gorm.DB, org, company int64, url, platform, builtBy, actor string) (int, error) {
	facts := configuratorFacts(url, platform, builtBy)
	added := 0
	for _, f := range facts {
		_, had, err := addFact(tx, org, company, f, actor)
		if err != nil {
			return 0, err
		}
		if !had {
			added++
		}
	}
	if len(facts) == 0 {
		return 0, nil
	}
	var o Org
	if err := tx.First(&o, org).Error; err != nil {
		return 0, err
	}
	if strings.TrimSpace(o.Settings.LookFor) != "" {
		return added, nil
	}
	o.Settings.LookFor = "configurator: a product configurator or builder on their site"
	return added, tx.Model(&o).Select("settings").Updates(&o).Error
}
