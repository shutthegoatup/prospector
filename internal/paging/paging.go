// Package paging is how every list an agent can read comes a page at a time, with a
// cursor that only its own list can read.
package paging

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
)

const (
	Usual = 50
	Most  = 200
)

// Page asks for part of a list. Every list an agent can read comes a page at a
// time, so none of them is ever cut without saying so.
type Page struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"how many to return: 50 if left out, 200 at most"`
	Cursor string `json:"cursor,omitempty" jsonschema:"the next_cursor of the previous answer, to carry on from where it ended; leave out to start"`
}

func (p Page) Size() int {
	if p.Limit <= 0 {
		return Usual
	}
	return min(p.Limit, Most)
}

var ErrCursor = errors.New("that cursor isn't one this list gave out: leave it out to start from the beginning")

// Cursor is where a list was left, as numbers only its own list can read. It is
// opaque to the caller, so what it holds can change without breaking anyone.
func Cursor(at ...int64) string {
	raw, _ := json.Marshal(at)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// At reads a cursor of n numbers; an empty one is the start, which is all zeros.
func (p Page) At(n int) ([]int64, error) {
	out := make([]int64, n)
	if p.Cursor == "" {
		return out, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(p.Cursor)
	if err != nil || json.Unmarshal(raw, &out) != nil || len(out) != n {
		return nil, ErrCursor
	}
	for _, v := range out {
		if v < 0 {
			return nil, ErrCursor
		}
	}
	return out, nil
}

// FromRequest reads limit and cursor from a request's query.
func FromRequest(r *http.Request) (Page, error) {
	p := Page{Cursor: r.URL.Query().Get("cursor")}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return p, errors.New("limit is a whole number, 1 or more")
		}
		p.Limit = n
	}
	return p, nil
}

// LinkNext names the next page in a Link header (RFC 8288), for clients that follow
// those and don't read the body's next_cursor.
func LinkNext(w http.ResponseWriter, r *http.Request, next string) {
	if next == "" {
		return
	}
	q := r.URL.Query()
	q.Set("cursor", next)
	w.Header().Set("Link", "<"+r.URL.Path+"?"+q.Encode()+`>; rel="next"`)
}

// RanOut is where a cursor stands once an oldest-first list has run out.
const RanOut = math.MaxInt64

// AfterID is the next page of rows that are in id order, after the row a cursor
// names, and the cursor for the page after that: RanOut when this page is the last.
func AfterID[T any](rows []T, id func(T) int64, after int64, size int) (page []T, at int64) {
	from := 0
	for from < len(rows) && id(rows[from]) <= after {
		from++
	}
	rows = rows[from:]
	if len(rows) <= size {
		return rows, RanOut
	}
	return rows[:size], id(rows[size-1])
}

// NoneBefore is where a cursor stands once a newest-first list has run out: no row
// has an id below 1. Zero is the start, which is every row.
const NoneBefore = 1

// BeforeID is AfterID for rows that are newest first.
func BeforeID[T any](rows []T, id func(T) int64, before int64, size int) (page []T, at int64) {
	from := 0
	for before > 0 && from < len(rows) && id(rows[from]) >= before {
		from++
	}
	rows = rows[from:]
	if len(rows) <= size {
		return rows, NoneBefore
	}
	return rows[:size], id(rows[size-1])
}
