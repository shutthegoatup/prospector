package onboarding

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/store"
)

// CustomerStore is the customers area's queries: who outside the organisation may see their onboarding, and their links and sessions.
type CustomerStore struct{ *store.Core }

func (s *CustomerStore) customerAccess(grant int64) (*CustomerAccess, error) {
	var a CustomerAccess
	if err := s.DB.Where("id = ? and revoked_at is null", grant).First(&a.Grant).Error; err != nil {
		return nil, ErrNoCustomer
	}
	org := a.Grant.OrgID
	if err := s.DB.First(&a.Org, org).Error; err != nil {
		return nil, ErrNoCustomer
	}
	if err := s.DB.Where("org_id = ? and id = ?", org, a.Grant.OnboardingID).First(&a.Onboarding).Error; err != nil {
		return nil, ErrNoCustomer
	}
	if err := s.DB.Where("org_id = ? and id = ?", org, a.Grant.ContactID).First(&a.Contact).Error; err != nil {
		return nil, ErrNoCustomer
	}
	// What made them invitable has to hold still, each time: someone who has left the
	// company, lost their address or gone on the never-contact list has no page.
	if invitable(&a.Org, a.Onboarding, a.Contact) != nil {
		return nil, ErrNoCustomer
	}
	return &a, nil
}

// CustomerByToken is the one look-up that starts from a secret and not an organisation:
// kind "link" finds by a mailed link, whatever its state, and "session" by a browser's
// cookie that hasn't expired. Either answers the same when nothing matches.
func (s *CustomerStore) CustomerByToken(kind, token string) (*CustomerAccess, error) {
	if token == "" {
		return nil, ErrNoCustomer
	}
	switch kind {
	case "link":
		var l store.CustomerLink
		if err := s.DB.Where("token_hash = ?", store.HashToken(token)).First(&l).Error; err != nil {
			return nil, ErrNoCustomer
		}
		a, err := s.customerAccess(l.GrantID)
		if err != nil {
			return nil, err
		}
		a.Link = &l
		return a, nil
	case "session":
		var sess store.CustomerSession
		if err := s.DB.Where("token_hash = ? and expires_at > ?", store.HashToken(token), time.Now()).First(&sess).Error; err != nil {
			return nil, ErrNoCustomer
		}
		return s.customerAccess(sess.GrantID)
	}
	return nil, ErrNoCustomer
}

// RedeemCustomerLink spends a link and opens a session for the browser that sent it.
// The link is spent inside the transaction, so two requests can't both use it.
func (s *CustomerStore) RedeemCustomerLink(org int64, a *CustomerAccess, token string) (string, error) {
	session := store.RandomToken(32)
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		spent := tx.Model(&store.CustomerLink{}).Where("org_id = ? and grant_id = ? and token_hash = ? and used_at is null and expires_at > ?", org, a.Grant.ID, store.HashToken(token), now).Update("used_at", now)
		if spent.Error != nil {
			return spent.Error
		}
		if spent.RowsAffected != 1 {
			return ErrNoCustomer
		}
		if err := tx.Where("org_id = ? and expires_at < ?", org, now).Delete(&store.CustomerSession{}).Error; err != nil {
			return err
		}
		return tx.Create(&store.CustomerSession{OrgID: org, GrantID: a.Grant.ID, TokenHash: store.HashToken(session), ExpiresAt: now.Add(CustomerSessionLife)}).Error
	})
	return session, err
}

func (s *CustomerStore) EndCustomerSession(token string) error {
	return s.DB.Where("token_hash = ?", store.HashToken(token)).Delete(&store.CustomerSession{}).Error
}

