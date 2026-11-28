package app

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/privacy"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// withRequest hands an operation the request it came in, for the few that need its
// cookies or address.
func withRequest(f api.StrictHandlerFunc, _ string) api.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, in any) (any, error) {
		return f(context.WithValue(ctx, requestKey{}, r), w, r, in)
	}
}

var requestFrom = request.RequestFrom

// someone is the signed-in person asking: an API key is no one, so what is a person's
// own is refused to it.
func someone(ctx context.Context) (*request.Req, error) {
	q := request.ReqFrom(ctx)
	if q.Key != nil || q.User == nil || q.Member == nil {
		return nil, request.Refused{Text: say.New("an API key is no one's: this is for a signed-in member")}
	}
	return q, nil
}

func meOut(u *store.User, m *store.Membership) api.Me {
	return api.Me{UserId: u.ID, Name: u.Name, Email: u.Email, Role: api.MeRole(m.Role), HasPassword: u.HasPassword(),
		CallerName: m.CallerName, Signature: m.Signature, Language: u.Language}
}

func (s apiServer) GetMe(ctx context.Context, _ api.GetMeRequestObject) (api.GetMeResponseObject, error) {
	q, err := someone(ctx)
	if err != nil {
		return nil, err
	}
	return api.GetMe200JSONResponse(meOut(q.User, q.Member)), nil
}

func (s apiServer) ChangeMe(ctx context.Context, in api.ChangeMeRequestObject) (api.ChangeMeResponseObject, error) {
	q, err := someone(ctx)
	if err != nil {
		return nil, err
	}
	if in.Body == nil {
		return nil, say.New("send what changes, such as {\"signature\": \"Ada\"}")
	}
	m, u := *q.Member, *q.User
	if lang := in.Body.Language; lang != nil && *lang != u.Language {
		if *lang != "" && !speaks(*lang) {
			return nil, say.Errorf("Prospector isn't translated into %q", *lang)
		}
		if err := s.a.s.SetLanguage(u.ID, *lang); err != nil {
			return nil, err
		}
		u.Language = *lang
	}
	if in.Body.CallerName != nil || in.Body.Signature != nil {
		request.Put(&m.CallerName, in.Body.CallerName)
		request.Put(&m.Signature, in.Body.Signature)
		if err := s.a.s.SaveVoice(q.Org.ID, q.User.ID, m.CallerName, m.Signature); err != nil {
			return nil, err
		}
		m.CallerName, m.Signature = strings.TrimSpace(m.CallerName), strings.TrimSpace(m.Signature)
	}
	return api.ChangeMe200JSONResponse(meOut(&u, &m)), nil
}

// passwordChangedResponse carries the device cookie the new password vouches for.
type passwordChangedResponse struct{ device *http.Cookie }

func (p passwordChangedResponse) VisitChangeMyPasswordResponse(w http.ResponseWriter) error {
	if p.device != nil {
		http.SetCookie(w, p.device)
	}
	return api.ChangeMyPassword200JSONResponse{Message: passwordChanged}.VisitChangeMyPasswordResponse(w)
}

func (s apiServer) ChangeMyPassword(ctx context.Context, in api.ChangeMyPasswordRequestObject) (api.ChangeMyPasswordResponseObject, error) {
	q, err := someone(ctx)
	if err != nil {
		return nil, err
	}
	if in.Body == nil {
		return nil, say.New("send the current password and the new one: {\"current\": \"…\", \"new\": \"…\"}")
	}
	device, wait, err := s.a.passwordChange(requestFrom(ctx), q.User, in.Body.Current, in.Body.New)
	if wait > 0 {
		return nil, throttledFor(wait)
	}
	if err != nil {
		return nil, err
	}
	return passwordChangedResponse{device}, nil
}

func memberOut(m identity.Member) api.Member {
	areas := []api.MemberAreas{}
	for _, ar := range identity.Areas {
		if m.Works(ar.Name) {
			areas = append(areas, api.MemberAreas(ar.Name))
		}
	}
	return api.Member{UserId: m.UserID, Name: m.Name, Email: m.Email, Role: api.MemberRole(m.Role), Areas: areas}
}

