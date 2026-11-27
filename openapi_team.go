package main

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/say"
)

// withRequest hands an operation the request it came in, for the few that need its
// cookies or address.
func withRequest(f api.StrictHandlerFunc, _ string) api.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, in any) (any, error) {
		return f(context.WithValue(ctx, requestKey{}, r), w, r, in)
	}
}

func requestFrom(ctx context.Context) *http.Request { return ctx.Value(requestKey{}).(*http.Request) }

// someone is the signed-in person asking: an API key is no one, so what is a person's
// own is refused to it.
func someone(ctx context.Context) (*Req, error) {
	q := reqFrom(ctx)
	if q.Key != nil || q.User == nil || q.Member == nil {
		return nil, refused{say.New("an API key is no one's: this is for a signed-in member")}
	}
	return q, nil
}

func meOut(u *User, m *Membership) api.Me {
	return api.Me{UserId: u.ID, Name: u.Name, Email: u.Email, Role: api.MeRole(m.Role), HasPassword: u.HasPassword(),
		CallerName: m.CallerName, Signature: m.Signature}
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
	m := *q.Member
	put(&m.CallerName, in.Body.CallerName)
	put(&m.Signature, in.Body.Signature)
	if err := s.a.s.SaveVoice(q.Org.ID, q.User.ID, m.CallerName, m.Signature); err != nil {
		return nil, err
	}
	m.CallerName, m.Signature = strings.TrimSpace(m.CallerName), strings.TrimSpace(m.Signature)
	return api.ChangeMe200JSONResponse(meOut(q.User, &m)), nil
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

func memberOut(m Member) api.Member {
	areas := []api.MemberAreas{}
	for _, ar := range Areas {
		if m.Works(ar.Name) {
			areas = append(areas, api.MemberAreas(ar.Name))
		}
	}
	return api.Member{UserId: m.UserID, Name: m.Name, Email: m.Email, Role: api.MemberRole(m.Role), Areas: areas}
}

func (s apiServer) ListMembers(ctx context.Context, in api.ListMembersRequestObject) (api.ListMembersResponseObject, error) {
	all, err := s.a.s.Members(reqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	page, next, err := listPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListMembers200JSONResponse{Headers: api.ListMembers200ResponseHeaders{Link: nextLink(ctx, next)}}
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
	i := slices.IndexFunc(all, func(m Member) bool { return m.UserID == user })
	if i < 0 {
		return api.Member{}, noSuch("that person isn't in this organisation")
	}
	return memberOut(all[i]), nil
}

func (s apiServer) AddMember(ctx context.Context, in api.AddMemberRequestObject) (api.AddMemberResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("say who: {\"email\": \"…\"}")
	}
	msg, err := s.a.addMember(ctx, reqFrom(ctx), memberIn{Email: in.Body.Email, Name: in.Body.Name, Role: string(in.Body.Role)})
	if err != nil {
		return nil, err
	}
	return api.AddMember201JSONResponse{Message: msg}, nil
}

func (s apiServer) ChangeMember(ctx context.Context, in api.ChangeMemberRequestObject) (api.ChangeMemberResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send what changes, such as {\"role\": \"owner\"} or {\"areas\": [\"sales\"]}")
	}
	org := reqFrom(ctx).Org.ID
	if _, err := s.member(org, in.User); err != nil {
		return nil, err
	}
	if in.Body.Role != "" {
		if err := s.a.s.SetRole(org, in.User, string(in.Body.Role)); err != nil {
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
	q := reqFrom(ctx)
	if _, err := s.a.s.RemoveMember(q.Org.ID, in.User, q.Actor()); err != nil {
		return nil, err
	}
	return api.RemoveMember204Response{}, nil
}

func (s apiServer) ListNeverContactCovers(ctx context.Context, in api.ListNeverContactCoversRequestObject) (api.ListNeverContactCoversResponseObject, error) {
	o, err := s.a.s.OrgBySlug(reqFrom(ctx).Org.Slug)
	if err != nil {
		return nil, err
	}
	all := s.a.s.NeverContactCovers(o)
	page, next, err := listPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListNeverContactCovers200JSONResponse{Headers: api.ListNeverContactCovers200ResponseHeaders{Link: nextLink(ctx, next)}}
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
	return api.GetMailServer200JSONResponse(s.mailServerOut(reqFrom(ctx).Org.ID)), nil
}

func (s apiServer) MakeBounceHook(ctx context.Context, _ api.MakeBounceHookRequestObject) (api.MakeBounceHookResponseObject, error) {
	base, err := url.Parse(s.a.base)
	if s.a.base == "" || err != nil || base.Scheme != "https" {
		return nil, say.New("Postmark needs this server's public https address (PROSPECTOR_URL) to post to") //nolint:staticcheck // Postmark is a name
	}
	org := reqFrom(ctx).Org
	secret, err := s.a.s.NewBounceHook(org.ID)
	if err != nil {
		return nil, err
	}
	hook := *base
	hook.Path, hook.User = "/hooks/postmark", url.UserPassword(org.Slug, secret)
	return api.MakeBounceHook200JSONResponse{Url: hook.String(), Username: org.Slug, Password: secret}, nil
}

func (s apiServer) DropBounceHook(ctx context.Context, _ api.DropBounceHookRequestObject) (api.DropBounceHookResponseObject, error) {
	if err := s.a.s.DropBounceHook(reqFrom(ctx).Org.ID); err != nil {
		return nil, err
	}
	return api.DropBounceHook204Response{}, nil
}

func (s apiServer) SaveMailServer(ctx context.Context, in api.SaveMailServerRequestObject) (api.SaveMailServerResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send the server: {\"host\": \"smtp.example.com\", \"port\": 587, …}, or an empty host for none")
	}
	org := reqFrom(ctx).Org.ID
	b := in.Body
	if _, err := s.a.saveMail(org, mailIn{Host: b.Host, Port: b.Port, Username: b.Username, Password: b.Password, Sender: b.Sender}); err != nil {
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

func domainOut(d SSODomain) api.SSODomain {
	return api.SSODomain{Domain: d.Domain, Record: d.Record(), Token: d.Token, VerifiedAt: d.VerifiedAt}
}

func (s apiServer) GetSSO(ctx context.Context, _ api.GetSSORequestObject) (api.GetSSOResponseObject, error) {
	out, err := s.ssoOut(reqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	return api.GetSSO200JSONResponse(out), nil
}

func (s apiServer) SaveSSO(ctx context.Context, in api.SaveSSORequestObject) (api.SaveSSOResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send the provider: {\"issuer\": \"https://login.example.com\", \"client_id\": \"…\", \"client_secret\": \"…\"}, or an empty issuer to turn it off")
	}
	q, b := reqFrom(ctx), in.Body
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
	c, err := s.a.s.SSO(reqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	page, next, err := listPage(c.Domains, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListSSODomains200JSONResponse{Headers: api.ListSSODomains200ResponseHeaders{Link: nextLink(ctx, next)}}
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
	d, err := s.a.addSSODomain(reqFrom(ctx).Org.ID, in.Body.Domain)
	if err != nil {
		return nil, err
	}
	return api.AddSSODomain201JSONResponse(domainOut(*d)), nil
}

func (s apiServer) RemoveSSODomain(ctx context.Context, in api.RemoveSSODomainRequestObject) (api.RemoveSSODomainResponseObject, error) {
	if err := s.a.removeSSODomain(reqFrom(ctx).Org.ID, in.Domain); err != nil {
		return nil, err
	}
	return api.RemoveSSODomain204Response{}, nil
}

func (s apiServer) VerifySSODomain(ctx context.Context, in api.VerifySSODomainRequestObject) (api.VerifySSODomainResponseObject, error) {
	d, err := s.a.verifySSODomain(ctx, reqFrom(ctx).Org.ID, in.Domain)
	if err != nil {
		return nil, err
	}
	return api.VerifySSODomain200JSONResponse(domainOut(*d)), nil
}

// exportResponse writes the export as it is, rather than through a map of it.
type exportResponse struct{ out *OrgExport }

func (e exportResponse) VisitExportOrgResponse(w http.ResponseWriter) error {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, e.out)
	return nil
}

func (s apiServer) ExportOrg(ctx context.Context, _ api.ExportOrgRequestObject) (api.ExportOrgResponseObject, error) {
	q := reqFrom(ctx)
	out, err := s.a.s.ExportOrg(q.Org.ID, q.Actor())
	if err != nil {
		return nil, err
	}
	return exportResponse{out}, nil
}
