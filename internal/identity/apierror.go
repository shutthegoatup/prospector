package identity

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/shutthegoatup/prospector/internal/store"
)

// Unstorable is the database turning the data down: a row that breaks one of its rules,
// such as a name already taken, or a character Postgres won't store (classes 22 and
// 23). Trying again with the same request can't help.
func Unstorable(err error) bool {
	var pg *pgconn.PgError
	return store.BreaksRule(err) || errors.As(err, &pg) && strings.HasPrefix(pg.Code, "22")
}
