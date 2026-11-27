package main

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
