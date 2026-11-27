package main

import "github.com/shutthegoatup/prospector/internal/store"

// What moved to internal/store keeps its name here.

const AudienceCustomers = store.AudienceCustomers
const ContactSuggested = store.ContactSuggested
const KindService = store.KindService
const PerMonth = store.PerMonth
const PerYear = store.PerYear
const PhaseConversation = store.PhaseConversation
const RunWaiting = store.RunWaiting
const RunWorking = store.RunWorking
const StepOurs = store.StepOurs
const StepTheirs = store.StepTheirs
const SubActive = store.SubActive
const SubEnded = store.SubEnded
const SubEnding = store.SubEnding
const SubNotice = store.SubNotice
const SubScheduled = store.SubScheduled
const ThenEnds = store.ThenEnds
const ThenRolls = store.ThenRolls
const areaAny = store.AreaAny
const dayLayout = store.DayLayout
const kindAnthropic = store.KindAnthropic
const kindOpenAI = store.KindOpenAI
const proofLife = store.ProofLife

type APIKey = store.APIKey
type Agent = store.Agent
type AgentRun = store.AgentRun
type AgentTool = store.AgentTool
type Agreement = store.Agreement
type AgreementEvent = store.AgreementEvent
type AgreementField = store.AgreementField
type AgreementFile = store.AgreementFile
type AgreementRecipient = store.AgreementRecipient
type Asset = store.Asset
type BounceHook = store.BounceHook
type Campaign = store.Campaign
type CampaignRecipient = store.CampaignRecipient
type Company = store.Company
type Contact = store.Contact
type CustomerGrant = store.CustomerGrant
type CustomerLink = store.CustomerLink
type CustomerSession = store.CustomerSession
type Deal = store.Deal
type Enquiry = store.Enquiry
type EnquiryMessage = store.EnquiryMessage
type EnrichUsage = store.EnrichUsage
type Enricher = store.Enricher
type Erasure = store.Erasure
type Event = store.Event
type Fact = store.Fact
type Identity = store.Identity
type MailDomain = store.MailDomain
type Membership = store.Membership
type Offering = store.Offering
type Onboarding = store.Onboarding
type OnboardingStep = store.OnboardingStep
type Org = store.Org
type OrgMail = store.OrgMail
type OrgSettings = store.OrgSettings
type PasswordToken = store.PasswordToken
type RequestClaim = store.RequestClaim
type Role = store.Role
type SSOConnection = store.SSOConnection
type SSODomain = store.SSODomain
type SealKey = store.SealKey
type Session = store.Session
type Standing = store.Standing
type StepPlan = store.StepPlan
type Subscription = store.Subscription
type Suggestion = store.Suggestion
type Suppression = store.Suppression
type Task = store.Task
type TaskNote = store.TaskNote
type TaskRequest = store.TaskRequest
type UnsubscribeLink = store.UnsubscribeLink
type User = store.User
type Verdict = store.Verdict

var Stages = store.Stages
var addMonths = store.AddMonths
var assetKey = store.AssetKey
var bareDomain = store.BareDomain
var deref = store.Deref
var domainOf = store.DomainOf
var enquiryOrigins = store.EnquiryOrigins
var formatMoney = store.FormatMoney
var linkedinPath = store.LinkedinPath
var mustDay = store.MustDay
var validStage = store.ValidStage

const sealing = store.Sealing

type Actor = store.Actor
type SSO = store.SSO
type core = store.Core
type idpSpec = store.IdpSpec
type keyring = store.Keyring
type purpose = store.Purpose
type secretKey = store.SecretKey

var emailDomain = store.EmailDomain
var errNoKeys = store.ErrNoKeys
var errUnknownKey = store.ErrUnknownKey
var hashToken = store.HashToken
var lookupVersion = store.LookupVersion
var mac = store.Mac
var sealed = store.Sealed
var sealedVersion = store.SealedVersion

type notFound = store.NotFound

var displayName = store.DisplayName
var hold = store.Hold
var holdOrg = store.HoldOrg
var logged = store.Logged
var missing = store.Missing
var noSuch = store.NoSuch
var nonEmpty = store.NonEmpty
var randomToken = store.RandomToken
var str = store.Str
var today = store.Today


type Change = store.Change
type conflict = store.Conflict

var parseMoney = store.ParseMoney

const builtinPrefix = store.BuiltinPrefix

var bareAddress = store.BareAddress
var breaksRule = store.BreaksRule
var builtinKeyName = store.BuiltinKeyName
var companyName = store.CompanyName
var emailRe = store.EmailRe
var errErased = store.ErrErased
var identifiers = store.Identifiers
var openTask = store.OpenTask
var personKey = store.PersonKey
var unusableHash = store.UnusableHash
