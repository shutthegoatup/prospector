package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/shutthegoatup/prospector/api"
)

// The account operations are in the spec ahead of their handlers, so the front end can
// be written against them. Until then every one answers 501, before any caller is
// worked out; the forms under /login, /forgot, /reset and /password still do the work.

const accountPrefix = "/api/v1/"

func accountPath(path string) bool { return strings.HasPrefix(path, accountPrefix) }

func notYet(w http.ResponseWriter) {
	writeProblem(w, http.StatusNotImplemented, "not built yet: sign in at /login")
}

var errNotYet = refused("not built yet")

func (apiServer) GetSession(context.Context, api.GetSessionRequestObject) (api.GetSessionResponseObject, error) {
	return nil, errNotYet
}

func (apiServer) SignIn(context.Context, api.SignInRequestObject) (api.SignInResponseObject, error) {
	return nil, errNotYet
}

func (apiServer) SignOut(context.Context, api.SignOutRequestObject) (api.SignOutResponseObject, error) {
	return nil, errNotYet
}

func (apiServer) ChangePassword(context.Context, api.ChangePasswordRequestObject) (api.ChangePasswordResponseObject, error) {
	return nil, errNotYet
}

func (apiServer) GetSignInWays(context.Context, api.GetSignInWaysRequestObject) (api.GetSignInWaysResponseObject, error) {
	return nil, errNotYet
}

func (apiServer) FindSingleSignOn(context.Context, api.FindSingleSignOnRequestObject) (api.FindSingleSignOnResponseObject, error) {
	return nil, errNotYet
}

func (apiServer) RequestPasswordReset(context.Context, api.RequestPasswordResetRequestObject) (api.RequestPasswordResetResponseObject, error) {
	return nil, errNotYet
}

func (apiServer) GetPasswordReset(context.Context, api.GetPasswordResetRequestObject) (api.GetPasswordResetResponseObject, error) {
	return nil, errNotYet
}

func (apiServer) ResetPassword(context.Context, api.ResetPasswordRequestObject) (api.ResetPasswordResponseObject, error) {
	return nil, errNotYet
}
