package app

import (
	"maps"
	"slices"
	"testing"

	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/store"
)

// Every action the store can write has words in the history, so nothing reaches a page
// as the bare name it is stored by, and no two actions share a name.
func TestEveryActionIsWorded(t *testing.T) {
	t.Parallel()
	all := store.Actions()
	stages := []string{crm.CompanyQualified, crm.CompanyRejected, crm.CompanyReview}
	notes := []string{}
	for _, k := range research.NoteKinds {
		notes = append(notes, k.Key)
	}
	for family, parts := range map[store.Family][]string{
		store.Marked:    slices.Collect(maps.Keys(crm.Marks)),
		store.Noted:     notes,
		store.CompanyTo: stages,
		store.VerdictTo: stages,
		store.RoleTo:    {research.RoleReview, research.RolePursued, research.RoleJunked},
		// What an agent or a vendor may suggest of a person, and what a vendor looks up.
		store.Suggested:    {"email", "phone", "linkedin"},
		store.Dismissed:    {"email", "phone", "linkedin"},
		store.Enriched:     {"email", "phone"},
		store.OnboardField: {"due"},
		// Any field: these are worded with the field's own name.
		store.SetField: {"stage"}, store.DealField: {"stage"}, store.SubField: {"amount"}, store.EnquiryField: {"state"}, store.Drafted: {"email"},
	} {
		for _, part := range parts {
			all = append(all, family.Of(part))
		}
	}
	seen := map[string]bool{}
	for _, a := range all {
		if !crm.Worded(a) {
			t.Errorf("the history has no words for %q: add them to historyVerbs", a)
		}
		if seen[a.String()] {
			t.Errorf("%q is named twice", a)
		}
		seen[a.String()] = true
	}
}
