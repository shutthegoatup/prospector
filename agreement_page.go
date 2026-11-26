package main

import (
	"errors"
	"fmt"
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

// Preparing is everything a draft's Prepare page shows. People are only those at the
// company who could be sent it; whoever is on it already is left in, for the page to tell.
func (s *Store) Preparing(o *Org, id int64) (*PreparePage, error) {
	ag, err := s.Agreement(o.ID, id)
	if err != nil {
		return nil, err
	}
	c, err := s.CompanyByID(o.ID, ag.CompanyID)
	if err != nil {
		return nil, missing(err, "no company %d", ag.CompanyID)
	}
	p := PreparePage{Agreement: *ag, Company: *c}
	if _, err := s.agreementPages(o.ID, &p.Agreement); err != nil {
		p.Problem = err.Error()
	}
	var failed [4]error
	p.Parties, failed[0] = s.AgreementRecipients(o.ID, ag.ID)
	var people []Contact
	people, failed[1] = s.People(o.ID, c.ID)
	p.Members, failed[2] = s.Members(o.ID)
	p.Missing, failed[3] = s.Readiness(o.ID, p.Agreement)
	if err := errors.Join(failed[:]...); err != nil {
		return nil, fmt.Errorf("agreement %d: %w", ag.ID, err)
	}
	for _, person := range people {
		if signable(o, c.ID, person) == nil {
			p.People = append(p.People, person)
		}
	}
	return &p, nil
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
