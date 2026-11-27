package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/say"
)

// The account operations sign people in and out, so they are asked by whoever is there:
// signed in, signed out, or still to choose their own password. Only a key is refused.
func (a *App) accountCaller(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hasKey(r) {
			writeProblem(w, r, http.StatusForbidden, say.New("a key can't sign in, sign out or change a password: a person does that in a browser"))
			return
		}
		// Anyone may send these, so none is let run to more than an address and a password need.
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		sess, u := a.session(r)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), reqKey{}, &Req{User: u, Session: sess})))
	})
}

var errSignedOut = typed{http.StatusUnauthorized, api.ProblemTypeSignedOut, say.New("signed out"), 0}

func signedInFrom(ctx context.Context) (*Req, error) {
	q := reqFrom(ctx)
	if q.User == nil {
		return nil, errSignedOut
	}
	return q, nil
}

// sessionOut is a session as the API shows it: the organisations it opens, each saying
// whether its own sign-on still stands in the way.
func (a *App) sessionOut(sess *Session, u *User) (api.Session, error) {
	out := api.Session{User: api.SessionUser{Id: u.ID, Name: u.Name, Email: u.Email, HasPassword: u.HasPassword()},
		MustChangePassword: u.MustChangePassword, Limited: sess.Limited, Orgs: []api.SessionOrg{}}
	orgs, err := a.s.OrgsFor(u.ID)
	if err != nil {
		return out, err
	}
	for _, o := range orgs {
		if !sess.Opens(o.ID) {
			continue
		}
		out.Orgs = append(out.Orgs, api.SessionOrg{Slug: o.Slug, Name: o.Name, Role: api.SessionOrgRole(o.Role),
			SsoRequired: a.ssoRequired(&o.Org, u, &Membership{Role: o.Role}, sess)})
	}
	return out, nil
}

func (s apiServer) GetSession(ctx context.Context, _ api.GetSessionRequestObject) (api.GetSessionResponseObject, error) {
	q, err := signedInFrom(ctx)
	if err != nil {
		return nil, err
	}
	out, err := s.a.sessionOut(q.Session, q.User)
	return api.GetSession200JSONResponse(out), err
}

type signInDone struct {
	api.SignIn200JSONResponse
	cookies []*http.Cookie
}

func (s signInDone) VisitSignInResponse(w http.ResponseWriter) error {
	setCookies(w, s.cookies)
	return s.SignIn200JSONResponse.VisitSignInResponse(w)
}

func setCookies(w http.ResponseWriter, cookies []*http.Cookie) {
	for _, c := range cookies {
		if c != nil {
			http.SetCookie(w, c)
		}
	}
}

func (s apiServer) SignIn(ctx context.Context, in api.SignInRequestObject) (api.SignInResponseObject, error) {
	if in.Body == nil {
		return nil, say.New(`send {"email": "…", "password": "…"}`)
	}
	r := requestFrom(ctx)
	token, u, device, wait, err := s.a.passwordSignIn(r, in.Body.Email, in.Body.Password)
	switch {
	case wait > 0:
		return nil, throttledFor(wait)
	case errors.Is(err, errWrongLogin):
		return nil, typed{http.StatusUnauthorized, api.ProblemTypeWrongLogin, errWrongLogin, 0}
	case err != nil:
		return nil, err
	}
	sess, _, err := s.a.s.SessionFor(token)
	if err != nil {
		return nil, err
	}
	out, err := s.a.sessionOut(sess, u)
	if err != nil {
		return nil, err
	}
	return signInDone{api.SignIn200JSONResponse(out), []*http.Cookie{s.a.sessionCookie(r, token), device}}, nil
}

type signedOut struct{ api.SignOut204Response }

func (signedOut) VisitSignOutResponse(w http.ResponseWriter) error {
	http.SetCookie(w, signedOutCookie())
	return api.SignOut204Response{}.VisitSignOutResponse(w)
}

func (s apiServer) SignOut(ctx context.Context, _ api.SignOutRequestObject) (api.SignOutResponseObject, error) {
	s.a.endSession(requestFrom(ctx))
	return signedOut{}, nil
}

type newPassword struct {
	api.ChangePassword204Response
	device *http.Cookie
}

func (p newPassword) VisitChangePasswordResponse(w http.ResponseWriter) error {
	setCookies(w, []*http.Cookie{p.device})
	return p.ChangePassword204Response.VisitChangePasswordResponse(w)
}

