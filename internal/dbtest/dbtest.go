// Package dbtest gives tests Postgres databases. A run migrates one template database,
// then copies it once for each test that can run at the same time. A test borrows a
// copy and empties it when it is done, so nothing is migrated per test. Every database
// is named for the run and dropped at its end, so runs sharing a server don't meet.
package dbtest

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Help is what to do when there is no server to make databases on.
const Help = `the tests need a Postgres they may create databases on. For example:
  docker run -d --rm --name prospector-test-pg -e POSTGRES_PASSWORD=test -p 127.0.0.1:55439:5432 postgres:18
  export PROSPECTOR_TEST_PG='postgres://postgres:test@127.0.0.1:55439/postgres?sslmode=disable'`

const prefix = "prospector_test_"

// Server is the Postgres a run makes its databases on, from PROSPECTOR_TEST_PG.
type Server struct {
	admin    *sql.DB
	base     *url.URL
	template string
	pool     chan string
	empty    string
	made     []string
}

// Start makes the run's databases. migrate brings the template, at the URL it is given,
// up to date, and must close its connections: Postgres copies a database only while
// nobody is connected to it. Stop drops every database the run made.
func Start(migrate func(url string) error) (s *Server, stop func(), err error) {
	raw := os.Getenv("PROSPECTOR_TEST_PG")
	if raw == "" {
		return nil, nil, errors.New("PROSPECTOR_TEST_PG isn't set: " + Help)
	}
	base, err := url.Parse(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("PROSPECTOR_TEST_PG: %w", err)
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		return nil, nil, err
	}
	s = &Server{admin: admin, base: base}
	stop = func() {
		for _, name := range s.made {
			s.drop(name)
		}
		_ = admin.Close()
	}
	defer func() {
		if err != nil {
			stop()
		}
	}()
	s.clearStale()
	// Named for when the run began, so a later run can tell this one's leftovers from
	// another run still going.
	s.template = fmt.Sprintf("%s%d_%s", prefix, time.Now().Unix(), randomHex())
	if err = s.create(s.template, ""); err != nil {
		return nil, nil, fmt.Errorf("making the template database: %w\n%s", err, Help)
	}
	if err = migrate(s.URL(s.template)); err != nil {
		return nil, nil, fmt.Errorf("migrating the template database: %w", err)
	}
	if s.empty, err = emptying(s.URL(s.template)); err != nil {
		return nil, nil, err
	}
	n := min(runtime.GOMAXPROCS(0), 8)
	s.pool = make(chan string, n)
	for i := range n {
		name := fmt.Sprintf("%s_%d", s.template, i)
		if err = s.create(name, s.template); err != nil {
			return nil, nil, fmt.Errorf("copying the template database: %w", err)
		}
		s.pool <- name
	}
	return s, stop, nil
}

// emptying is the SQL that empties a copy: deleting, which is far quicker than
// truncating tables this small, with foreign keys unchecked so the order doesn't
// matter, and ids counting from 1 again. Which migrations ran stays, as it would in a
// real database.
func emptying(template string) (string, error) {
	db, err := sql.Open("pgx", template)
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()
	var stmt string
	return stmt, db.QueryRow(`select 'set session_replication_role = replica; '
		|| (select string_agg('delete from ' || quote_ident(tablename), '; ') from pg_tables where schemaname = current_schema() and tablename <> 'goose_db_version')
		|| coalesce('; ' || (select string_agg('alter sequence ' || quote_ident(sequencename) || ' restart', '; ') from pg_sequences where schemaname = current_schema() and sequencename <> 'goose_db_version_id_seq'), '')`).
		Scan(&stmt)
}

// Borrow lends the test an empty, migrated database for as long as it runs. With none
// free it makes another rather than wait: a test holding one while it waits for a
// second could otherwise wait for good. Whatever connects to it must be closed by a
// cleanup registered after Borrow, so it runs first.
func (s *Server) Borrow(t testing.TB) string {
	t.Helper()
	var name string
	select {
	case name = <-s.pool:
	default:
		return s.Fresh(t)
	}
	t.Cleanup(func() {
		defer func() { s.pool <- name }()
		if err := s.runSQL(name, s.empty); err != nil {
			// A copy that couldn't be emptied would hand the next test its rows.
			s.drop(name)
			if _, err := s.admin.Exec("create database " + name + " template " + s.template); err != nil {
				t.Errorf("replacing database %s: %v", name, err)
			}
		}
	})
	return s.URL(name)
}

// Fresh is a migrated database for this test alone, for a test that changes its schema.
func (s *Server) Fresh(t testing.TB) string {
	t.Helper()
	name := s.template + "_" + randomHex()
	if _, err := s.admin.Exec("create database " + name + " template " + s.template); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.drop(name) })
	return s.URL(name)
}

// Bare is a database with nothing in it at all, not even the schema.
func (s *Server) Bare(t testing.TB) string {
	t.Helper()
	name := s.template + "_bare_" + randomHex()
	if _, err := s.admin.Exec("create database " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.drop(name) })
	return s.URL(name)
}

// URL is how to reach one of the server's databases.
func (s *Server) URL(name string) string {
	u := *s.base
	u.Path = "/" + name
	return u.String()
}

func (s *Server) create(name, from string) error {
	stmt := "create database " + name
	if from != "" {
		stmt += " template " + from
	}
	if _, err := s.admin.Exec(stmt); err != nil {
		return err
	}
	s.made = append(s.made, name)
	return nil
}

func (s *Server) runSQL(name, stmt string) error {
	db, err := sql.Open("pgx", s.URL(name))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	_, err = db.Exec(stmt)
	return err
}

func (s *Server) drop(name string) {
	if _, err := s.admin.Exec("drop database if exists " + name + " with (force)"); err != nil {
		fmt.Fprintln(os.Stderr, "dropping", name+":", err)
	}
}

// clearStale drops what runs killed by a timeout or a panic left behind. Only those
// begun over an hour ago: a newer one may belong to a run in another session.
func (s *Server) clearStale() {
	rows, err := s.admin.Query("select datname from pg_database where datname like $1", prefix+"%")
	if err != nil {
		return
	}
	var stale []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) != nil {
			continue
		}
		began, _, _ := strings.Cut(strings.TrimPrefix(name, prefix), "_")
		if at, err := strconv.ParseInt(began, 10, 64); err == nil && time.Since(time.Unix(at, 0)) > time.Hour {
			stale = append(stale, name)
		}
	}
	_ = rows.Close()
	for _, name := range stale {
		s.drop(name)
	}
}

func randomHex() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}
