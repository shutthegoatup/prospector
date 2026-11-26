package main

import "testing"

// A segment says what a company is before what it serves, so "CGI studio for carmakers"
// is a studio, not a carmaker.
func TestASegmentRuleReadsOnlyWhatTheCompanyIs(t *testing.T) {
	manufacturers := "carmaker, manufacturer, OEM"
	for _, c := range []struct {
		segment, words string
		want           bool
	}{
		{"CGI studio for carmakers", manufacturers, false},
		{"CGI studio for carmakers", "studio", true},
		{"agency for manufacturers", "agency", true},
		{"agency for manufacturers", manufacturers, false},
		{"Boatbuilder", "boatbuilder", true},
		{"Carmakers", "carmaker", true},
		{"Premium carmaker serving fleet buyers", manufacturers, true},
		{"Studio to the trade", "trade", false},
		{"OEM with an online configurator", "oem", true},
		{"Visualisation studios", "studio", true},
		{"Studio", "studios", false},
		{"", "studio", false},
		{"Interior design studio", "interior design", true},
		{"Interior studio", "interior design", false},
	} {
		if _, got := segmentMatch(c.segment, splitList(c.words)); got != c.want {
			t.Errorf("segment %q against %q: %v, want %v", c.segment, c.words, got, c.want)
		}
	}
}
