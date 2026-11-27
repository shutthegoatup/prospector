package main

import "github.com/shutthegoatup/prospector/internal/tickets"

// What moved to internal/tickets keeps its name here.

const EnquiryClosed = tickets.EnquiryClosed
const EnquiryOpen = tickets.EnquiryOpen
const EnquiryWaiting = tickets.EnquiryWaiting
const enquiryPageSize = tickets.EnquiryPageSize
const replyLimit = tickets.ReplyLimit

type EnquiryFilter = tickets.EnquiryFilter
type EnquiryIn = tickets.EnquiryIn
type enquiriesAround = tickets.EnquiriesAround
type enquiryStore = tickets.EnquiryStore

var errEnquiryEmail = tickets.ErrEnquiryEmail
