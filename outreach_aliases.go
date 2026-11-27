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

const CampaignDraft = outreach.CampaignDraft
const CampaignPaused = outreach.CampaignPaused
const CampaignSending = outreach.CampaignSending
const CampaignSent = outreach.CampaignSent
const MailFailed = outreach.MailFailed
const MailQueued = outreach.MailQueued
const MailSending = outreach.MailSending
const MailSent = outreach.MailSent
const MailSkipped = outreach.MailSkipped
const MailUnknown = outreach.MailUnknown
const mailRefused = outreach.MailRefused
const mailRefusedFor = outreach.MailRefusedFor
const mailTaken = outreach.MailTaken
const mailUnknown = outreach.MailAnswerUnknown

type CampaignIn = outreach.CampaignIn
type Recipient = outreach.Recipient
type mailAnswer = outreach.MailAnswer
type outreachStore = outreach.OutreachStore

var askDNS = outreach.AskDNS
var leftOutBecause = outreach.LeftOutBecause
var mailDomainOf = outreach.MailDomainOf
