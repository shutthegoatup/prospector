package main

import (
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"github.com/shutthegoatup/prospector/internal/privacy"
)

func tableOf(t *testing.T, s *Store, model any) *schema.Schema {
	t.Helper()
	stmt := &gorm.Statement{DB: s.DB}
	if err := stmt.Parse(model); err != nil {
		t.Fatal(err)
	}
	return stmt.Schema
}

func jsonFields(v any) map[string]bool {
	out := map[string]bool{}
	for i, typ := 0, reflect.TypeOf(v); i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		out[name] = true
	}
	return out
}

// Where the organisation's export carries each table that belongs to an organisation,
// or after "not exported:" why it doesn't. A new table has to be given a line here.
var orgExportHolds = map[string]string{
	"memberships":          "members",
	"api_keys":             "api_keys",
	"sso_connections":      "single_sign_on",
	"sso_domains":          "single_sign_on",
	"org_mails":            "mail_server",
	"companies":            "companies",
	"facts":                "facts",
	"contacts":             "contacts",
	"deals":                "deals",
	"subscriptions":        "subscriptions",
	"onboardings":          "onboardings",
	"onboarding_steps":     "onboarding_steps",
	"customer_grants":      "onboarding_access",
	"customer_links":       "not exported: the hash of a link mailed to a customer, which opens their page once and expires",
	"customer_sessions":    "not exported: the hash of a cookie in a customer's browser, which expires",
	"enquiries":            "enquiries",
	"enquiry_messages":     "enquiry_messages",
	"suppressions":         "not_to_be_mailed",
	"bounce_hooks":         "not exported: the hash of the secret Postmark's webhook signs in with, which can only add bounces and complaints to the list",
	"unsubscribe_links":    "not exported: the hash of the secret in one mail's unsubscribe link, which can only unsubscribe that address",
	"campaigns":            "campaigns",
	"agreements":           "agreements",
	"agreement_files":      "not exported: the documents themselves, downloaded from each agreement; the export carries each one's SHA-256",
	"campaign_recipients":  "campaign_mail",
	"agreement_recipients": "agreement_recipients",
	"agreement_fields":     "agreement_fields",
	"agreement_events":     "agreement_events",
	"seal_keys":            "not exported: the organisation's private key for sealing agreements; its certificate is in every PDF it sealed",
	"suggestions":          "suggested_details",
	"tasks":                "tasks",
	"task_requests":        "task_requests",
	"task_notes":           "task_notes",
	"events":               "history",
	"erasures":             "people_erased",
	"request_claims":       "not exported: says which agent is on a request for the next few minutes, and lapses by itself",
	"agents":               "agents",
	"offerings":            "offerings",
	"verdicts":             "verdicts",
	"roles":                "roles",
	"enrichers":            "enrichment_vendors",
	"enrich_usages":        "not exported: how many lookups each vendor was asked this month, which only its limit needs",
	"agent_runs":           "not exported: counts, a lease and why each run ended; what a run did is in history, as the built-in agent's events",
}

// Where one person's export carries each table that points at a contact.
var personExportHolds = map[string]string{
	"facts":                "facts",
	"enquiries":            "enquiries",
	"suggestions":          "suggested_details",
	"deals":                "deals",
	"customer_grants":      "onboarding_access",
	"campaign_recipients":  "campaign_mail",
	"agreement_recipients": "agreements_to_sign",
	"events":               "history",
}

func TestEveryTableIsInTheExportsOrSaysWhyNot(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	check := func(table string, holds map[string]string, export any, name string) {
		field, listed := holds[table]
		switch {
		case !listed:
			t.Errorf("%s isn't accounted for in %s: carry it in the export and name the field in tables_test.go, or say there why it is left out", table, name)
		case !strings.HasPrefix(field, "not exported: ") && !jsonFields(export)[field]:
			t.Errorf("%s is said to be in %s as %q, which it doesn't have", table, name, field)
		}
	}
	for _, m := range models {
		sch := tableOf(t, s, m)
		if sch.LookUpField("org_id") != nil {
			check(sch.Table, orgExportHolds, privacy.OrgExport{}, "the organisation's export")
		}
		if sch.LookUpField("contact_id") != nil {
			check(sch.Table, personExportHolds, privacy.PersonExport{}, "a person's export")
		}
	}
}

// pointingAt puts one otherwise empty row in every table that points at a contact,
// and returns how many of those rows are left.
func pointingAt(t *testing.T, s *Store, org, contact int64) func() map[string]int64 {
	t.Helper()
	var tables []*schema.Schema
	for _, m := range models {
		sch := tableOf(t, s, m)
		at := sch.LookUpField("contact_id")
		if at == nil {
			continue
		}
		tables = append(tables, sch)
		row := reflect.New(sch.ModelType)
		for name, v := range map[string]int64{"org_id": org, "contact_id": contact} {
			if err := sch.LookUpField(name).Set(s.DB.Statement.Context, row, v); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.DB.Create(row.Interface()).Error; err != nil {
			t.Fatalf("%s: %v", sch.Table, err)
		}
	}
	return func() map[string]int64 {
		left := map[string]int64{}
		for _, sch := range tables {
			var n int64
			// Under the id negated too, where a restore parks rows while it renumbers people.
			if err := s.DB.Table(sch.Table).Where("org_id = ? and contact_id in ?", org, []int64{contact, -contact}).Count(&n).Error; err != nil {
				t.Fatal(err)
			}
			if n > 0 {
				left[sch.Table] = n
			}
		}
		return left
	}
}

func TestWhateverPointsAtAPersonGoesWhenTheyDo(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	add := func(name string) *Contact {
		return must(s.QuickAddPerson(o, QuickAdd{Name: name, Company: "Larkmoor"}, "Nia"))
	}

	erased := add("Ada Vale")
	left := pointingAt(t, s, o.ID, erased.ID)
	if len(left()) < len(privacy.PointAtContacts) {
		t.Fatalf("the test put rows in fewer tables than point at a contact: %v", left())
	}
	if err := s.ErasePerson(o.ID, erased.ID, "Nia"); err != nil {
		t.Fatal(err)
	}
	if still := left(); len(still) > 0 {
		t.Errorf("erasing a person left rows that point at them, for whoever is given their id next: %v", still)
	}

	stays, dropped := add("Bram Osei"), add("Cleo Marsh")
	left = pointingAt(t, s, o.ID, dropped.ID)
	if _, err := s.ReplaceContacts(o.ID, []Contact{*stays}, nil); err != nil {
		t.Fatal(err)
	}
	if still := left(); len(still) > 0 {
		t.Errorf("a restore without someone left rows that point at them: %v", still)
	}
}

// follow moves what points at a contact when a restore gives them another id, table by
// table from pointAtContacts.
func TestARestoreMovesEveryTableThatPointsAtAContact(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	moved := map[string]bool{}
	for _, m := range privacy.PointAtContacts {
		moved[tableOf(t, s, m).Table] = true
	}
	for _, m := range models {
		if sch := tableOf(t, s, m); sch.LookUpField("contact_id") != nil && !moved[sch.Table] {
			t.Errorf("%s points at a contact and isn't in pointAtContacts, so a restore that renumbers people would leave it behind", sch.Table)
		}
	}
}
