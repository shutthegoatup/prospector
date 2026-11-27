package request

import (
	"context"
	"net/url"
)

type ListURLKey struct{}

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
