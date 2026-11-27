package main

import "github.com/shutthegoatup/prospector/internal/prospecting"

// What moved to internal/prospecting keeps its name here.

// listPage is generic, so a wrapper rather than an alias.
func listPage[T any](all []T, limit int, cursor string) ([]T, string, error) {
	return prospecting.ListPage(all, limit, cursor)
}

const CompanyQualified = prospecting.CompanyQualified
const CompanyRejected = prospecting.CompanyRejected
const CompanyReview = prospecting.CompanyReview
const SuggestionAccepted = prospecting.SuggestionAccepted
const SuggestionDismissed = prospecting.SuggestionDismissed
const actionHeld = prospecting.ActionHeld
const actionWithheld = prospecting.ActionWithheld
const activityPageSize = prospecting.ActivityPageSize
const answerNo = prospecting.AnswerNo
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
var ensureCompany = prospecting.EnsureCompany
var erased = prospecting.ErasedIn
var fill = prospecting.Fill
var firstField = prospecting.FirstField
var fitOf = prospecting.FitOf
var foundForOwn = prospecting.FoundForOwn
var heldOffering = prospecting.HeldOffering
var httpURL = prospecting.HttpURL
var judgedByRoles = prospecting.JudgedByRoles
var markDate = prospecting.MarkDate
var offeringStages = prospecting.OfferingStages
var pageIn = prospecting.PageIn
var pageOf = prospecting.PageOf
var pipeline = prospecting.Pipeline
var putUpForReview = prospecting.PutUpForReview
var suggestable = prospecting.Suggestable
var uniqueKey = prospecting.UniqueKey
var vendors = prospecting.Vendors