func (s apiServer) ListMembers(ctx context.Context, in api.ListMembersRequestObject) (api.ListMembersResponseObject, error) {
	all, err := s.a.s.Members(request.ReqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	page, next, err := paging.ListPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListMembers200JSONResponse{Headers: api.ListMembers200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.Member{}, next
	for _, m := range page {
		out.Body.Items = append(out.Body.Items, memberOut(m))
	}
	return out, nil
}

func (s apiServer) member(org, user int64) (api.Member, error) {
	all, err := s.a.s.Members(org)
	if err != nil {
		return api.Member{}, err
	}
	i := slices.IndexFunc(all, func(m identity.Member) bool { return m.UserID == user })
	if i < 0 {
		return api.Member{}, store.NoSuch("that person isn't in this organisation")
	}
	return memberOut(all[i]), nil
}

func (s apiServer) AddMember(ctx context.Context, in api.AddMemberRequestObject) (api.AddMemberResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("say who: {\"email\": \"…\"}")
	}
	msg, err := s.a.addMember(ctx, request.ReqFrom(ctx), memberIn{Email: in.Body.Email, Name: in.Body.Name, Role: string(in.Body.Role)})
	if err != nil {
		return nil, err
	}
	return api.AddMember201JSONResponse{Message: msg}, nil
}

func (s apiServer) ChangeMember(ctx context.Context, in api.ChangeMemberRequestObject) (api.ChangeMemberResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send what changes, such as {\"role\": \"owner\"} or {\"areas\": [\"sales\"]}")
	}
	org := request.ReqFrom(ctx).Org.ID
	if _, err := s.member(org, in.User); err != nil {
		return nil, err
	}
	if in.Body.Role != "" {
		if err := s.a.s.SetRole(org, in.User, string(in.Body.Role), request.ReqFrom(ctx).Actor()); err != nil {
			return nil, err
		}
	}
	if in.Body.Areas != nil {
		areas := []string{}
		for _, ar := range *in.Body.Areas {
			areas = append(areas, string(ar))
		}
		if err := s.a.s.SetAreas(org, in.User, areas); err != nil {
			return nil, err
		}
	}
	m, err := s.member(org, in.User)
	if err != nil {
		return nil, err
	}
	return api.ChangeMember200JSONResponse(m), nil
}

func (s apiServer) RemoveMember(ctx context.Context, in api.RemoveMemberRequestObject) (api.RemoveMemberResponseObject, error) {
	q := request.ReqFrom(ctx)
	if _, err := s.a.s.RemoveMember(q.Org.ID, in.User, q.Actor()); err != nil {
		return nil, err
	}
	return api.RemoveMember204Response{}, nil
}

func (s apiServer) ListNeverContactCovers(ctx context.Context, in api.ListNeverContactCoversRequestObject) (api.ListNeverContactCoversResponseObject, error) {
	o, err := s.a.s.OrgBySlug(request.ReqFrom(ctx).Org.Slug)
	if err != nil {
		return nil, err
	}
	all := s.a.s.NeverContactCovers(o)
	page, next, err := paging.ListPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListNeverContactCovers200JSONResponse{Headers: api.ListNeverContactCovers200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor, out.Body.Total = nonNil(page), next, len(all)
	return out, nil
}

func mailOut(v MailView) api.MailServer {
	return api.MailServer{Host: v.Host, Port: v.Port, Username: v.Username, Sender: v.Sender, HasPassword: v.HasPassword, SiteMail: v.SiteMail}
}

func (s apiServer) mailServerOut(org int64) api.MailServer {
	out := mailOut(s.a.mailView(org))
	out.HasBounceHook = s.a.s.HasBounceHook(org)
	return out
}

func (s apiServer) GetMailServer(ctx context.Context, _ api.GetMailServerRequestObject) (api.GetMailServerResponseObject, error) {
	return api.GetMailServer200JSONResponse(s.mailServerOut(request.ReqFrom(ctx).Org.ID)), nil
}

func (s apiServer) MakeBounceHook(ctx context.Context, _ api.MakeBounceHookRequestObject) (api.MakeBounceHookResponseObject, error) {
	base, err := url.Parse(s.a.base)
	if s.a.base == "" || err != nil || base.Scheme != "https" {
		return nil, say.New("Postmark needs this server's public https address (PROSPECTOR_URL) to post to") //nolint:staticcheck // Postmark is a name
	}
	org := request.ReqFrom(ctx).Org
	secret, err := s.a.s.NewBounceHook(org.ID, request.ReqFrom(ctx).Actor())
	if err != nil {
		return nil, err
	}
	hook := *base
	hook.Path, hook.User = "/hooks/postmark", url.UserPassword(org.Slug, secret)
	return api.MakeBounceHook200JSONResponse{Url: hook.String(), Username: org.Slug, Password: secret}, nil
}

func (s apiServer) DropBounceHook(ctx context.Context, _ api.DropBounceHookRequestObject) (api.DropBounceHookResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.a.s.DropBounceHook(q.Org.ID, q.Actor()); err != nil {
		return nil, err
	}
	return api.DropBounceHook204Response{}, nil
}

func (s apiServer) SaveMailServer(ctx context.Context, in api.SaveMailServerRequestObject) (api.SaveMailServerResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send the server: {\"host\": \"smtp.example.com\", \"port\": 587, …}, or an empty host for none")
	}
	org := request.ReqFrom(ctx).Org.ID
	b := in.Body
	if _, err := s.a.saveMail(org, mailIn{Host: b.Host, Port: b.Port, Username: b.Username, Password: b.Password, Sender: b.Sender}, request.ReqFrom(ctx).Actor()); err != nil {
		return nil, err
	}
	return api.SaveMailServer200JSONResponse(s.mailServerOut(org)), nil
}

