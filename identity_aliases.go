package main

import "github.com/shutthegoatup/prospector/internal/identity"

// What moved to internal/identity keeps its name here.

type outsider = identity.Outsider
type ssoStore = identity.SsoStore

var claim = identity.Claim
var errElsewhere = identity.ErrElsewhere
var errNoAccount = identity.ErrNoAccount
var errNotMember = identity.ErrNotMember
var errUnvouched = identity.ErrUnvouched
