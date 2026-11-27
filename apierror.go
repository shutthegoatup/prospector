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
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/say"
)

// refused is an error about something the caller may not do, whatever it sends.
type refused struct{ *say.Text }

// ours reports a failure that is the server's rather than the caller's: the database,
// the network, or the request running out of time. Everything else the store returns
// says what the caller got wrong.
func ours(err error) bool {
	var pg *pgconn.PgError
	var network net.Error
	if errors.As(err, &pg) || unstorable(err) {
		return !unstorable(err)
	}
	return errors.As(err, &network) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, sql.ErrConnDone) || errors.Is(err, sql.ErrTxDone) || errors.Is(err, driver.ErrBadConn) ||
		errors.Is(err, gorm.ErrInvalidTransaction) || errors.Is(err, gorm.ErrInvalidDB)
}

const (
	sayOurs       = "the server couldn't finish that; try again in a moment"
	sayTaken      = "that couldn't be saved: it clashes with something already here"
	sayUnstorable = "that couldn't be saved: it holds a character that can't be stored"
)

// fromDatabase is an error in the database's or its driver's own words.
func fromDatabase(err error) bool {
	var pg *pgconn.PgError
	// A failure to connect names the database's user and host.
	var dial *pgconn.ConnectError
	if errors.As(err, &pg) || errors.As(err, &dial) {
		return true
	}
	// database/sql keeps some of its errors to itself, such as a pool that has closed.
	return strings.Contains(err.Error(), "sql: ") ||
		errors.Is(err, sql.ErrConnDone) || errors.Is(err, sql.ErrTxDone) || errors.Is(err, driver.ErrBadConn) ||
		errors.Is(err, gorm.ErrInvalidTransaction) || errors.Is(err, gorm.ErrInvalidDB)
}

// cutOff is a connection that failed underneath whatever was asked. Postgres passes
// one lost part-way through a statement or a commit on as the network's own error,
// with the database's address in it, so any such error is taken for the server's
// unless a handler has said it is another server's.
func cutOff(err error) bool {
	var network net.Error
	var fromPgconn interface{ SafeToRetry() bool }
	// Not an unexpected EOF: that is also a request body cut short, which is the caller's.
	return errors.As(err, &network) || errors.As(err, &fromPgconn) || pgconn.Timeout(err)
}

// elsewhere is a failure of another server's, such as a sign-in provider that couldn't
// be reached, worded by the handler for whoever asked: theirs to know, and not ours.
type elsewhere struct{ err error }

func (e elsewhere) Error() string { return e.err.Error() }
func (e elsewhere) Unwrap() error { return e.err }

// telling is what a caller is told about err when a handler would answer with code. A
// database's own words never go out, nor a connection's, nor what a handler calls a
// failure of the server's: they carry internals and mean nothing to a reader, so they
// are logged and the caller gets ours instead. Any other answer is the handler's to
// word, such as a 502 passing on what another server said.
func telling(where string, err error, code int) (int, say.Sayer) {
	var clash conflict
	var theirs elsewhere
	if errors.As(err, &theirs) && !fromDatabase(err) {
		return code, wordsOf(err)
	}
	switch {
	case breaksRule(err):
		log.Printf("%s: %v", where, err)
		return http.StatusConflict, say.New(sayTaken)
	case unstorable(err):
		log.Printf("%s: %v", where, err)
		return http.StatusBadRequest, say.New(sayUnstorable)
	case errors.As(err, &clash):
		return http.StatusConflict, wordsOf(err)
	case code == http.StatusInternalServerError, code < 500 && (fromDatabase(err) || cutOff(err)):
		log.Printf("%s: %v", where, err)
		return http.StatusInternalServerError, say.New(sayOurs)
	}
	return code, wordsOf(err)
}

// wordsOf is err's own for a person, when it is the words of one say.Text, and otherwise
// its text as it stands: another error wrapped round one adds words a catalogue can't know.
func wordsOf(err error) say.Sayer {
	var s say.Sayer
	if errors.As(err, &s) && s.Error() == err.Error() {
		return s
	}
	return say.New(err.Error())
}

