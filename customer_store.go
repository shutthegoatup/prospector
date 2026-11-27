package main

import (
	"time"

	"gorm.io/gorm"
)

func (s *Store) customerAccess(grant int64) (*CustomerAccess, error) {
	var a CustomerAccess
	if err := s.db.Where("id = ? and revoked_at is null", grant).First(&a.Grant).Error; err != nil {
		return nil, errNoCustomer
	}
	org := a.Grant.OrgID
	if err := s.db.First(&a.Org, org).Error; err != nil {
		return nil, errNoCustomer
	}
	if err := s.db.Where("org_id = ? and id = ?", org, a.Grant.OnboardingID).First(&a.Onboarding).Error; err != nil {
		return nil, errNoCustomer
	}
	if err := s.db.Where("org_id = ? and id = ?", org, a.Grant.ContactID).First(&a.Contact).Error; err != nil {
		return nil, errNoCustomer
	}
	// What made them invitable has to hold still, each time: someone who has left the
	// company, lost their address or gone on the never-contact list has no page.
	if invitable(&a.Org, a.Onboarding, a.Contact) != nil {
		return nil, errNoCustomer
	}
	return &a, nil
}

// CustomerByToken is the one look-up that starts from a secret and not an organisation:
// kind "link" finds by a mailed link, whatever its state, and "session" by a browser's
// cookie that hasn't expired. Either answers the same when nothing matches.
func (s *Store) CustomerByToken(kind, token string) (*CustomerAccess, error) {
	if token == "" {
		return nil, errNoCustomer
	}
	switch kind {
	case "link":
		var l CustomerLink
		if err := s.db.Where("token_hash = ?", hashToken(token)).First(&l).Error; err != nil {
			return nil, errNoCustomer
		}
		a, err := s.customerAccess(l.GrantID)
		if err != nil {
			return nil, err
		}
		a.Link = &l
		return a, nil
	case "session":
		var sess CustomerSession
		if err := s.db.Where("token_hash = ? and expires_at > ?", hashToken(token), time.Now()).First(&sess).Error; err != nil {
			return nil, errNoCustomer
		}
		return s.customerAccess(sess.GrantID)
	}
	return nil, errNoCustomer
}

// RedeemCustomerLink spends a link and opens a session for the browser that sent it.
// The link is spent inside the transaction, so two requests can't both use it.
func (s *Store) RedeemCustomerLink(org int64, a *CustomerAccess, token string) (string, error) {
	session := randomToken(32)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		spent := tx.Model(&CustomerLink{}).Where("org_id = ? and grant_id = ? and token_hash = ? and used_at is null and expires_at > ?", org, a.Grant.ID, hashToken(token), now).Update("used_at", now)
		if spent.Error != nil {
			return spent.Error
		}
		if spent.RowsAffected != 1 {
			return errNoCustomer
		}
		if err := tx.Where("org_id = ? and expires_at < ?", org, now).Delete(&CustomerSession{}).Error; err != nil {
			return err
		}
		return tx.Create(&CustomerSession{OrgID: org, GrantID: a.Grant.ID, TokenHash: hashToken(session), ExpiresAt: now.Add(customerSessionLife)}).Error
	})
	return session, err
}

func (s *Store) EndCustomerSession(token string) error {
	return s.db.Where("token_hash = ?", hashToken(token)).Delete(&CustomerSession{}).Error
}

// newCustomerLink makes a link for a grant, unless one was made inside customerLinkGap.
func (s *Store) newCustomerLink(tx *gorm.DB, g CustomerGrant) (string, error) {
	// Held, so a burst of requests for one person is counted one at a time.
	if err := hold(tx, &CustomerGrant{}, g.OrgID, g.ID); err != nil {
		return "", err
	}
	var recent int64
	if err := tx.Model(&CustomerLink{}).Where("org_id = ? and grant_id = ? and created_at > ?", g.OrgID, g.ID, time.Now().Add(-customerLinkGap)).Count(&recent).Error; err != nil {
		return "", err
	}
	if recent > 0 {
		return "", errLinkTooSoon
	}
	if err := tx.Where("org_id = ? and created_at < ?", g.OrgID, time.Now().Add(-customerLinkKept)).Delete(&CustomerLink{}).Error; err != nil {
		return "", err
	}
	token := randomToken(32)
	return token, tx.Create(&CustomerLink{OrgID: g.OrgID, GrantID: g.ID, TokenHash: hashToken(token), ExpiresAt: time.Now().Add(customerLinkLife)}).Error
}

// Mailable is the people at these companies who could be sent a link: known, with an address.
func (s *Store) Mailable(org int64, companies []int64) ([]Contact, error) {
	var people []Contact
	return people, s.in(org).Where("company_id in ? and status <> ? and key not like ?", companies, ContactSuggested, "org:%").
		Where("coalesce(email, '') <> ''").Order("id").Find(&people).Error
}

// FreshCustomerLink makes another link for someone already let in.
func (s *Store) FreshCustomerLink(org, grant int64) (token string, err error) {
	return token, s.db.Transaction(func(tx *gorm.DB) error {
		var g CustomerGrant
		if err := tx.Where("org_id = ? and id = ?", org, grant).First(&g).Error; err != nil {
			return missing(err, "no customer %d in this organisation", grant)
		}
		token, err = s.newCustomerLink(tx, g)
		return err
	})
}

