package main

import (
	"log"
	"time"

	"gorm.io/gorm"
)

const (
	// actionHeld marks a company a task came to that was already held, with how.
	actionHeld = "task:held"
	// actionWithheld counts a person refused for a task, with only why.
	actionWithheld = "task:withheld"

	heldRejected  = "rejected"
	heldCustomer  = "customer"
	heldOpenDeal  = "open_deal"
	heldQualified = "qualified"
	// heldNeverContact is a company on a domain the never-contact list covers. It is read
	// from the list as it is now, so it isn't recorded.
	heldNeverContact = "never_contact"

	withheldNeverContact = "never_contact"
	withheldErased       = "erased"
)

// heldWhenFound records how a known company stood when a task first came to it, so the
// task can say it wasn't a new find. What happens to it afterwards doesn't change that.
func (s *Store) heldWhenFound(tx *gorm.DB, org int64, who Actor, c *Company, stage string) error {
	if who.Task == 0 || stage == CompanyReview {
		return nil
	}
	var before int64
	if err := tx.Model(&Event{}).Where("org_id = ? and task_id = ? and company_id = ? and action not like 'note:%'", org, who.Task, c.ID).
		Count(&before).Error; err != nil || before > 0 {
		return err
	}
	why, err := heldAs(tx, org, c, stage)
	if err != nil {
		return err
	}
	return s.logAs(tx, org, 0, &c.ID, who, actionHeld, &why)
}

// heldWhenReached is heldWhenFound for a company an agent reaches through a fact or a
// person rather than by adding it, judged by the task's offering as add_company is.
func (s *Store) heldWhenReached(tx *gorm.DB, org int64, who Actor, c *Company) error {
	if who.Task == 0 {
		return nil
	}
	stage := c.Stage
	if who.Offering != 0 {
		stages, err := offeringStages(tx, org, who.Offering, []int64{c.ID})
		if err != nil {
			return err
		}
		stage = stages[c.ID]
	}
	return s.heldWhenFound(tx, org, who, c, stage)
}

// heldAs is why a company that isn't up for review isn't new: a verdict against it
// first, then whether it is a customer or in a conversation, else it is qualified.
func heldAs(tx *gorm.DB, org int64, c *Company, stage string) (string, error) {
	if stage == CompanyRejected {
		return heldRejected, nil
	}
	var people []Contact
	if err := tx.Where("org_id = ? and company_id = ?", org, c.ID).Find(&people).Error; err != nil {
		return "", err
	}
	var subs []Subscription
	if err := tx.Where("org_id = ? and company_id = ?", org, c.ID).Find(&subs).Error; err != nil {
		return "", err
	}
	today := time.Now().Format(dayLayout)
	for _, sub := range subs {
		if sub.On(today).Status != SubEnded {
			return heldCustomer, nil
		}
	}
	switch pipeline(*c, people) {
	case "won":
		return heldCustomer, nil
	case "outreach", "replied", "meeting", "proposal":
		return heldOpenDeal, nil
	}
	return heldQualified, nil
}

// withheld counts a person refused for a task. Only the reason is kept: the event
// outlives an erasure and goes out in exports, so nothing in it may say who it was.
// The refusal stands whether or not this is written.
func (s *Store) withheld(org int64, who Actor, why string) {
	if who.Task == 0 {
		return
	}
	if err := s.logAs(s.db, org, 0, nil, who, actionWithheld, &why); err != nil {
		log.Printf("counting a refusal for task %d: %v", who.Task, err)
	}
}
