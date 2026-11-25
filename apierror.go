package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	sqlite "github.com/glebarez/go-sqlite"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
)

// notFound is an error about something that isn't here, or isn't this organisation's.
type notFound string

func (e notFound) Error() string { return string(e) }

func noSuch(format string, args ...any) error { return notFound(fmt.Sprintf(format, args...)) }

// missing is what a failed look-up means: nothing there, unless the look-up itself
// failed, which mustn't read as the thing being gone.
func missing(err error, format string, args ...any) error {
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return noSuch(format, args...)
}

// ours reports a failure that is the server's rather than the caller's: the database,
// the network, or the request running out of time. Everything else the store returns
// says what the caller got wrong.
func ours(err error) bool {
	var pg *pgconn.PgError
	var lite *sqlite.Error
	var network net.Error
	if errors.As(err, &pg) || unstorable(err) {
		return !unstorable(err)
	}
	return errors.As(err, &lite) || errors.As(err, &network) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, sql.ErrConnDone) || errors.Is(err, sql.ErrTxDone) || errors.Is(err, driver.ErrBadConn) ||
		errors.Is(err, gorm.ErrInvalidTransaction) || errors.Is(err, gorm.ErrInvalidDB)
}

// unstorable is the database turning the data down: a row that breaks one of its rules,
// such as a name already taken, or a character Postgres won't store (classes 22 and
// 23). Trying again with the same request can't help.
func unstorable(err error) bool {
	var pg *pgconn.PgError
	return breaksRule(err) || errors.As(err, &pg) && strings.HasPrefix(pg.Code, "22")
}

// breaksRule is the part of unstorable that is a row against one of the database's
// rules, a unique index among them, and not a character it won't store.
func breaksRule(err error) bool {
	var pg *pgconn.PgError
	var lite *sqlite.Error
	if errors.As(err, &pg) {
		return strings.HasPrefix(pg.Code, "23")
	}
	const sqliteConstraint = 19
	return errors.As(err, &lite) && lite.Code()&0xff == sqliteConstraint
}

const (
	sayOurs       = "the server couldn't finish that; try again in a moment"
	sayUnstorable = "that couldn't be saved: it clashes with something already here, or holds a character that can't be stored"
)

// fromDatabase is an error in the database's or its driver's own words.
func fromDatabase(err error) bool {
	var pg *pgconn.PgError
	var lite *sqlite.Error
	// A failure to connect names the database's user and host.
	var dial *pgconn.ConnectError
	if errors.As(err, &pg) || errors.As(err, &lite) || errors.As(err, &dial) {
		return true
	}
	// database/sql keeps some of its errors to itself, such as a pool that has closed.
	return strings.Contains(err.Error(), "sql: ") ||
		errors.Is(err, sql.ErrConnDone) || errors.Is(err, sql.ErrTxDone) || errors.Is(err, driver.ErrBadConn) ||
		errors.Is(err, gorm.ErrInvalidTransaction) || errors.Is(err, gorm.ErrInvalidDB)
}

// telling is what a caller is told about err when a handler would answer with code. A
// database's own words never go out, nor what a handler calls a failure of the
// server's: they carry internals and mean nothing to a reader, so they are logged and
// the caller gets ours instead. Any other answer is the handler's to word, such as a
// 502 passing on what another server said.
func telling(where string, err error, code int) (int, string) {
	switch {
	case unstorable(err):
		if code >= 500 {
			code = http.StatusBadRequest
		}
		log.Printf("%s: %v", where, err)
		return code, sayUnstorable
	case code == http.StatusInternalServerError, code < 500 && fromDatabase(err):
		log.Printf("%s: %v", where, err)
		return http.StatusInternalServerError, sayOurs
	}
	return code, err.Error()
}

// addTool registers a tool whose errors an agent reads through telling, as a page or
// an API caller would: the database's own words are of no use to it either.
func addTool[In, Out any](s *mcp.Server, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(s, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		res, out, err := h(ctx, req, in)
		if err != nil {
			_, text := telling("tool "+t.Name, err, statusOf(err))
			err = errors.New(text)
		}
		return res, out, err
	})
}

func statusOf(err error) int {
	var missing notFound
	var away notSent
	switch {
	case errors.As(err, &away):
		return http.StatusBadGateway
	case errors.As(err, &missing), errors.Is(err, gorm.ErrRecordNotFound):
		return http.StatusNotFound
	case errors.Is(err, errErased):
		return http.StatusConflict
	case ours(err):
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}

// problem is an API error as RFC 9457 lays one out.
type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func writeProblem(w http.ResponseWriter, code int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(problem{Type: "about:blank", Title: http.StatusText(code), Status: code, Detail: detail}); err != nil {
		log.Print(err)
	}
}

// apiFail answers an API caller with what went wrong. A failure of the server's own is
// logged and not described: its text can carry internals.
func apiFail(w http.ResponseWriter, r *http.Request, err error) {
	code, detail := telling(r.Method+" "+r.URL.Path, err, statusOf(err))
	writeProblem(w, code, detail)
}

// challenge refuses a request for want of a usable key, saying why in the header as
// RFC 6750 asks, so a client can tell a key to replace from one that can't do this.
func challenge(w http.ResponseWriter, code int, reason, scope, detail string) {
	header := `Bearer realm="prospector"`
	if reason != "" {
		header += fmt.Sprintf(`, error=%q`, reason)
	}
	if scope != "" {
		header += fmt.Sprintf(`, scope=%q`, scope)
	}
	w.Header().Set("WWW-Authenticate", header)
	writeProblem(w, code, detail)
}