// addTool registers a tool whose errors an agent reads through telling, as a page or
// an API caller would: the database's own words are of no use to it either.
func addTool[In, Out any](s *mcp.Server, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(s, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		res, out, err := h(ctx, req, in)
		if err != nil {
			// An agent reads English, as it reads the rest of the API's descriptions.
			_, text := telling("tool "+t.Name, err, statusOf(err))
			err = errors.New(text.Error())
		}
		return res, out, err
	})
}

func statusOf(err error) int {
	var missing notFound
	var away notSent
	var clash conflict
	var no refused
	var theirs elsewhere
	switch {
	case errors.As(err, &no):
		return http.StatusForbidden
	// Another server that couldn't be reached is the caller's to sort out, such as an
	// issuer they typed: not ours, even when the network is what said no.
	case errors.As(err, &theirs) && !fromDatabase(err):
		return http.StatusBadRequest
	case errors.As(err, &away):
		return http.StatusBadGateway
	case errors.As(err, &missing), errors.Is(err, gorm.ErrRecordNotFound):
		return http.StatusNotFound
	case errors.Is(err, errTampered):
		return http.StatusInternalServerError
	case errors.Is(err, errErased), errors.As(err, &clash), breaksRule(err):
		return http.StatusConflict
	case ours(err):
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}

// problem is an API error as RFC 9457 lays one out.
type problem struct {
	Type   api.ProblemType `json:"type"`
	Title  string          `json:"title"`
	Status int             `json:"status"`
	Detail string          `json:"detail,omitempty"`
}

// problemTitles are the titles of the problems a client acts on, one for each type as
// RFC 9457 asks.
var problemTitles = map[api.ProblemType]*say.Text{
	api.ProblemTypeSignedOut:          say.New("Signed out"),
	api.ProblemTypeSessionLimited:     say.New("Signed in for another organisation"),
	api.ProblemTypeWrongLogin:         say.New("Wrong email or password"),
	api.ProblemTypeThrottled:          say.New("Slow down"),
	api.ProblemTypeLinkUsed:           say.New("Link no longer works"),
	api.ProblemTypeMustChangePassword: say.New("Choose your own password"),
	api.ProblemTypeSsoRequired:        say.New("Single sign-on required"),
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, detail say.Sayer) {
	writeTyped(w, r, status, api.ProblemTypeUntyped, detail)
}

// writeTyped answers with a Problem in r's language, which RFC 9457 lets its title and
// detail be.
func writeTyped(w http.ResponseWriter, r *http.Request, status int, kind api.ProblemType, detail say.Sayer) {
	lang := speaking(w, r)
	title := http.StatusText(status)
	if t, ok := problemTitles[kind]; ok {
		title = t.In(lang)
	} else {
		kind = api.ProblemTypeUntyped
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(problem{Type: kind, Title: title, Status: status, Detail: detail.In(lang)}); err != nil {
		log.Print(err)
	}
}

// typed is a refusal a client acts on by its type rather than by reading it.
type typed struct {
	status int
	kind   api.ProblemType
	*say.Text
	// retry is the seconds to wait, for a throttled one.
	retry int
}

func throttledFor(seconds int) typed {
	return typed{http.StatusTooManyRequests, api.ProblemTypeThrottled, say.Errorf("too many attempts: try again in %d seconds", seconds), seconds}
}

// apiFail answers an API caller with what went wrong. A failure of the server's own is
// logged and not described: its text can carry internals.
func apiFail(w http.ResponseWriter, r *http.Request, err error) {
	var t typed
	if errors.As(err, &t) {
		if t.retry > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(t.retry))
		}
		writeTyped(w, r, t.status, t.kind, t.Text)
		return
	}
	code, detail := telling(r.Method+" "+r.URL.Path, err, statusOf(err))
	writeProblem(w, r, code, detail)
}

// challenge refuses a request for want of a usable key, saying why in the header as
// RFC 6750 asks, so a client can tell a key to replace from one that can't do this.
func challenge(w http.ResponseWriter, r *http.Request, code int, reason, scope string, detail say.Sayer) {
	header := `Bearer realm="prospector"`
	if reason != "" {
		header += fmt.Sprintf(`, error=%q`, reason)
	}
	if scope != "" {
		header += fmt.Sprintf(`, scope=%q`, scope)
	}
	w.Header().Set("WWW-Authenticate", header)
	writeProblem(w, r, code, detail)
}
