package main

import "github.com/shutthegoatup/prospector/internal/privacy"

// What moved to internal/privacy keeps its name here.

const snapshotSchema = privacy.SnapshotSchema

type CompanyRow = privacy.CompanyRow
type ExportEvent = privacy.ExportEvent
type OrgExport = privacy.OrgExport
type Person = privacy.Person
type PersonExport = privacy.PersonExport
type privacyStore = privacy.PrivacyStore

var alternates = privacy.Alternates
var companyStem = privacy.CompanyStem
var competitorHold = privacy.CompetitorHold
var laterColumns = privacy.LaterColumns
var laterValue = privacy.LaterValue
var linkCompanies = privacy.LinkCompanies
var parseInserts = privacy.ParseInserts
var pointAtContacts = privacy.PointAtContacts
var snapshotColumns = privacy.SnapshotColumns
var sqlQuote = privacy.SqlQuote

type privacyAPI = privacy.PrivacyAPI
