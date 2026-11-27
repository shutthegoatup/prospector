package prospecting

import (
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/store"
)

const // ActionHeld marks a company a task came to that was already held, with how.
ActionHeld = "task:held"

const // ActionWithheld counts a person refused for a task, with only why.
ActionWithheld = "task:withheld"

const HeldRejected = "rejected"

const HeldCustomer = "customer"

const HeldOpenDeal = "open_deal"

const HeldQualified = "qualified"

// heldAs is why a company that isn't up for review isn't new: a verdict against it
// first, then whether it is a customer or in a conversation, else it is qualified.
func heldAs(tx *gorm.DB, org int64, c *store.Company, stage string) (string, error) {
	if stage == CompanyRejected {
		return HeldRejected, nil
	}
	var people []store.Contact
	if err := tx.Where("org_id = ? and company_id = ?", org, c.ID).Find(&people).Error; err != nil {
		return "", err
	}
	var subs []store.Subscription
	if err := tx.Where("org_id = ? and company_id = ?", org, c.ID).Find(&subs).Error; err != nil {
		return "", err
	}
	today := time.Now().Format(store.DayLayout)
	for _, sub := range subs {
		if sub.On(today).Status != store.SubEnded {
			return HeldCustomer, nil
		}
	}
	switch Pipeline(*c, people) {
	case "won":
		return HeldCustomer, nil
	case "outreach", "replied", "meeting", "proposal":
		return HeldOpenDeal, nil
	}
	return HeldQualified, nil
}
