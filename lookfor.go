package main

import (
	"cmp"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// Asset is something an organisation looks for at a company, such as a product
// configurator, a careers page or an applicant tracking system. A company has it
// when it has a fact whose field is the asset's.
type Asset struct {
	Field string
	Hint  string
}

// assetKey lets "Careers page", "careers-page" and "careers_page" name one field.
func assetKey(field string) string {
	return underscores.Replace(strings.ToLower(strings.TrimSpace(field)))
}

var underscores = strings.NewReplacer(" ", "_", "-", "_")

// Assets reads LookFor, one "field: hint" per line; the hint is optional.
func (s OrgSettings) Assets() ([]Asset, error) {
	var out []Asset
	seen := map[string]bool{}
	for line := range strings.SplitSeq(s.LookFor, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		field, hint, _ := strings.Cut(line, ":")
		key := assetKey(field)
		if key == "" {
			return nil, fmt.Errorf("%q needs a name before the colon", strings.TrimSpace(line))
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Asset{Field: key, Hint: strings.TrimSpace(hint)})
	}
	return out, nil
}

type AssetFound struct {
	Asset
	Has bool
}

// assetsFound says which of the organisation's assets these facts show.
func assetsFound(set OrgSettings, facts []Fact) []AssetFound {
	assets, _ := set.Assets()
	have := map[string]bool{}
	for _, f := range facts {
		have[assetKey(f.Field)] = true
	}
	out := make([]AssetFound, len(assets))
	for i, a := range assets {
		out[i] = AssetFound{a, have[a.Field]}
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

// moveAssetColumns turns what companies hold in the old configurator columns into
// facts, credited to whoever found the company, then empties the columns so the next
// start finds nothing to move. They are kept so an earlier release still starts.
func moveAssetColumns(db *gorm.DB) error {
	for _, col := range []string{"configurator_url", "platform", "built_by"} {
		if !db.Migrator().HasColumn(&Company{}, col) {
			return nil
		}
	}
	const held = "coalesce(configurator_url, '') <> '' or coalesce(platform, '') <> '' or coalesce(built_by, '') <> ''"
	return db.Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			ID, OrgID                                   int64
			FoundBy, ConfiguratorURL, Platform, BuiltBy string
		}
		if err := tx.Raw(`select id, org_id, coalesce(found_by, '') as found_by, coalesce(configurator_url, '') as configurator_url,
			coalesce(platform, '') as platform, coalesce(built_by, '') as built_by from companies where ` + held).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			if _, err := addConfiguratorFacts(tx, r.OrgID, r.ID, r.ConfiguratorURL, r.Platform, r.BuiltBy, cmp.Or(r.FoundBy, "import")); err != nil {
				return err
			}
		}
		return tx.Exec(`update companies set configurator_url = '', platform = '', built_by = '' where ` + held).Error
	})
}
