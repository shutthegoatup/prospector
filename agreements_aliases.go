package main

import "github.com/shutthegoatup/prospector/internal/agreements"

// What moved to internal/agreements keeps its name here.

const RecipientCopy = agreements.RecipientCopy
const RecipientSigner = agreements.RecipientSigner
const agreementFileLimit = agreements.AgreementFileLimit
const signingConsent = agreements.SigningConsent

type Certificate = agreements.Certificate
type CertificateEvent = agreements.CertificateEvent
type CertificateRound = agreements.CertificateRound
type CertificateSigner = agreements.CertificateSigner
type Party = agreements.Party
type Stamp = agreements.Stamp
type agreementStore = agreements.AgreementStore

var checkChain = agreements.CheckChain
var checkSeal = agreements.CheckSeal
var contains = agreements.Contains
var documentSigning = agreements.DocumentSigning
var errBrokenChain = agreements.ErrBrokenChain
var errTampered = agreements.ErrTampered
var eventHash = agreements.EventHash
var fileName = agreements.FileName
var fillIn = agreements.FillIn
var readForSealing = agreements.ReadForSealing
