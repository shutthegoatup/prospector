package main

import "github.com/shutthegoatup/prospector/internal/marketing"

// What moved to internal/marketing keeps its name here.

const KindMarketing = marketing.KindMarketing
const SuppressBounced = marketing.SuppressBounced
const SuppressComplained = marketing.SuppressComplained
const SuppressUnsubscribed = marketing.SuppressUnsubscribed

type mailGate = marketing.MailGate
type suppressionStore = marketing.SuppressionStore

var errBounced = marketing.ErrBounced
var errComplained = marketing.ErrComplained
var errSuppressed = marketing.ErrSuppressed

const CampaignDraft = marketing.CampaignDraft
const CampaignPaused = marketing.CampaignPaused
const CampaignSending = marketing.CampaignSending
const CampaignSent = marketing.CampaignSent
const MailFailed = marketing.MailFailed
const MailQueued = marketing.MailQueued
const MailSending = marketing.MailSending
const MailSent = marketing.MailSent
const MailSkipped = marketing.MailSkipped
const MailUnknown = marketing.MailUnknown
const mailRefused = marketing.MailRefused
const mailRefusedFor = marketing.MailRefusedFor
const mailTaken = marketing.MailTaken
const mailUnknown = marketing.MailAnswerUnknown

type CampaignIn = marketing.CampaignIn
type Recipient = marketing.Recipient
type mailAnswer = marketing.MailAnswer
type outreachStore = marketing.OutreachStore

var askDNS = marketing.AskDNS
var leftOutBecause = marketing.LeftOutBecause
var mailDomainOf = marketing.MailDomainOf
