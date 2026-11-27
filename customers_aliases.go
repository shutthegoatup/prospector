package main

import "github.com/shutthegoatup/prospector/internal/customers"

// The customers and onboarding stores live in internal/customers; these keep their names here.

const customerActor = customers.CustomerActor
const customerLinkGap = customers.CustomerLinkGap
const customerSessionLife = customers.CustomerSessionLife

type CustomerAccess = customers.CustomerAccess
type CustomerInvite = customers.CustomerInvite
type OnboardingIn = customers.OnboardingIn
type OnboardingRow = customers.OnboardingRow
type customerStore = customers.CustomerStore
type onboardingStore = customers.OnboardingStore

var errNoCustomer = customers.ErrNoCustomer
