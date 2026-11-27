package main

import (
	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/store"
)

// What moved to internal/prospecting keeps its name here.

// listPage is generic, so a wrapper rather than an alias.
func listPage[T any](all []T, limit int, cursor string) ([]T, string, error) {
	return paging.ListPage(all, limit, cursor)
}

const CompanyQualified = prospecting.CompanyQualified
const CompanyRejected = prospecting.CompanyRejected
const CompanyReview = prospecting.CompanyReview
const SuggestionAccepted = prospecting.SuggestionAccepted
const SuggestionDismissed = prospecting.SuggestionDismissed
const actionWithheld = prospecting.ActionWithheld
const activityPageSize = prospecting.ActivityPageSize
const answerUnknown = prospecting.AnswerUnknown
const assetMet = prospecting.AssetMet
const assetNotFound = prospecting.AssetNotFound
const assetNotMet = prospecting.AssetNotMet
const assetUnchecked = prospecting.AssetUnchecked
const companiesPageSize = prospecting.CompaniesPageSize
const enrichAgain = prospecting.EnrichAgain
const heldCustomer = prospecting.HeldCustomer
const heldOpenDeal = prospecting.HeldOpenDeal
const heldQualified = prospecting.HeldQualified
const heldRejected = prospecting.HeldRejected
const historyPageSize = prospecting.HistoryPageSize
const lookupFound = prospecting.LookupFound
const maxDetail = prospecting.MaxDetail
const maxWaiting = prospecting.MaxWaiting
const researchPageSize = prospecting.ResearchPageSize

type AddContactIn = prospecting.AddContactIn
type AssetFound = prospecting.AssetFound
type CompanyListRow = prospecting.CompanyListRow
type EnricherIn = prospecting.EnricherIn
type HistoryItem = prospecting.HistoryItem
type Page = prospecting.Page
type PendingDetail = prospecting.PendingDetail
type QuickAdd = prospecting.QuickAdd
type ResearchRow = prospecting.ResearchRow
type Stats = prospecting.Stats
type enrichPerson = prospecting.EnrichPerson
type prospectingStore = prospecting.ProspectingStore
type vendorFind = prospecting.VendorFind

var Marks = prospecting.Marks
var assetsFound = prospecting.AssetsFound
var detailOf = prospecting.DetailOf
var erased = prospecting.ErasedIn
var fill = prospecting.Fill
var firstField = store.FirstField
var fitOf = prospecting.FitOf
var httpURL = prospecting.HttpURL
var markDate = prospecting.MarkDate
var pageIn = paging.PageIn
var pipeline = prospecting.Pipeline
var suggestable = prospecting.Suggestable
var vendors = prospecting.Vendors
