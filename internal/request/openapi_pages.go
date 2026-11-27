package request

import (
	"context"
	"net/http"
	"net/url"
)

type ListURLKey struct{}

// RequestKey holds the request an operation came in, for the few that need it.
type RequestKey struct{}

func RequestFrom(ctx context.Context) *http.Request { return ctx.Value(RequestKey{}).(*http.Request) }

// LanguageFrom is the language to answer the operation in ctx in.
func LanguageFrom(ctx context.Context) string { return LanguageOf(RequestFrom(ctx)) }

// NextLink is the Link header for the page after this one: empty when this one is the last.
func NextLink(ctx context.Context, next string) string {
	at, _ := ctx.Value(ListURLKey{}).(*url.URL)
	if next == "" || at == nil {
		return ""
	}
	q := at.Query()
	q.Set("cursor", next)
	return "<" + at.Path + "?" + q.Encode() + `>; rel="next"`
}
