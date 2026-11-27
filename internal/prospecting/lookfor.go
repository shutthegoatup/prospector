package prospecting

import (
	"fmt"

	"github.com/shutthegoatup/prospector/internal/store"
)

const AssetMet = "met"

const AssetNotMet = "not_met"

const AssetNotFound = "not_found"

const AssetUnchecked = "unchecked"

type AssetFound struct {
	store.Asset
	// State is met, not_met, not_found, or unchecked when no fact speaks to it; Fact is
	// the latest that does.
	State string
	Fact  *store.Fact
	Has   bool
}

// AssetsFound says, for each of the organisation's assets, what the latest fact about
// it shows. The facts come oldest first.
func AssetsFound(set store.OrgSettings, facts []store.Fact) []AssetFound {
	assets, _ := set.Assets()
	latest := map[string]*store.Fact{}
	for i := range facts {
		latest[store.AssetKey(facts[i].Field)] = &facts[i]
	}
	out := make([]AssetFound, len(assets))
	for i, a := range assets {
		found := AssetFound{Asset: a, State: AssetUnchecked, Fact: latest[a.Field]}
		if f := found.Fact; f != nil {
			switch f.Answer {
			case AnswerNo:
				found.State = AssetNotMet
			case AnswerUnknown:
				found.State = AssetNotFound
			default:
				found.State = AssetMet
			}
		}
		found.Has = found.State == AssetMet
		out[i] = found
	}
	return out
}

// Fit is how well a company matches what the organisation looks for.
type Fit struct {
	Of, Met, NotFound, Unchecked int
	// MustsMissed are the musts it doesn't have, or that no fact shows yet.
	MustsMissed int
}

func FitOf(found []AssetFound) Fit {
	f := Fit{Of: len(found)}
	for _, a := range found {
		switch a.State {
		case AssetMet:
			f.Met++
		case AssetNotFound:
			f.NotFound++
		case AssetUnchecked:
			f.Unchecked++
		}
		if a.Must && a.State != AssetMet {
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
