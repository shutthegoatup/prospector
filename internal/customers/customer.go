package customers

import (
	"errors"
	"net/mail"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/store"
)

const customerLinkLife = 7 * 24 * time.Hour

const CustomerSessionLife = 30 * 24 * time.Hour

const // CustomerLinkGap is the least time between two links mailed for one grant, so
// neither a member nor the page's own "send another" can fill somebody's inbox.
CustomerLinkGap = 5 * time.Minute

const // customerLinkKept is how long a spent or expired link still earns a fresh one.
customerLinkKept = 90 * 24 * time.Hour

var ErrNoCustomer = errors.New("that link doesn't open anything: it may have been withdrawn")

// CustomerAccess is a grant with everything it stands on. It exists only while all of
// them do: the grant not withdrawn, its onboarding and its contact still there.
type CustomerAccess struct {
	Grant      store.CustomerGrant
	Org        store.Org
	Onboarding store.Onboarding
	Contact    store.Contact
	// Link is set when access was found by a link: it may be spent or expired.
	Link *store.CustomerLink
}

// invitable is whether a contact may have a page for an onboarding: they are a person,
// picked, at the company being onboarded, with one address a link can be mailed to, and
// not on the never-contact list.
func invitable(o *store.Org, ob store.Onboarding, c store.Contact) error {
	switch {
	case strings.HasPrefix(c.Key, "org:"):
		return errors.New("that row stands for a company, not a person")
	case c.CompanyID == nil || *c.CompanyID != ob.CompanyID:
		return errors.New("that person is not at the company being onboarded")
	case c.Status == store.ContactSuggested:
		return errors.New("this contact is still a suggestion in Research; a person picks them first")
	}
	if _, err := mail.ParseAddress(strings.TrimSpace(store.Deref(c.Email))); err != nil {
		return errors.New("they have no single email address to send the link to")
	}
	return o.Settings.Listed(store.Deref(c.Email), store.Deref(c.Linkedin))
}

var errLinkTooSoon = errors.New("a link was sent a moment ago: it can take a minute to arrive")

// DropCustomerGrants ends access for the grants a query matches: the links and the
// browsers go with them. keep leaves the grant rows, withdrawn, as a record.
func DropCustomerGrants(tx *gorm.DB, org int64, matching *gorm.DB, keep bool) error {
	var ids []int64
	if err := matching.Model(&store.CustomerGrant{}).Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	for _, table := range []any{&store.CustomerLink{}, &store.CustomerSession{}} {
		if err := tx.Where("org_id = ? and grant_id in ?", org, ids).Delete(table).Error; err != nil {
			return err
		}
	}
	if keep {
		return tx.Model(&store.CustomerGrant{}).Where("org_id = ? and id in ? and revoked_at is null", org, ids).Update("revoked_at", time.Now()).Error
	}
	return tx.Where("org_id = ? and id in ?", org, ids).Delete(&store.CustomerGrant{}).Error
}

// CustomerInvite is who has been let see an onboarding, for the members' pages.
type CustomerInvite struct {
	store.CustomerGrant
	Name, Email string
}

// CustomerActor is who a step says finished it when the customer marked it themselves.
// A fixed word, not their name: the person is on the history entry, which erasure removes.
const CustomerActor = "customer"
