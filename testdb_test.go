package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
)

// The tests run on Postgres. A run migrates one template database, then copies it once
// for each test that can run at the same time. A test borrows a copy and empties it
// when it is done, so nothing is migrated per test. Every database is named for the
// run and dropped at its end, so runs sharing a server don't meet.
var testDB struct {
	admin    *sql.DB
	server   *url.URL
	template string
	pool     chan string
	empty    string
}

const testPGHelp = `the tests need a Postgres they may create databases on. For example:
  docker run -d --rm --name prospector-test-pg -e POSTGRES_PASSWORD=test -p 127.0.0.1:55439:5432 postgres:18
  export PROSPECTOR_TEST_PG='postgres://postgres:test@127.0.0.1:55439/postgres?sslmode=disable'`

// withDatabases makes the run's databases, runs the tests and drops them again.
func withDatabases(run func() int) int {
	// A hash at the real cost takes tens of milliseconds, and most tests sign someone in.
	passwordCost = bcrypt.MinCost
	dummyHash, _ = bcrypt.GenerateFromPassword([]byte("unused"), passwordCost)

	raw := os.Getenv("PROSPECTOR_TEST_PG")
	if raw == "" {
		fmt.Fprintln(os.Stderr, "PROSPECTOR_TEST_PG isn't set:", testPGHelp)
		return 1
	}
	server, err := url.Parse(raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, "PROSPECTOR_TEST_PG:", err)
		return 1
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer admin.Close()
	testDB.admin, testDB.server = admin, server

	clearStaleDatabases()
	// Named for when the run began, so a later run can tell this one's leftovers from
	// another run still going.
	id := make([]byte, 4)
	rand.Read(id)
	runName := fmt.Sprintf("%s%d_%s", testDBPrefix, time.Now().Unix(), hex.EncodeToString(id))
	var made []string
	defer func() {
		for _, name := range made {
			dropDatabase(name)
		}
	}()
	create := func(name, from string) error {
		stmt := "create database " + name
		if from != "" {
			stmt += " template " + from
		}
		if _, err := admin.Exec(stmt); err != nil {
			return err
		}
		made = append(made, name)
		return nil
	}

	testDB.template = runName
	if err := create(runName, ""); err != nil {
		fmt.Fprintln(os.Stderr, "making the template database:", err, "\n"+testPGHelp)
		return 1
	}
	s, err := Open(databaseURL(runName))
	if err != nil {
		fmt.Fprintln(os.Stderr, "migrating the template database:", err)
		return 1
	}
	// Emptied by deleting, which is far quicker than truncating tables this small, with
	// foreign keys unchecked so the order doesn't matter, and ids counting from 1 again.
	err = s.db.Raw(`select 'set session_replication_role = replica; '
		|| (select string_agg('delete from ' || quote_ident(tablename), '; ') from pg_tables where schemaname = current_schema())
		|| coalesce('; ' || (select string_agg('alter sequence ' || quote_ident(sequencename) || ' restart', '; ') from pg_sequences where schemaname = current_schema()), '')`).
		Scan(&testDB.empty).Error
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// Postgres copies a database only while nobody is connected to it.
	closeStore(s)

	n := min(runtime.GOMAXPROCS(0), 8)
	testDB.pool = make(chan string, n)
	for i := range n {
		name := fmt.Sprintf("%s_%d", runName, i)
		if err := create(name, runName); err != nil {
			fmt.Fprintln(os.Stderr, "copying the template database:", err)
			return 1
		}
		testDB.pool <- name
	}
	return run()
}

const testDBPrefix = "prospector_test_"

// clearStaleDatabases drops what runs killed by a timeout or a panic left behind. Only
// those begun over an hour ago: a newer one may belong to a run in another session.
func clearStaleDatabases() {
	rows, err := testDB.admin.Query("select datname from pg_database where datname like $1", testDBPrefix+"%")
	if err != nil {
		return
	}
	var stale []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) != nil {
			continue
		}
		began, _, _ := strings.Cut(strings.TrimPrefix(name, testDBPrefix), "_")
		if at, err := strconv.ParseInt(began, 10, 64); err == nil && time.Since(time.Unix(at, 0)) > time.Hour {
			stale = append(stale, name)
		}
	}
	rows.Close()
	for _, name := range stale {
		dropDatabase(name)
	}
}

func databaseURL(name string) string {
	u := *testDB.server
	u.Path = "/" + name
	return u.String()
}

func dropDatabase(name string) {
	if _, err := testDB.admin.Exec("drop database if exists " + name + " with (force)"); err != nil {
		fmt.Fprintln(os.Stderr, "dropping", name+":", err)
	}
}

// tempStore lends the test an empty, migrated database for as long as it runs. With
// none free it makes another rather than wait: a test holding one while it waits for a
// second could otherwise wait for good.
func tempStore(t *testing.T) *Store {
	t.Helper()
	var name string
	select {
	case name = <-testDB.pool:
	default:
		return ownStore(t)
	}
	s, err := connect(databaseURL(name))
	if err != nil {
		testDB.pool <- name
		t.Fatal(err)
	}
	lent(t, s)
	t.Cleanup(func() {
		defer func() { testDB.pool <- name }()
		closeStore(s)
		if err := emptyDatabase(name); err != nil {
			// A copy that couldn't be emptied would hand the next test its rows.
			dropDatabase(name)
			if _, err := testDB.admin.Exec("create database " + name + " template " + testDB.template); err != nil {
				t.Errorf("replacing database %s: %v", name, err)
			}
		}
	})
	return s
}

// ownStore is a store on a database no other test will borrow, for a test that changes
// the schema.
func ownStore(t *testing.T) *Store {
	t.Helper()
	s, err := connect(freshDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	return lent(t, s)
}

// lent readies a store for a test and closes it when the test ends.
func lent(t *testing.T, s *Store) *Store {
	t.Helper()
	limitConnections(s)
	var err error
	if s.keys, err = parseKeys("1:" + testKey); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeStore(s) })
	return s
}

// freshDatabase is a migrated database for this test alone, for a test that changes
// its schema, as a test of migrating from an older release does.
func freshDatabase(t *testing.T) string {
	t.Helper()
	id := make([]byte, 4)
	rand.Read(id)
	name := testDB.template + "_" + hex.EncodeToString(id)
	if _, err := testDB.admin.Exec("create database " + name + " template " + testDB.template); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dropDatabase(name) })
	return databaseURL(name)
}

func emptyDatabase(name string) error {
	db, err := sql.Open("pgx", databaseURL(name))
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(testDB.empty)
	return err
}

// Each test holds its own pool, and Postgres takes only so many connections at once.
func limitConnections(s *Store) {
	if db, err := s.db.DB(); err == nil {
		db.SetMaxOpenConns(10)
	}
}

var testKey = strings.Repeat("0a", 32)

func closeStore(s *Store) {
	if db, err := s.db.DB(); err == nil {
		db.Close()
	}
}
