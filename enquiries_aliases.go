package main

import "github.com/shutthegoatup/prospector/internal/enquiries"

// What moved to internal/enquiries keeps its name here.

const EnquiryClosed = enquiries.EnquiryClosed
const EnquiryOpen = enquiries.EnquiryOpen
const EnquiryWaiting = enquiries.EnquiryWaiting
const enquiryPageSize = enquiries.EnquiryPageSize
const replyLimit = enquiries.ReplyLimit

type EnquiryFilter = enquiries.EnquiryFilter
type EnquiryIn = enquiries.EnquiryIn
type enquiriesAround = enquiries.EnquiriesAround
type enquiryStore = enquiries.EnquiryStore

var enquiryIDs = enquiries.EnquiryIDs
var errEnquiryEmail = enquiries.ErrEnquiryEmail
var inThread = enquiries.InThread