// newCustomerLink makes a link for a grant, unless one was made inside customerLinkGap.
func (s *CustomerStore) newCustomerLink(tx *gorm.DB, g store.CustomerGrant) (string, error) {
	// Held, so a burst of requests for one person is counted one at a time.
	if err := store.Hold(tx, &store.CustomerGrant{}, g.OrgID, g.ID); err != nil {
		return "", err
	}
	var recent int64
	if err := tx.Model(&store.CustomerLink{}).Where("org_id = ? and grant_id = ? and created_at > ?", g.OrgID, g.ID, time.Now().Add(-CustomerLinkGap)).Count(&recent).Error; err != nil {
		return "", err
	}
	if recent > 0 {
		return "", errLinkTooSoon
	}
	if err := tx.Where("org_id = ? and created_at < ?", g.OrgID, time.Now().Add(-customerLinkKept)).Delete(&store.CustomerLink{}).Error; err != nil {
		return "", err
	}
	token := store.RandomToken(32)
	return token, tx.Create(&store.CustomerLink{OrgID: g.OrgID, GrantID: g.ID, TokenHash: store.HashToken(token), ExpiresAt: time.Now().Add(customerLinkLife)}).Error
}

// Mailable is the people at these companies who could be sent a link: known, with an address.
func (s *CustomerStore) Mailable(org int64, companies []int64) ([]store.Contact, error) {
	var people []store.Contact
	return people, s.InOrg(org).Where("company_id in ? and status <> ? and key not like ?", companies, store.ContactSuggested, "org:%").
		Where("coalesce(email, '') <> ''").Order("id").Find(&people).Error
}

// FreshCustomerLink makes another link for someone already let in.
func (s *CustomerStore) FreshCustomerLink(org, grant int64) (token string, err error) {
	return token, s.DB.Transaction(func(tx *gorm.DB) error {
		var g store.CustomerGrant
		if err := tx.Where("org_id = ? and id = ?", org, grant).First(&g).Error; err != nil {
			return store.Missing(err, "no customer %d in this organisation", grant)
		}
		token, err = s.newCustomerLink(tx, g)
		return err
	})
}

// InviteCustomer lets a contact at the onboarding's company see their steps, and
// returns the link to mail them. Inviting someone again makes a fresh link, and brings
// back access that was withdrawn.
func (s *CustomerStore) InviteCustomer(o *store.Org, onboarding, contact int64, actor string) (string, *store.Contact, error) {
	var token string
	var c store.Contact
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := store.HoldOrg(tx, o.ID); err != nil {
			return err
		}
		var ob store.Onboarding
		if err := tx.Where("org_id = ? and id = ?", o.ID, onboarding).First(&ob).Error; err != nil {
			return store.Missing(err, "no onboarding %d", onboarding)
		}
		if err := tx.Where("org_id = ? and id = ?", o.ID, contact).First(&c).Error; err != nil {
			return store.Missing(err, "no contact %d", contact)
		}
		if err := invitable(o, ob, c); err != nil {
			return err
		}
		g := store.CustomerGrant{OrgID: o.ID, OnboardingID: ob.ID, ContactID: c.ID}
		if err := tx.Where(g).Attrs(store.CustomerGrant{InvitedBy: actor}).FirstOrCreate(&g).Error; err != nil {
			return err
		}
		if g.RevokedAt != nil {
			if err := tx.Model(&g).Updates(map[string]any{"revoked_at": nil, "invited_by": actor}).Error; err != nil {
				return err
			}
		}
		var err error
		if token, err = s.newCustomerLink(tx, g); err != nil {
			return err
		}
		return s.Log(tx, o.ID, c.ID, c.CompanyID, actor, store.ActOnboardInvite, nil)
	})
	return token, &c, err
}

// RevokeCustomer withdraws someone's access at once, in every browser.
func (s *CustomerStore) RevokeCustomer(org, grant int64, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var g store.CustomerGrant
		if err := tx.Where("org_id = ? and id = ?", org, grant).First(&g).Error; err != nil {
			return store.Missing(err, "no access %d", grant)
		}
		if err := DropCustomerGrants(tx, org, tx.Where("org_id = ? and id = ?", org, grant), true); err != nil {
			return err
		}
		var c store.Contact
		// A grant can outlive its contact, which leaves nobody's history to note it in.
		if err := tx.Where("org_id = ? and id = ?", org, g.ContactID).First(&c).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		return s.Log(tx, org, c.ID, c.CompanyID, actor, store.ActOnboardRevoke, nil)
	})
}

