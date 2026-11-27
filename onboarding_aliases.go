package main

import "github.com/shutthegoatup/prospector/internal/onboarding"

// The customers and onboarding stores live in internal/onboarding; these keep their names here.

const customerActor = onboarding.CustomerActor
const customerLinkGap = onboarding.CustomerLinkGap
const customerSessionLife = onboarding.CustomerSessionLife

type CustomerAccess = onboarding.CustomerAccess
type CustomerInvite = onboarding.CustomerInvite
type OnboardingIn = onboarding.OnboardingIn
type OnboardingRow = onboarding.OnboardingRow
type customerStore = onboarding.CustomerStore
type onboardingStore = onboarding.OnboardingStore

var errNoCustomer = onboarding.ErrNoCustomer

type onboardingAPI = onboarding.OnboardingAPI

var errNoCustomerMail = onboarding.ErrNoCustomerMail
