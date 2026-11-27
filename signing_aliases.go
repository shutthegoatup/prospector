package main

import "github.com/shutthegoatup/prospector/internal/signing"

// What moved to internal/signing keeps its name here.

const RecipientCopy = signing.RecipientCopy
const RecipientSigner = signing.RecipientSigner
const agreementFileLimit = signing.AgreementFileLimit
const signingConsent = signing.SigningConsent

type Certificate = signing.Certificate
type CertificateEvent = signing.CertificateEvent
type CertificateRound = signing.CertificateRound
type CertificateSigner = signing.CertificateSigner
type Party = signing.Party
type Stamp = signing.Stamp
type agreementStore = signing.AgreementStore

var checkChain = signing.CheckChain
var checkSeal = signing.CheckSeal
var contains = signing.Contains
var documentSigning = signing.DocumentSigning
var errBrokenChain = signing.ErrBrokenChain
var errTampered = signing.ErrTampered
var eventHash = signing.EventHash
var fileName = signing.FileName
var fillIn = signing.FillIn
var readForSealing = signing.ReadForSealing

type signingAPI = signing.SigningAPI

var agreementOut = signing.AgreementOut
