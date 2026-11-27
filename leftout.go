package main

import (
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
