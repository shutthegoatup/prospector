package store

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// NotFound is an error about something that isn't here, or isn't this organisation's.
type NotFound string

func (e NotFound) Error() string { return string(e) }

func NoSuch(format string, args ...any) error { return NotFound(fmt.Sprintf(format, args...)) }

// Missing is what a failed look-up means: nothing there, unless the look-up itself
// failed, which mustn't read as the thing being gone.
func Missing(err error, format string, args ...any) error {
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return NoSuch(format, args...)
}

// Conflict is an error about something that can't be made because what is already here
// has its place: asking again won't help until one of the two changes.
type Conflict string

func (e Conflict) Error() string { return string(e) }

// BreaksRule is the part of unstorable that is a row against one of the database's
// rules, a unique index among them, and not a character it won't store.
func BreaksRule(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && strings.HasPrefix(pg.Code, "23")
}
