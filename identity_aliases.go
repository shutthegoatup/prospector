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

const AgreementDraft = identity.AgreementDraft
const apiKeys = identity.ApiKeys
const areaSales = identity.AreaSales
const areaSupport = identity.AreaSupport
const frontOrgSlug = identity.FrontOrgSlug
const resetGap = identity.ResetGap
const sessionLife = identity.SessionLife
const sessions = identity.Sessions

type Member = identity.Member
type OrgMembership = identity.OrgMembership
type identityStore = identity.IdentityStore

var Areas = identity.Areas
var Scopes = identity.Scopes
var browserOrigin = identity.BrowserOrigin
var deviceMAC = identity.DeviceMAC
var errBadLink = identity.ErrBadLink
var errTooSoon = identity.ErrTooSoon
var errWrongCurrent = identity.ErrWrongCurrent
var errWrongLogin = identity.ErrWrongLogin
var lastOwner = identity.LastOwner
var signInBucket = identity.SignInBucket
var slugify = identity.Slugify
var unstorable = identity.Unstorable
var validScope = identity.ValidScope
