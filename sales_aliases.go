package main

import "github.com/shutthegoatup/prospector/internal/sales"

// What moved to internal/sales keeps its name here.

const DealNew = sales.DealNew

type dealStore = sales.DealStore

const ThenRenews = sales.ThenRenews

type SubscriptionChange = sales.SubscriptionChange
type SubscriptionIn = sales.SubscriptionIn
type SubscriptionRow = sales.SubscriptionRow
type subscriptionStore = sales.SubscriptionStore

type salesAPI = sales.SalesAPI

var contracted = sales.Contracted
var subscriptionOut = sales.SubscriptionOut
