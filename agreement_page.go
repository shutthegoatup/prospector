package main

import (
	"errors"
	"log"
	"net/http"
	"net/mail"
	"strings"
)

// PreparePage is a draft being set out for signing: who signs and gets a copy, the people
// and members who could be added, and what it still lacks.
type PreparePage struct {
	Agreement Agreement
	Company   Company
	Parties   []Party
	People    []Contact
	Members   []Member
	Missing   []string
	Problem   string
}

// signerColours tell one signer's fields from another's, by their place among the signers.
var signerColours = []string{"#ff7452", "#ffc233", "#35c58a", "#8f7bff", "#56c5f4", "#ff9f43"}

func signerColour(i int) string { return signerColours[i%len(signerColours)] }

func (a *App) prepareAgreement(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	ag, err := a.s.Agreement(q.Org.ID, idParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := a.s.CompanyByID(q.Org.ID, ag.CompanyID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p := PreparePage{Agreement: *ag, Company: *c}
	if _, err := a.s.agreementPages(q.Org.ID, &p.Agreement); err != nil {
		p.Problem = err.Error()
	}
	var failed [4]error
	p.Parties, failed[0] = a.s.AgreementRecipients(q.Org.ID, ag.ID)
	var people []Contact
	people, failed[1] = a.s.People(q.Org.ID, c.ID)
	p.Members, failed[2] = a.s.Members(q.Org.ID)
	p.Missing, failed[3] = a.s.Readiness(q.Org.ID, p.Agreement)
	if err := errors.Join(failed[:]...); err != nil {
		log.Printf("agreement %d: %v", ag.ID, err)
		http.Error(w, "the agreement couldn't be read", http.StatusInternalServerError)
		return
	}
	for _, person := range people {
		if signable(q.Org, c.ID, person) == nil {
			p.People = append(p.People, person)
		}
	}
	w.Header().Set("Content-Security-Policy", pdfPolicy)
	a.page(w, r, "companies", PrepareView(q, p))
}

// on is whether a contact or member is on the agreement already, so they aren't offered twice.
func (p PreparePage) on(contact, member int64) bool {
	for _, r := range p.Parties {
		if (r.ContactID != nil && *r.ContactID == contact) || (r.UserID != nil && *r.UserID == member) {
			return true
		}
	}
	return false
}

// signers are the parties who sign, each with the colour their fields are drawn in.
func (p PreparePage) signers() []Party {
	var out []Party
	for _, r := range p.Parties {
		if r.Role == RecipientSigner {
			out = append(out, r)
		}
	}
	return out
}

func (p PreparePage) copies() []Party {
	var out []Party
	for _, r := range p.Parties {
		if r.Role == RecipientCopy {
			out = append(out, r)
		}
	}
	return out
}

// personLine is a contact as the picker shows them: their name, and where it would go.
func personLine(c Contact) string {
	addr, _ := mail.ParseAddress(strings.TrimSpace(deref(c.Email)))
	if addr == nil {
		return displayName(c)
	}
	return displayName(c) + " · " + addr.Address
}
