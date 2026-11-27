package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/shutthegoatup/prospector/internal/dbtest"
)

// testDB is the run's Postgres, with one migrated template copied for each test that
// can run at the same time (see dbtest).
var testDB *dbtest.Server

// withDatabases makes the run's databases, runs the tests and drops them again.
func withDatabases(run func() int) int {
	// A hash at the real cost takes tens of milliseconds, and most tests sign someone in.
	passwordCost = bcrypt.MinCost
	dummyHash, _ = bcrypt.GenerateFromPassword([]byte("unused"), passwordCost)

	srv, stop, err := dbtest.Start(func(url string) error {
		s, err := Open(url)
		if err != nil {
			return err
		}
		closeStore(s)
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer stop()
	testDB = srv
	return run()
}

// tempStore lends the test an empty, migrated database for as long as it runs.
func tempStore(t *testing.T) *Store {
	t.Helper()
	return storeOn(t, testDB.Borrow(t))
}

// ownStore is a store on a database no other test will borrow, for a test that changes
// the schema.
func ownStore(t *testing.T) *Store {
	t.Helper()
	return storeOn(t, testDB.Fresh(t))
}

// freshDatabase is a migrated database for this test alone, for a test that changes
// its schema.
func freshDatabase(t *testing.T) string { return testDB.Fresh(t) }

// storeOn readies a store for a test and closes it when the test ends, before the
// database it is on is given back.
func storeOn(t *testing.T, url string) *Store {
	t.Helper()
	s, err := connect(url)
	if err != nil {
		t.Fatal(err)
	}
	limitConnections(s)
	if s.keys, err = parseKeys("1:" + testKey); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeStore(s) })
	return s
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
