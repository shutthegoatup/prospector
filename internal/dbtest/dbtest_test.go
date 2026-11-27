package dbtest

import (
	"database/sql"
	"testing"
)

func count(t *testing.T, url string) int {
	t.Helper()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("select count(*) from things").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func add(t *testing.T, url string) {
	t.Helper()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("insert into things (name) values ('one')"); err != nil {
		t.Fatal(err)
	}
}

// TestABorrowedDatabaseComesBackEmpty: every copy carries the template's schema, and
// what one test leaves in it is gone before the next borrows it.
func TestABorrowedDatabaseComesBackEmpty(t *testing.T) {
	s, stop, err := Start(func(url string) error {
		db, err := sql.Open("pgx", url)
		if err != nil {
			return err
		}
		defer db.Close()
		_, err = db.Exec("create table things (id serial primary key, name text)")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	for i := range cap(s.pool) + 1 {
		t.Run("borrow", func(t *testing.T) {
			url := s.Borrow(t)
			if n := count(t, url); n != 0 {
				t.Fatalf("borrow %d found %d rows another test left", i, n)
			}
			add(t, url)
		})
	}
	if url := s.Fresh(t); count(t, url) != 0 {
		t.Fatal("a fresh copy isn't empty")
	}
}
