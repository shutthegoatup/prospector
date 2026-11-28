package main

import (
	"context"
	"net/http"

	"github.com/shutthegoatup/prospector/internal/request"
)

type requestKey = request.RequestKey

// listURL hands each operation the address it was asked at, so a page of a list can
// name the next one in its Link header, and the request, for an answer that names
// this server's own address.
func listURL(f func(ctx context.Context, w http.ResponseWriter, r *http.Request, in any) (any, error)) func(ctx context.Context, w http.ResponseWriter, r *http.Request, in any) (any, error) {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, in any) (any, error) {
		ctx = context.WithValue(ctx, requestKey{}, r)
		return f(context.WithValue(ctx, request.ListURLKey{}, r.URL), w, r, in)
	}
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
