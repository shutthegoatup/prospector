package main

import "github.com/shutthegoatup/prospector/internal/research"

// What moved to internal/research keeps its name here.

const RoleJunked = research.RoleJunked
const RolePursued = research.RolePursued
const RoleReview = research.RoleReview
const TaskClosed = research.TaskClosed
const TaskDone = research.TaskDone
const TaskWaiting = research.TaskWaiting
const TaskWorking = research.TaskWorking
const agentNotes = research.AgentNotes
const answerYes = research.AnswerYes
const claimFor = research.ClaimFor
const companyNotesPageSize = research.CompanyNotesPageSize
const heldNeverContact = research.HeldNeverContact
const maxBrief = research.MaxBrief
const maxNote = research.MaxNote
const runGap = research.RunGap
const runsRead = research.RunsRead
const taskNotesPageSize = research.TaskNotesPageSize
const taskNotesShown = research.TaskNotesShown
const taskPageSize = research.TaskPageSize
const withheldErased = research.WithheldErased
const withheldNeverContact = research.WithheldNeverContact

type AddCompanyIn = research.AddCompanyIn
type AddCompanyOut = research.AddCompanyOut
type AddContactOut = research.AddContactOut
type AddFactIn = research.AddFactIn
type AddRoleIn = research.AddRoleIn
type AddRoleOut = research.AddRoleOut
type AgentScore = research.AgentScore
type CompanyDetail = research.CompanyDetail
type DraftIn = research.DraftIn
type DueItem = research.DueItem
type DueOut = research.DueOut
type FactIn = research.FactIn
type FinishIn = research.FinishIn
type MarkIn = research.MarkIn
type NoteIn = research.NoteIn
type RoleIn = research.RoleIn
type Run = research.Run
type SearchHit = research.SearchHit
type TaskBrief = research.TaskBrief
type TaskCounts = research.TaskCounts
type TaskOut = research.TaskOut
type TaskView = research.TaskView
type VerdictRow = research.VerdictRow
type researchStore = research.ResearchStore

var contactStage = research.ContactStage
var firstLine = research.FirstLine
var lookForList = research.LookForList
var normalURL = research.NormalURL
var runsOf = research.RunsOf