// InviteCustomer lets a contact at the onboarding's company see their steps, and
// returns the link to mail them. Inviting someone again makes a fresh link, and brings
// back access that was withdrawn.
func (s *Store) InviteCustomer(o *Org, onboarding, contact int64, actor string) (string, *Contact, error) {
	var token string
	var c Contact
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := holdOrg(tx, o.ID); err != nil {
			return err
		}
		var ob Onboarding
		if err := tx.Where("org_id = ? and id = ?", o.ID, onboarding).First(&ob).Error; err != nil {
			return missing(err, "no onboarding %d", onboarding)
		}
		if err := tx.Where("org_id = ? and id = ?", o.ID, contact).First(&c).Error; err != nil {
			return missing(err, "no contact %d", contact)
		}
		if err := invitable(o, ob, c); err != nil {
			return err
		}
		g := CustomerGrant{OrgID: o.ID, OnboardingID: ob.ID, ContactID: c.ID}
		if err := tx.Where(g).Attrs(CustomerGrant{InvitedBy: actor}).FirstOrCreate(&g).Error; err != nil {
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
		return s.log(tx, o.ID, c.ID, c.CompanyID, actor, "onboard:invite", nil)
	})
	return token, &c, err
}

// RevokeCustomer withdraws someone's access at once, in every browser.
func (s *Store) RevokeCustomer(org, grant int64, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var g CustomerGrant
		if err := tx.Where("org_id = ? and id = ?", org, grant).First(&g).Error; err != nil {
			return missing(err, "no access %d", grant)
		}
		if err := dropCustomerGrants(tx, org, tx.Where("org_id = ? and id = ?", org, grant), true); err != nil {
			return err
		}
		var c Contact
		if err := tx.Where("org_id = ? and id = ?", org, g.ContactID).First(&c).Error; err != nil {
			return nil
		}
		return s.log(tx, org, c.ID, c.CompanyID, actor, "onboard:revoke", nil)
	})
}

// CustomerInvites are the people let see each of an organisation's onboardings, by
// onboarding, with those whose access was withdrawn.
func (s *Store) CustomerInvites(org int64) (map[int64][]CustomerInvite, error) {
	var grants []CustomerGrant
	if err := s.in(org).Order("id").Find(&grants).Error; err != nil {
		return nil, err
	}
	out := map[int64][]CustomerInvite{}
	if len(grants) == 0 {
		return out, nil
	}
	var people []Contact
	if err := s.in(org).Where("id in (?)", s.in(org).Model(&CustomerGrant{}).Select("contact_id")).Find(&people).Error; err != nil {
		return nil, err
	}
	who := make(map[int64]Contact, len(people))
	for _, p := range people {
		who[p.ID] = p
	}
	for _, g := range grants {
		if p, ok := who[g.ContactID]; ok {
			out[g.OnboardingID] = append(out[g.OnboardingID], CustomerInvite{g, displayName(p), deref(p.Email)})
		}
	}
	return out, nil
}

// CustomerSteps are the steps of an onboarding that are the customer's to do. The
// organisation's own steps were written for its own eyes and are never read for this page.
func (s *Store) CustomerSteps(org int64, a *CustomerAccess) ([]OnboardingStep, error) {
	var steps []OnboardingStep
	return steps, s.in(org).Where("onboarding_id = ? and who = ?", a.Onboarding.ID, StepTheirs).Order("seq").Find(&steps).Error
}

func (s *Store) DropCustomerLink(token string) {
	_ = logged(s.db.Where("token_hash = ?", hashToken(token)).Delete(&CustomerLink{}))
}

// CustomerSetStep marks one of the customer's own steps done, or takes back a mark they
// made. A step a member marked stays as the member left it, and so does a step that
// isn't theirs or belongs to another onboarding.
func (s *Store) CustomerSetStep(org int64, a *CustomerAccess, step int64, done bool) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		// Held and read again inside, so access withdrawn or a person erased a moment ago marks nothing.
		if err := hold(tx, &CustomerGrant{}, org, a.Grant.ID); err != nil {
			return err
		}
		if err := tx.Where("id = ? and org_id = ? and revoked_at is null", a.Grant.ID, org).First(&CustomerGrant{}).Error; err != nil {
			return errNoCustomer
		}
		var st OnboardingStep
		if err := tx.Where("org_id = ? and onboarding_id = ? and id = ? and who = ?", org, a.Onboarding.ID, step, StepTheirs).First(&st).Error; err != nil {
			return errNoCustomer
		}
		updates, action := map[string]any{"done_on": today(), "done_by": customerActor}, "onboard:done"
		switch {
		case done && st.DoneOn != nil, !done && (st.DoneOn == nil || st.DoneBy != customerActor):
			return nil
		case !done:
			updates = map[string]any{"done_on": nil, "done_by": "", "done_entry": 0}
		}
		marked := tx.Model(&OnboardingStep{}).Where("id = ? and org_id = ? and done_by = ? and done_entry = ?", st.ID, org, st.DoneBy, st.DoneEntry).Updates(updates)
		if marked.Error != nil || marked.RowsAffected == 0 {
			return marked.Error
		}
		if done {
			entry := Event{OrgID: org, ContactID: a.Contact.ID, CompanyID: &a.Onboarding.CompanyID, User: customerActor, Action: action, Value: nonEmpty(st.Name), At: time.Now()}
			if err := tx.Create(&entry).Error; err != nil {
				return err
			}
			return tx.Model(&OnboardingStep{}).Where("org_id = ? and id = ?", org, st.ID).Update("done_entry", entry.ID).Error
		}
		// Taking a mark back removes the entry it wrote, not adds one: someone outside
		// pressing the two buttons in turn can't fill the company's history. A mark made
		// before steps kept their entry leaves it, since which one is theirs isn't known.
		if st.DoneEntry == 0 {
			return nil
		}
		return tx.Where("org_id = ? and id = ? and action = ?", org, st.DoneEntry, "onboard:done").Delete(&Event{}).Error
	})
}
