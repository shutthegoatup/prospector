package main

import (
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
	return strings.NewReplacer(" ", "_", "-", "_").Replace(strings.ToLower(strings.TrimSpace(field)))
}

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
// configurator, else the company's site, else nothing.
func configuratorFacts(url, platform, builtBy, domain string) []FactIn {
	url, platform, builtBy = strings.TrimSpace(url), strings.TrimSpace(platform), strings.TrimSpace(builtBy)
	source := url
	if !httpURL.MatchString(source) {
		source = ""
		if d := bareDomain(domain); d != "" {
			source = "https://" + d
		}
	}
	var out []FactIn
	add := func(field, value string) {
		if value != "" {
			out = append(out, FactIn{Field: field, Value: value, SourceURL: source})
		}
	}
	add("configurator", orElse(url, platform))
	add("platform", platform)
	add("built_by", builtBy)
	return out
}

func orElse(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// moveAssetColumns turns what companies hold in the old configurator columns into
// facts, then empties the columns so the next start finds nothing to move. An
// organisation that had any gets "configurator" to look for, so it keeps seeing it.
func moveAssetColumns(db *gorm.DB) error {
	for _, col := range []string{"configurator_url", "platform", "built_by"} {
		if !db.Migrator().HasColumn(&Company{}, col) {
			return nil
		}
	}
	const held = "coalesce(configurator_url, '') <> '' or coalesce(platform, '') <> '' or coalesce(built_by, '') <> ''"
	return db.Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			ID, OrgID                                  int64
			Domain, ConfiguratorURL, Platform, BuiltBy string
		}
		if err := tx.Raw(`select id, org_id, coalesce(domain, '') as domain, coalesce(configurator_url, '') as configurator_url,
			coalesce(platform, '') as platform, coalesce(built_by, '') as built_by from companies where ` + held).Scan(&rows).Error; err != nil {
			return err
		}
		orgs := map[int64]bool{}
		for _, r := range rows {
			for _, f := range configuratorFacts(r.ConfiguratorURL, r.Platform, r.BuiltBy, r.Domain) {
				if _, _, err := addFact(tx, r.OrgID, r.ID, f, "import"); err != nil {
					return err
				}
			}
			orgs[r.OrgID] = true
		}
		for id := range orgs {
			var o Org
			if err := tx.First(&o, id).Error; err != nil {
				return err
			}
			if strings.TrimSpace(o.Settings.LookFor) == "" {
				o.Settings.LookFor = "configurator: a product configurator or builder on their site"
				if err := tx.Model(&o).Select("settings").Updates(&o).Error; err != nil {
					return err
				}
			}
		}
		return tx.Exec(`update companies set configurator_url = '', platform = '', built_by = '' where ` + held).Error
	})
}
