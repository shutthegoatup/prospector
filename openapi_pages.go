package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/shutthegoatup/prospector/internal/paging"
)

type listURLKey struct{}

// pageIn checks what a list was asked for as the other lists do. Zero is the limit
// left out, which the generated code can't tell from 0 asked for.
func pageIn(limit int, cursor string) (paging.Page, error) {
	if limit < 0 {
		return paging.Page{}, errors.New("limit is a whole number, 1 or more")
	}
	return paging.Page{Limit: limit, Cursor: cursor}, nil
}

type requestKey struct{}

// listURL hands each operation the address it was asked at, so a page of a list can
// name the next one in its Link header, and the request, for an answer that names
// this server's own address.
func listURL(f func(ctx context.Context, w http.ResponseWriter, r *http.Request, in any) (any, error)) func(ctx context.Context, w http.ResponseWriter, r *http.Request, in any) (any, error) {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, in any) (any, error) {
		ctx = context.WithValue(ctx, requestKey{}, r)
		return f(context.WithValue(ctx, listURLKey{}, r.URL), w, r, in)
	}
}

// nextLink is the Link header for the page after this one: empty when this one is the last.
func nextLink(ctx context.Context, next string) string {
	at, _ := ctx.Value(listURLKey{}).(*url.URL)
	if next == "" || at == nil {
		return ""
	}
	q := at.Query()
	q.Set("cursor", next)
	return "<" + at.Path + "?" + q.Encode() + `>; rel="next"`
}

// listPage is one page of a list already read whole and in its order. Its cursor is
// how far down the list the page ended, so a row added or gone between two pages
// moves the rest by one.
func listPage[T any](all []T, limit int, cursor string) (page []T, next string, err error) {
	in, err := pageIn(limit, cursor)
	if err != nil {
		return nil, "", err
	}
	at, err := in.At(1)
	if err != nil {
		return nil, "", err
	}
	from := min(int(at[0]), len(all))
	to := min(from+in.Size(), len(all))
	if to < len(all) {
		next = paging.Cursor(int64(to))
	}
	return all[from:to], next, nil
}

// noEmptyLink drops the Link header the generated code writes on every page, the last
// included, where there is no next page to name.
func noEmptyLink(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&linkless{ResponseWriter: w}, r)
	})
}

type linkless struct{ http.ResponseWriter }

func (l *linkless) Unwrap() http.ResponseWriter { return l.ResponseWriter }

func (l *linkless) WriteHeader(code int) {
	if l.Header().Get("Link") == "" {
		l.Header().Del("Link")
	}
	l.ResponseWriter.WriteHeader(code)
}

func (l *linkless) Write(b []byte) (int, error) {
	if l.Header().Get("Link") == "" {
		l.Header().Del("Link")
	}
	return l.ResponseWriter.Write(b)
}
