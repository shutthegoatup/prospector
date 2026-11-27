package prospecting

import "github.com/shutthegoatup/prospector/internal/crm"

// What moved to internal/crm keeps its name here.

const CompanyQualified = crm.CompanyQualified
const CompanyRejected = crm.CompanyRejected
const CompanyReview = crm.CompanyReview

type CRMStore = crm.CRMStore
type Page = crm.Page

var PageOf = crm.PageOf
var pipelineAt = crm.PipelineAt
