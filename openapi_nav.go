package main

import (
	"context"
	"strings"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
)

func orgSummary(o Org) api.OrgSummary {
	return api.OrgSummary{Id: o.ID, Slug: o.Slug, Name: o.Name, Color: o.Color}
}

func (s apiServer) GetNav(ctx context.Context, _ api.GetNavRequestObject) (api.GetNavResponseObject, error) {
	q := request.ReqFrom(ctx)
	n := s.a.nav(q)
	out := api.Nav{Org: orgSummary(*q.Org), Orgs: []api.NavOrg{}, Areas: []api.NavAreas{}, Work: "API key · " + keyName(q)}
	out.Counts.Today, out.Counts.Research, out.Counts.Conversations = n.Today, n.Research, n.Conversations
	out.Counts.Enquiries, out.Counts.Companies = n.Enquiries, n.Companies
	if q.Member == nil {
		return api.GetNav200JSONResponse(out), nil
	}
	user := struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}{Email: q.User.Email, Name: q.User.Name}
	out.User = &user
	for _, o := range q.Orgs {
		out.Orgs = append(out.Orgs, api.NavOrg{Id: o.ID, Slug: o.Slug, Name: o.Name, Color: o.Color, Role: api.NavOrgRole(o.Role)})
	}
	out.Role = api.NavRole(q.Member.Role)
	for _, ar := range strings.Fields(q.Member.Areas) {
		out.Areas = append(out.Areas, api.NavAreas(ar))
	}
	out.Sales, out.Support, out.Owner = q.Member.Works(identity.AreaSales), q.Member.Works(identity.AreaSupport), q.Owner()
	out.Work = memberWork(*q.Member)
	return api.GetNav200JSONResponse(out), nil
}

func keyName(q *request.Req) string {
	if q.Key == nil {
		return ""
	}
	return q.Key.Name
}

func (s apiServer) CreateOrg(ctx context.Context, in api.CreateOrgRequestObject) (api.CreateOrgResponseObject, error) {
	q := request.ReqFrom(ctx)
	if q.Key != nil || q.User == nil {
		return nil, request.Refused{Text: say.New("a key belongs to one organisation and can't start another: sign in to do that")}
	}
	if in.Body == nil {
		return nil, say.New(`send the organisation's name: {"name": "Acme"}`)
	}
	o, err := newOrgFor(s.a.s, q, in.Body.Name)
	if err != nil {
		return nil, err
	}
	return api.CreateOrg201JSONResponse(orgSummary(*o)), nil
}

// newOrgFor starts an organisation owned by whoever is signed in, unless their sign-in
// came through another organisation's single sign-on, which opens that one alone.
func newOrgFor(st *Store, q *request.Req, name string) (*Org, error) {
	if q.Session != nil && q.Session.Limited {
		return nil, request.Refused{Text: say.New("you're signed in through one organisation's single sign-on, which opens only that organisation: sign in with a password to start another")}
	}
	return st.CreateOrgWithOwner("", name, q.User)
}