func (s apiServer) ChangePassword(ctx context.Context, in api.ChangePasswordRequestObject) (api.ChangePasswordResponseObject, error) {
	q, err := signedInFrom(ctx)
	if err != nil {
		return nil, err
	}
	if in.Body == nil {
		return nil, say.New(`send {"current": "…", "new": "…"}`)
	}
	device, wait, err := s.a.passwordChange(requestFrom(ctx), q.User, in.Body.Current, in.Body.New)
	if wait > 0 {
		return nil, throttledFor(wait)
	}
	if err != nil {
		return nil, err
	}
	return newPassword{device: device}, nil
}

func (s apiServer) GetSignInWays(context.Context, api.GetSignInWaysRequestObject) (api.GetSignInWaysResponseObject, error) {
	out := api.GetSignInWays200JSONResponse{Password: true, Forgot: s.a.mailOn(), Sso: s.a.base != "", Social: []api.SocialWay{}}
	for _, way := range s.a.social {
		out.Social = append(out.Social, api.SocialWay{Key: way.Key, Label: way.Label, StartUrl: "/login/with/" + way.Key})
	}
	return out, nil
}

type ssoFound struct {
	api.FindSingleSignOn200JSONResponse
	asked *http.Cookie
}

func (f ssoFound) VisitFindSingleSignOnResponse(w http.ResponseWriter) error {
	http.SetCookie(w, f.asked)
	return f.FindSingleSignOn200JSONResponse.VisitFindSingleSignOnResponse(w)
}

func (s apiServer) FindSingleSignOn(ctx context.Context, in api.FindSingleSignOnRequestObject) (api.FindSingleSignOnResponseObject, error) {
	if s.a.base == "" {
		return nil, noSuch("this server has no address of its own (PROSPECTOR_URL) for a provider to send people back to, so it has no single sign-on")
	}
	if in.Body == nil {
		return nil, say.New(`send {"email": "…"}`)
	}
	email := strings.ToLower(strings.TrimSpace(in.Body.Email))
	orgs, err := s.a.s.SSOOrgsFor(emailDomain(email))
	if err != nil {
		return nil, err
	}
	out := ssoFound{api.FindSingleSignOn200JSONResponse{Orgs: []api.SingleSignOnOrg{}}, s.a.askedCookie(requestFrom(ctx))}
	for _, o := range orgs {
		out.Orgs = append(out.Orgs, api.SingleSignOnOrg{Slug: o.Slug, Name: o.Name, StartUrl: ssoStartURL(&o, email, safeNext(in.Body.Next))})
	}
	return out, nil
}

func (s apiServer) RequestPasswordReset(ctx context.Context, in api.RequestPasswordResetRequestObject) (api.RequestPasswordResetResponseObject, error) {
	if !s.a.mailOn() {
		return nil, noSuch("this server sends no mail, so it can't email a link: ask whoever runs it to reset your password")
	}
	if in.Body == nil {
		return nil, say.New(`send {"email": "…"}`)
	}
	email, lang := in.Body.Email, languageOf(requestFrom(ctx))
	s.a.background(func() { s.a.sendReset(email, lang) })
	return api.RequestPasswordReset202Response{}, nil
}

var errLinkUsed = typed{http.StatusGone, api.ProblemTypeLinkUsed, errBadLink, 0}

func (s apiServer) GetPasswordReset(_ context.Context, in api.GetPasswordResetRequestObject) (api.GetPasswordResetResponseObject, error) {
	u, err := s.a.s.PasswordTokenUser(in.Token)
	if err != nil {
		return nil, errLinkUsed
	}
	return api.GetPasswordReset200JSONResponse{Email: u.Email}, nil
}

func (s apiServer) ResetPassword(ctx context.Context, in api.ResetPasswordRequestObject) (api.ResetPasswordResponseObject, error) {
	if in.Body == nil {
		return nil, say.New(`send {"password": "…"}`)
	}
	select {
	case hashSlots <- struct{}{}:
		defer func() { <-hashSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	err := s.a.s.RedeemPasswordToken(in.Token, in.Body.Password)
	if errors.Is(err, errBadLink) {
		return nil, errLinkUsed
	}
	if err != nil {
		return nil, err
	}
	return api.ResetPassword204Response{}, nil
}