// CustomerInvites are the people let see each of an organisation's onboardings, by
// onboarding, with those whose access was withdrawn.
func (s *CustomerStore) CustomerInvites(org int64) (map[int64][]CustomerInvite, error) {
	var grants []store.CustomerGrant
	if err := s.InOrg(org).Order("id").Find(&grants).Error; err != nil {
		return nil, err
	}
	out := map[int64][]CustomerInvite{}
	if len(grants) == 0 {
		return out, nil
	}
	var people []store.Contact
	if err := s.InOrg(org).Where("id in (?)", s.InOrg(org).Model(&store.CustomerGrant{}).Select("contact_id")).Find(&people).Error; err != nil {
		return nil, err
	}
	who := make(map[int64]store.Contact, len(people))
	for _, p := range people {
		who[p.ID] = p
	}
	for _, g := range grants {
		if p, ok := who[g.ContactID]; ok {
			out[g.OnboardingID] = append(out[g.OnboardingID], CustomerInvite{g, store.DisplayName(p), store.Deref(p.Email)})
		}
	}
	return out, nil
}

// CustomerSteps are the steps of an onboarding that are the customer's to do. The
// organisation's own steps were written for its own eyes and are never read for this page.
func (s *CustomerStore) CustomerSteps(org int64, a *CustomerAccess) ([]store.OnboardingStep, error) {
	var steps []store.OnboardingStep
	return steps, s.InOrg(org).Where("onboarding_id = ? and who = ?", a.Onboarding.ID, store.StepTheirs).Order("seq").Find(&steps).Error
}

func (s *CustomerStore) DropCustomerLink(token string) {
	_ = store.Logged(s.DB.Where("token_hash = ?", store.HashToken(token)).Delete(&store.CustomerLink{}))
}

// CustomerSetStep marks one of the customer's own steps done, or takes back a mark they
// made. A step a member marked stays as the member left it, and so does a step that
// isn't theirs or belongs to another onboarding.
func (s *CustomerStore) CustomerSetStep(org int64, a *CustomerAccess, step int64, done bool) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		// Held and read again inside, so access withdrawn or a person erased a moment ago marks nothing.
		if err := store.Hold(tx, &store.CustomerGrant{}, org, a.Grant.ID); err != nil {
			return err
		}
		if err := tx.Where("id = ? and org_id = ? and revoked_at is null", a.Grant.ID, org).First(&store.CustomerGrant{}).Error; err != nil {
			return ErrNoCustomer
		}
		var st store.OnboardingStep
		if err := tx.Where("org_id = ? and onboarding_id = ? and id = ? and who = ?", org, a.Onboarding.ID, step, store.StepTheirs).First(&st).Error; err != nil {
			return ErrNoCustomer
		}
		updates, action := map[string]any{"done_on": store.Today(), "done_by": CustomerActor}, store.ActOnboardDone.String()
		switch {
		case done && st.DoneOn != nil, !done && (st.DoneOn == nil || st.DoneBy != CustomerActor):
			return nil
		case !done:
			updates = map[string]any{"done_on": nil, "done_by": "", "done_entry": 0}
		}
		marked := tx.Model(&store.OnboardingStep{}).Where("id = ? and org_id = ? and done_by = ? and done_entry = ?", st.ID, org, st.DoneBy, st.DoneEntry).Updates(updates)
		if marked.Error != nil || marked.RowsAffected == 0 {
			return marked.Error
		}
		if done {
			entry := store.Event{OrgID: org, ContactID: a.Contact.ID, CompanyID: &a.Onboarding.CompanyID, User: CustomerActor, Action: action, Value: store.NonEmpty(st.Name), At: time.Now()}
			if err := tx.Create(&entry).Error; err != nil {
				return err
			}
			return tx.Model(&store.OnboardingStep{}).Where("org_id = ? and id = ?", org, st.ID).Update("done_entry", entry.ID).Error
		}
		// Taking a mark back removes the entry it wrote, not adds one: someone outside
		// pressing the two buttons in turn can't fill the company's history. A mark made
		// before steps kept their entry leaves it, since which one is theirs isn't known.
		if st.DoneEntry == 0 {
			return nil
		}
		return tx.Where("org_id = ? and id = ? and action = ?", org, st.DoneEntry, store.ActOnboardDone.String()).Delete(&store.Event{}).Error
	})
}
