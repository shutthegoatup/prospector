package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// PersonExport is everything held about one contact row, for answering a request
// from that person to see it.
type PersonExport struct {
	ExportedAt time.Time     `json:"exported_at"`
	Note       string        `json:"note,omitempty"`
	Company    string        `json:"company,omitempty"`
	Person     Contact       `json:"person"`
	Facts      []Fact        `json:"facts"`
	Enquiries  []Enquiry     `json:"enquiries"`
	History    []ExportEvent `json:"history"`
}

type ExportEvent struct {
	At     time.Time `json:"at"`
	By     string    `json:"by"`
	Action string    `json:"action"`
	Value  string    `json:"value,omitempty"`
}

func (s *Store) ExportPerson(org, id int64, actor string) (*PersonExport, error) {
	out := &PersonExport{ExportedAt: time.Now().UTC(), Facts: []Fact{}, Enquiries: []Enquiry{}, History: []ExportEvent{}}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		c := &out.Person
		if err := tx.Where("org_id = ? and id = ?", org, id).First(c).Error; err != nil {
			return fmt.Errorf("no contact %d", id)
		}
		if strings.HasPrefix(c.Key, "org:") {
			return errors.New("that row stands for a company, not a person")
		}
		if len(alternates(deref(c.Person))) > 1 {
			out.Note = "This row names several people; everything here is held against all of them."
		}
		out.Company = deref(c.Maker)
		if c.CompanyID != nil {
			var co Company
			if err := tx.Where("org_id = ? and id = ?", org, *c.CompanyID).First(&co).Error; err == nil {
				out.Company = co.Name
			}
		}
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Order("id").Find(&out.Facts).Error; err != nil {
			return err
		}
		// As erase does: what they wrote to us counts whether or not it was matched to them.
		wrote := tx.Where("org_id = ? and contact_id = ?", org, id)
		if emails := emailRe.FindAllString(strings.ToLower(deref(c.Email)), -1); len(emails) > 0 {
			wrote = tx.Where("org_id = ? and (contact_id = ? or email in ?)", org, id, emails)
		}
		if err := wrote.Order("id").Find(&out.Enquiries).Error; err != nil {
			return err
		}
		var events []Event
		if err := tx.Where("org_id = ? and contact_id = ?", org, id).Order("at, id").Find(&events).Error; err != nil {
			return err
		}
		for _, e := range events {
			out.History = append(out.History, ExportEvent{At: e.At, By: e.User, Action: e.Action, Value: deref(e.Value)})
		}
		return s.log(tx, org, id, c.CompanyID, actor, "export", nil)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
