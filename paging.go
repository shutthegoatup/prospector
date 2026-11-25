package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

const (
	pageUsual = 50
	pageMost  = 200
)

// PageIn asks for part of a list. Every list an agent can read comes a page at a
// time, so none of them is ever cut without saying so.
type PageIn struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"how many to return: 50 if left out, 200 at most"`
	Cursor string `json:"cursor,omitempty" jsonschema:"the next_cursor of the previous answer, to carry on from where it ended; leave out to start"`
}

func (p PageIn) size() int {
	if p.Limit <= 0 {
		return pageUsual
	}
	return min(p.Limit, pageMost)
}

var errCursor = errors.New("that cursor isn't one this list gave out: leave it out to start from the beginning")

// A cursor is where a list was left, as numbers only its own list can read. It is
// opaque to the caller, so what it holds can change without breaking anyone.
func cursorOf(at ...int64) string {
	raw, _ := json.Marshal(at)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// at reads a cursor of n numbers; an empty one is the start, which is all zeros.
func (p PageIn) at(n int) ([]int64, error) {
	out := make([]int64, n)
	if p.Cursor == "" {
		return out, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(p.Cursor)
	if err != nil || json.Unmarshal(raw, &out) != nil || len(out) != n {
		return nil, errCursor
	}
	for _, v := range out {
		if v < 0 {
			return nil, errCursor
		}
	}
	return out, nil
}

// pageFrom reads limit and cursor from a request's query.
func pageFrom(r *http.Request) (PageIn, error) {
	p := PageIn{Cursor: r.URL.Query().Get("cursor")}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return p, errors.New("limit is a whole number, 1 or more")
		}
		p.Limit = n
	}
	return p, nil
}

// linkNext names the next page in a Link header (RFC 8288), for clients that follow
// those and don't read the body's next_cursor.
func linkNext(w http.ResponseWriter, r *http.Request, next string) {
	if next == "" {
		return
	}
	q := r.URL.Query()
	q.Set("cursor", next)
	w.Header().Set("Link", "<"+r.URL.Path+"?"+q.Encode()+`>; rel="next"`)
}

// afterID is the next page of rows that are in id order, after the row a cursor
// names, and the cursor for the page after that: ranOut when this page is the last.
func afterID[T any](rows []T, id func(T) int64, after int64, size int) (page []T, at int64) {
	from := 0
	for from < len(rows) && id(rows[from]) <= after {
		from++
	}
	rows = rows[from:]
	if len(rows) <= size {
		return rows, ranOut
	}
	return rows[:size], id(rows[size-1])
}

// noneBefore is where a cursor stands once a newest-first list has run out: no row
// has an id below 1. Zero is the start, which is every row.
const noneBefore = 1

// beforeID is afterID for rows that are newest first.
func beforeID[T any](rows []T, id func(T) int64, before int64, size int) (page []T, at int64) {
	from := 0
	for before > 0 && from < len(rows) && id(rows[from]) >= before {
		from++
	}
	rows = rows[from:]
	if len(rows) <= size {
		return rows, noneBefore
	}
	return rows[:size], id(rows[size-1])
}