func (s apiServer) TestMailServer(ctx context.Context, _ api.TestMailServerRequestObject) (api.TestMailServerResponseObject, error) {
	q, err := someone(ctx)
	if err != nil {
		return nil, err
	}
	msg, err := s.a.testMail(ctx, q)
	if err != nil {
		return nil, err
	}
	return api.TestMailServer200JSONResponse{Message: msg}, nil
}

func (s apiServer) ssoOut(org int64) (api.SSO, error) {
	c, err := s.a.s.SSO(org)
	if err != nil {
		return api.SSO{}, err
	}
	out := api.SSO{Issuer: c.Issuer, ClientId: c.ClientID, HasClientSecret: c.ClientSecret != "", Required: c.Required, AutoJoin: c.AutoJoin,
		Sealing: s.a.s.Keyring != nil}
	if s.a.base != "" {
		out.RedirectUri = s.a.callbackURL()
	}
	return out, nil
}

func domainOut(d store.SSODomain) api.SSODomain {
	return api.SSODomain{Domain: d.Domain, Record: d.Record(), Token: d.Token, VerifiedAt: d.VerifiedAt}
}

func (s apiServer) GetSSO(ctx context.Context, _ api.GetSSORequestObject) (api.GetSSOResponseObject, error) {
	out, err := s.ssoOut(request.ReqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	return api.GetSSO200JSONResponse(out), nil
}

func (s apiServer) SaveSSO(ctx context.Context, in api.SaveSSORequestObject) (api.SaveSSOResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send the provider: {\"issuer\": \"https://login.example.com\", \"client_id\": \"…\", \"client_secret\": \"…\"}, or an empty issuer to turn it off")
	}
	q, b := request.ReqFrom(ctx), in.Body
	if err := s.a.saveSSO(ctx, q, ssoIn{Issuer: b.Issuer, ClientID: b.ClientId, Secret: b.ClientSecret, Required: b.Required, AutoJoin: b.AutoJoin}); err != nil {
		return nil, err
	}
	out, err := s.ssoOut(q.Org.ID)
	if err != nil {
		return nil, err
	}
	return api.SaveSSO200JSONResponse(out), nil
}

func (s apiServer) ListSSODomains(ctx context.Context, in api.ListSSODomainsRequestObject) (api.ListSSODomainsResponseObject, error) {
	c, err := s.a.s.SSO(request.ReqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	page, next, err := paging.ListPage(c.Domains, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListSSODomains200JSONResponse{Headers: api.ListSSODomains200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.SSODomain{}, next
	for _, d := range page {
		out.Body.Items = append(out.Body.Items, domainOut(d))
	}
	return out, nil
}

func (s apiServer) AddSSODomain(ctx context.Context, in api.AddSSODomainRequestObject) (api.AddSSODomainResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("say which: {\"domain\": \"example.com\"}")
	}
	q := request.ReqFrom(ctx)
	d, err := s.a.addSSODomain(q.Org.ID, in.Body.Domain, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.AddSSODomain201JSONResponse(domainOut(*d)), nil
}

func (s apiServer) RemoveSSODomain(ctx context.Context, in api.RemoveSSODomainRequestObject) (api.RemoveSSODomainResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.a.removeSSODomain(q.Org.ID, in.Domain, q.Actor()); err != nil {
		return nil, err
	}
	return api.RemoveSSODomain204Response{}, nil
}

func (s apiServer) VerifySSODomain(ctx context.Context, in api.VerifySSODomainRequestObject) (api.VerifySSODomainResponseObject, error) {
	q := request.ReqFrom(ctx)
	d, err := s.a.verifySSODomain(ctx, q.Org.ID, in.Domain, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.VerifySSODomain200JSONResponse(domainOut(*d)), nil
}

// exportResponse writes the export as it is, rather than through a map of it.
type exportResponse struct{ out *privacy.OrgExport }

func (e exportResponse) VisitExportOrgResponse(w http.ResponseWriter) error {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, e.out)
	return nil
}

func (s apiServer) ExportOrg(ctx context.Context, _ api.ExportOrgRequestObject) (api.ExportOrgResponseObject, error) {
	q := request.ReqFrom(ctx)
	out, err := s.a.s.ExportOrg(q.Org.ID, q.Actor())
	if err != nil {
		return nil, err
	}
	return exportResponse{out}, nil
}
