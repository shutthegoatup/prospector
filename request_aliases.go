package main

import "github.com/shutthegoatup/prospector/internal/request"

// What moved to internal/request keeps its name here.

const langCookie = request.LangCookie

type Req = request.Req
type Voice = request.Voice
type listURLKey = request.ListURLKey
type localeSet = request.LocaleSet
type problem = request.Problem
type reqKey = request.ReqKey

var languageFor = request.LanguageFor
var languageOf = request.LanguageOf
var nextLink = request.NextLink
var problemTitles = request.ProblemTitles
var reqFrom = request.ReqFrom
var spoken = request.Spoken
var writeProblem = request.WriteProblem
var writeTyped = request.WriteTyped

// recast is generic, so a wrapper rather than an alias.

type refused = request.Refused

var plural = request.Plural
var put = request.Put
var worker = request.Worker
