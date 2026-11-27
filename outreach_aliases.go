package main

import "github.com/shutthegoatup/prospector/internal/outreach"

// What moved to internal/outreach keeps its name here.

const KindMarketing = outreach.KindMarketing
const SuppressBounced = outreach.SuppressBounced
const SuppressComplained = outreach.SuppressComplained
const SuppressUnsubscribed = outreach.SuppressUnsubscribed

type mailGate = outreach.MailGate
type suppressionStore = outreach.SuppressionStore

var errBounced = outreach.ErrBounced
var errComplained = outreach.ErrComplained
var errSuppressed = outreach.ErrSuppressed
