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
	// Must is a company being no fit without it, marked "(must)" after its name.
	Must bool
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
		field, must := cutMust(field)
		key := assetKey(field)
		if key == "" {
			return nil, fmt.Errorf("%q needs a name before the colon", strings.TrimSpace(line))
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Asset{Field: key, Hint: strings.TrimSpace(hint), Must: must})
	}
	return out, nil
}

func cutMust(field string) (string, bool) {
	trimmed := strings.TrimSpace(field)
	if len(trimmed) >= 6 && strings.EqualFold(trimmed[len(trimmed)-6:], "(must)") {
		return trimmed[:len(trimmed)-6], true
	}
	return trimmed, false
}

// What a company's facts say of one thing the organisation looks for.
const (
	assetMet       = "met"
	assetNotMet    = "not_met"
	assetNotFound  = "not_found"
	assetUnchecked = "unchecked"
)

type AssetFound struct {
	Asset
	// State is met, not_met, not_found, or unchecked when no fact speaks to it; Fact is
	// the latest that does.
	State string
	Fact  *Fact
	Has   bool
}

// assetsFound says, for each of the organisation's assets, what the latest fact about
// it shows. The facts come oldest first.
func assetsFound(set OrgSettings, facts []Fact) []AssetFound {
	assets, _ := set.Assets()
	latest := map[string]*Fact{}
	for i := range facts {
		latest[assetKey(facts[i].Field)] = &facts[i]
	}
	out := make([]AssetFound, len(assets))
	for i, a := range assets {
		found := AssetFound{Asset: a, State: assetUnchecked, Fact: latest[a.Field]}
		if f := found.Fact; f != nil {
			switch f.Answer {
			case answerNo:
				found.State = assetNotMet
			case answerUnknown:
				found.State = assetNotFound
			default:
				found.State = assetMet
			}
		}
		found.Has = found.State == assetMet
		out[i] = found
	}
	return out
}

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

// Fit is how well a company matches what the organisation looks for.
type Fit struct {
	Of, Met, NotFound, Unchecked int
	// MustsMissed are the musts it doesn't have, or that no fact shows yet.
	MustsMissed int
}

func fitOf(found []AssetFound) Fit {
	f := Fit{Of: len(found)}
	for _, a := range found {
		switch a.State {
		case assetMet:
			f.Met++
		case assetNotFound:
			f.NotFound++
		case assetUnchecked:
			f.Unchecked++
		}
		if a.Must && a.State != assetMet {
			f.MustsMissed++
		}
	}
	return f
}

// better orders fits best first: no must missed, then most met, then fewest left unknown.
func (f Fit) better(g Fit) bool {
	if f.MustsMissed != g.MustsMissed {
		return f.MustsMissed < g.MustsMissed
	}
	if f.Met != g.Met {
		return f.Met > g.Met
	}
	return f.NotFound+f.Unchecked < g.NotFound+g.Unchecked
}

func (f Fit) Text() string {
	s := fmt.Sprintf("%d of %d", f.Met, f.Of)
	if f.NotFound > 0 {
		s += fmt.Sprintf(" · %d not found", f.NotFound)
	}
	if f.Unchecked > 0 {
		s += fmt.Sprintf(" · %d unchecked", f.Unchecked)
	}
	if f.MustsMissed > 0 {
		s += " · misses a must"
	}
	return s
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
