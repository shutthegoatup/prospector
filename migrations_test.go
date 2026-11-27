package main

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

// Built by the server itself at start-up, best-effort, or by hand in the baseline: not
// something the models' tags describe.
var outsideTheModels = map[string]bool{
	"idx_companies_org_name_folded": true, "idx_companies_search": true, "idx_contacts_person_search": true,
	"idx_contacts_search": true, "idx_agent_runs_one_going": true,
	"goose_db_version": true, "goose_db_version_pkey": true,
}

// emptyStore is a database with nothing in it at all, not even the schema.
func emptyStore(t *testing.T) *Store {
	t.Helper()
	return storeOn(t, testDB.Bare(t))
}

// tablesOf is every column and index, one line each, for comparing two databases.
func tablesOf(t *testing.T, s *Store) map[string]bool {
	t.Helper()
	var lines []string
	if err := s.db.Raw(`select 'column ' || table_name || '.' || column_name || ' ' || data_type || ' null=' || is_nullable || ' default=' || coalesce(column_default, '')
		from information_schema.columns where table_schema = current_schema()
		union all select 'index ' || indexname || ' ' || indexdef from pg_indexes where schemaname = current_schema()`).Scan(&lines).Error; err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, l := range lines {
		name := strings.Fields(l)[1]
		if outsideTheModels[name] || strings.HasPrefix(name, "goose_db_version.") {
			continue
		}
		out[l] = true
	}
	return out
}

// TestTheMigrationsBuildWhatTheModelsSay keeps the models and the migrations in step: a
// field added to a model with no migration adding its column, or the other way round,
// fails here. Its column and index are named in the failure, ready to write one.
func TestTheMigrationsBuildWhatTheModelsSay(t *testing.T) {
	t.Parallel()
	migrated := tempStore(t)
	described := emptyStore(t)
	if err := described.db.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	have, want := tablesOf(t, migrated), tablesOf(t, described)
	for l := range want {
		if !have[l] {
			t.Errorf("the models have it but no migration makes it: %s", l)
		}
	}
	for l := range have {
		if !want[l] {
			t.Errorf("a migration makes it but no model has it: %s", l)
		}
	}
	// The release before goose ran AutoMigrate at start-up, so going back to it must still start.
	if err := migrated.db.AutoMigrate(models...); err != nil {
		t.Fatalf("the release before goose wouldn't start on a migrated database: %v", err)
	}
}

// TestADatabaseFromBeforeGooseIsOnlyRecorded: AutoMigrate made production's tables, so
// the baseline finds them there and changes nothing, rows included.
func TestADatabaseFromBeforeGooseIsOnlyRecorded(t *testing.T) {
	t.Parallel()
	s := emptyStore(t)
	if err := s.db.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	before := tablesOf(t, s)
	o := mustOrg(t, s, "alpha")
	if err := s.migrate(); err != nil {
		t.Fatal(err)
	}
	// The baseline is recorded as run; the migrations after it run as on any database.
	var baselines int64
	if err := s.db.Raw("select count(*) from goose_db_version where version_id = 1").Scan(&baselines).Error; err != nil || baselines != 1 {
		t.Fatalf("the baseline recorded %d times: %v", baselines, err)
	}
	if got := must(s.OrgBySlug("alpha")); got.ID != o.ID {
		t.Fatalf("the organisation was lost: %+v", got)
	}
	after := tablesOf(t, s)
	for l := range before {
		if !after[l] {
			t.Errorf("the baseline changed what was there: %s", l)
		}
	}
}

// TestGoingBackAReleaseStillStarts: a release that knows fewer migrations than the
// database has run, as after a deploy is rolled back, starts on it.
func TestGoingBackAReleaseStillStarts(t *testing.T) {
	t.Parallel()
	s := emptyStore(t)
	if err := s.migrate(); err != nil {
		t.Fatal(err)
	}
	later := fstest.MapFS{"migrations/00002_later.sql": {Data: []byte("-- +goose Up\ncreate table later_thing (id bigint primary key);\n")}}
	if err := s.migrateWith(later, "migrations"); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(); err != nil {
		t.Fatalf("an earlier release refused a database a later one migrated: %v", err)
	}
	var versions []int64
	if err := s.db.Raw("select version_id from goose_db_version where version_id > 0 order by version_id").Scan(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(versions) != "[1 2]" {
		t.Fatalf("versions recorded: %v", versions)
	}
}
