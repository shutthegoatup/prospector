package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/shutthegoatup/prospector/api"
)

type siteKey struct{}

// withSite carries the address the caller reaches Prospector at, for links that must
// work from outside it, like the bookmarklet.
func (a *App) withSite(f api.StrictHandlerFunc, _ string) api.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, in any) (any, error) {
		return f(context.WithValue(ctx, siteKey{}, a.baseURL(r)), w, r, in)
	}
}

func (s apiServer) DraftPerson(ctx context.Context, in api.DraftPersonRequestObject) (api.DraftPersonResponseObject, error) {
	p := in.Params
	name, role, company := ParseLinkedIn(p.Name, p.Headline)
	if p.Company != "" {
		company = p.Company
	}
	site, _ := ctx.Value(siteKey{}).(string)
	return api.DraftPerson200JSONResponse{Linkedin: strings.SplitN(p.Url, "?", 2)[0], Name: name, Title: role, Company: company,
		Bookmarklet: bookmarklet(site, reqFrom(ctx), "app/add")}, nil
}
