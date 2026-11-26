package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// A customer's page shows someone outside the organisation the onboarding steps that
// are theirs. They have no account: a member invites a contact, who is mailed a link.
// The link opens the page in one browser, once; another browser needs a link of its own,
// mailed to the same address. Nothing about who they may see comes from their request:
// the link or the browser's cookie names a grant, and the grant names one onboarding.

const (
	customerLinkLife    = 7 * 24 * time.Hour
	customerSessionLife = 30 * 24 * time.Hour
	// customerLinkGap is the least time between two links mailed for one grant, so
	// neither a member nor the page's own "send another" can fill somebody's inbox.
	customerLinkGap = 5 * time.Minute
	// customerLinkKept is how long a spent or expired link still earns a fresh one.
	customerLinkKept = 90 * 24 * time.Hour

	customerCookie = "customer"
)

// CustomerGrant lets one contact see one onboarding.
type CustomerGrant struct {
	ID           int64      `gorm:"primaryKey" json:"id"`
	OrgID        int64      `gorm:"uniqueIndex:idx_customer_grant,priority:1;not null" json:"-"`
	OnboardingID int64      `gorm:"uniqueIndex:idx_customer_grant,priority:2;not null" json:"onboarding_id"`
	ContactID    int64      `gorm:"uniqueIndex:idx_customer_grant,priority:3;index;not null" json:"contact_id"`
	InvitedBy    string     `gorm:"not null;default:''" json:"invited_by"`
	CreatedAt    time.Time  `json:"created_at"`
	RevokedAt    *time.Time `json:"revoked_at"`
}

// CustomerLink is a mailed link, stored as its hash. It opens the page once.
type CustomerLink struct {
	ID        int64  `gorm:"primaryKey"`
	OrgID     int64  `gorm:"index;not null"`
	GrantID   int64  `gorm:"index;not null"`
	TokenHash string `gorm:"uniqueIndex;not null"`
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// CustomerSession is one browser the page was opened in, stored as its cookie's hash.
type CustomerSession struct {
	ID        int64  `gorm:"primaryKey"`
	OrgID     int64  `gorm:"index;not null"`
	GrantID   int64  `gorm:"index;not null"`
	TokenHash string `gorm:"uniqueIndex;not null"`
	ExpiresAt time.Time
	CreatedAt time.Time
}

var errNoCustomer = errors.New("that link doesn't open anything: it may have been withdrawn")

// CustomerAccess is a grant with everything it stands on. It exists only while all of
// them do: the grant not withdrawn, its onboarding and its contact still there.
type CustomerAccess struct {
	Grant      CustomerGrant
	Org        Org
	Onboarding Onboarding
	Contact    Contact
	// Link is set when access was found by a link: it may be spent or expired.
	Link *CustomerLink
}

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

// invitable is whether a contact may have a page for an onboarding: they are a person,
// picked, at the company being onboarded, with one address a link can be mailed to, and
// not on the never-contact list.
func invitable(o *Org, ob Onboarding, c Contact) error {
	switch {
	case strings.HasPrefix(c.Key, "org:"):
		return errors.New("that row stands for a company, not a person")
	case c.CompanyID == nil || *c.CompanyID != ob.CompanyID:
		return errors.New("that person is not at the company being onboarded")
	case c.Status == ContactSuggested:
		return errors.New("this contact is still a suggestion in Research; a person picks them first")
	}
	if _, err := mail.ParseAddress(strings.TrimSpace(deref(c.Email))); err != nil {
		return errors.New("they have no single email address to send the link to")
	}
	return o.Settings.listed(deref(c.Email), deref(c.Linkedin))
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

func (l CustomerLink) opens() bool {
	return l.UsedAt == nil && l.ExpiresAt.After(time.Now())
}

// redeemCustomerLink spends a link and opens a session for the browser that sent it.
// The link is spent inside the transaction, so two requests can't both use it.
func (s *Store) redeemCustomerLink(a *CustomerAccess, token string) (string, error) {
	session := randomToken(32)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		spent := tx.Model(&CustomerLink{}).Where("token_hash = ? and used_at is null and expires_at > ?", hashToken(token), now).Update("used_at", now)
		if spent.Error != nil {
			return spent.Error
		}
		if spent.RowsAffected != 1 {
			return errNoCustomer
		}
		if err := tx.Where("org_id = ? and expires_at < ?", a.Grant.OrgID, now).Delete(&CustomerSession{}).Error; err != nil {
			return err
		}
		return tx.Create(&CustomerSession{OrgID: a.Grant.OrgID, GrantID: a.Grant.ID, TokenHash: hashToken(session), ExpiresAt: now.Add(customerSessionLife)}).Error
	})
	return session, err
}

func (s *Store) endCustomerSession(token string) error {
	return s.db.Where("token_hash = ?", hashToken(token)).Delete(&CustomerSession{}).Error
}

var errLinkTooSoon = errors.New("a link was sent a moment ago: it can take a minute to arrive")

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

// dropCustomerGrants ends access for the grants a query matches: the links and the
// browsers go with them. keep leaves the grant rows, withdrawn, as a record.
func dropCustomerGrants(tx *gorm.DB, org int64, matching *gorm.DB, keep bool) error {
	var ids []int64
	if err := matching.Model(&CustomerGrant{}).Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	for _, table := range []any{&CustomerLink{}, &CustomerSession{}} {
		if err := tx.Where("org_id = ? and grant_id in ?", org, ids).Delete(table).Error; err != nil {
			return err
		}
	}
	if keep {
		return tx.Model(&CustomerGrant{}).Where("org_id = ? and id in ? and revoked_at is null", org, ids).Update("revoked_at", time.Now()).Error
	}
	return tx.Where("org_id = ? and id in ?", org, ids).Delete(&CustomerGrant{}).Error
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

// CustomerInvite is who has been let see an onboarding, for the members' pages.
type CustomerInvite struct {
	CustomerGrant
	Name, Email string
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

// customerSteps are the steps of an onboarding that are the customer's to do. The
// organisation's own steps were written for its own eyes and are never read for this page.
func (s *Store) customerSteps(a *CustomerAccess) ([]OnboardingStep, error) {
	var steps []OnboardingStep
	return steps, s.db.Where("org_id = ? and onboarding_id = ? and who = ?", a.Grant.OrgID, a.Onboarding.ID, StepTheirs).Order("seq").Find(&steps).Error
}

var errNoCustomerMail = errors.New("mail isn't set up, so the link can't be sent: an owner sets a mail server in Settings, and the server needs PROSPECTOR_URL")

// customerMailOn is whether a link can be mailed for this organisation: it needs a mail
// server, its own or the app's, and PROSPECTOR_URL, because a link is never built from
// what a request claims its host is.
func (a *App) customerMailOn(org int64) bool {
	via, err := a.mailFor(org)
	return err == nil && via != nil && a.base != ""
}

func (s *Store) dropCustomerLink(token string) {
	logged(s.db.Where("token_hash = ?", hashToken(token)).Delete(&CustomerLink{}))
}

func (a *App) customerLinkMail(ctx context.Context, org *Org, to, token string) error {
	via, err := a.mailFor(org.ID)
	if err != nil {
		return err
	}
	if via == nil || a.base == "" {
		return errNoCustomerMail
	}
	return a.deliver(ctx, *via, Message{To: to, Subject: org.Name + ": your onboarding steps",
		Body: org.Name + " has a page showing the steps that are yours while they set you up.\n\n" +
			"Open it here:\n" + a.base + "/c/" + token + "\n\n" +
			"The link works once, within 7 days, and opens the page in the browser you use it in. " +
			"To open it somewhere else, use the link again and ask for a fresh one: it is sent to this address.\n\n" +
			"If you weren't expecting this, ignore it: nothing happens unless the link is opened.\n"})
}

// customerHeaders are for every page an outsider opens: a link in the address mustn't
// travel to another site in a Referer, and none of it belongs in a search index.
func customerHeaders(w http.ResponseWriter) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("content-type", "text/html; charset=utf-8")
}

func (a *App) customerRender(w http.ResponseWriter, r *http.Request, code int, v CustomerView) {
	customerHeaders(w)
	shown := customerShown{Page: "customer", State: v.State, Token: v.Token, Org: v.Org, Email: v.Email, Day: v.Day, Steps: []customerStepShown{}}
	for _, st := range v.Steps {
		shown.Steps = append(shown.Steps, customerStepShown{st.ID, st.Name, st.Due, st.DoneOn, st.DoneBy == customerActor})
	}
	writeShell(w, code, shown, "This page needs a browser with JavaScript.\n")
}

// customerShown is CustomerView as the page gets it. A step says only whether the
// customer marked it, never which member did.
type customerShown struct {
	Page  string              `json:"page"`
	State string              `json:"state"`
	Token string              `json:"token,omitempty"`
	Org   string              `json:"org,omitempty"`
	Email string              `json:"email,omitempty"`
	Day   string              `json:"day,omitempty"`
	Steps []customerStepShown `json:"steps"`
}

type customerStepShown struct {
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	Due    *string `json:"due"`
	DoneOn *string `json:"done_on"`
	Mine   bool    `json:"mine"`
}

// CustomerView is what a customer's page shows. It holds no teammate's name and no
// step that isn't the customer's.
type CustomerView struct {
	// Token is the link being opened, on the pages that ask for it to be confirmed.
	Token string
	// State is which page this is: "open", "again", "sent", "none" or "steps".
	State string
	Org   string
	Email string
	Steps []OnboardingStep
	Day   string
}

// customerLinkPage shows what a mailed link offers, and changes nothing: mail scanners
// and previews follow links, so opening it takes a press of the button.
func (a *App) customerLinkPage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	acc, err := a.s.CustomerByToken("link", token)
	if err != nil {
		a.customerRender(w, r, http.StatusNotFound, CustomerView{State: "none"})
		return
	}
	state := "again"
	if acc.Link.opens() {
		state = "open"
	}
	a.customerRender(w, r, http.StatusOK, CustomerView{State: state, Token: token, Org: acc.Org.Name})
}

func (a *App) customerOpen(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	acc, err := a.s.CustomerByToken("link", token)
	if err != nil {
		a.customerRender(w, r, http.StatusNotFound, CustomerView{State: "none"})
		return
	}
	session, err := a.s.redeemCustomerLink(acc, token)
	if err != nil {
		a.customerRender(w, r, http.StatusOK, CustomerView{State: "again", Token: token, Org: acc.Org.Name})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: customerCookie, Value: session, Path: "/c", HttpOnly: true, Secure: a.secure(r),
		SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(customerSessionLife)})
	customerHeaders(w)
	http.Redirect(w, r, "/c", http.StatusSeeOther)
}

// customerAgain mails a fresh link to the address the old one was sent to, and says
// the same thing whether or not it did: the page can't be used to learn whether a link
// was ever real, nor to reach any address but the invited one.
func (a *App) customerAgain(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if acc, err := a.s.CustomerByToken("link", token); err == nil && a.customerMailOn(acc.Org.ID) {
		var fresh string
		err := a.s.db.Transaction(func(tx *gorm.DB) error {
			var err error
			fresh, err = a.s.newCustomerLink(tx, acc.Grant)
			return err
		})
		if err == nil {
			org, to := acc.Org, deref(acc.Contact.Email)
			a.background(func() {
				ctx, cancel := context.WithTimeout(context.Background(), sendLimit)
				defer cancel()
				if err := a.customerLinkMail(ctx, &org, to, fresh); err != nil {
					// Nothing was sent, so the wait before another try doesn't apply.
					a.s.dropCustomerLink(fresh)
					log.Printf("a customer's link wasn't sent: %v", err)
				}
			})
		}
	}
	a.customerRender(w, r, http.StatusOK, CustomerView{State: "sent"})
}

func (a *App) customerSession(r *http.Request) (*CustomerAccess, string) {
	c, err := r.Cookie(customerCookie)
	if err != nil {
		return nil, ""
	}
	acc, err := a.s.CustomerByToken("session", c.Value)
	if err != nil {
		return nil, ""
	}
	return acc, c.Value
}

func (a *App) customerPage(w http.ResponseWriter, r *http.Request) {
	acc, _ := a.customerSession(r)
	if acc == nil {
		a.customerRender(w, r, http.StatusOK, CustomerView{State: "none"})
		return
	}
	steps, err := a.s.customerSteps(acc)
	if err != nil {
		http.Error(w, "the page couldn't be read", http.StatusInternalServerError)
		return
	}
	v := CustomerView{State: "steps", Org: acc.Org.Name, Email: deref(acc.Contact.Email), Steps: steps, Day: today()}
	a.customerRender(w, r, http.StatusOK, v)
}

func (a *App) customerLeave(w http.ResponseWriter, r *http.Request) {
	if _, token := a.customerSession(r); token != "" {
		if err := a.s.endCustomerSession(token); err != nil {
			log.Print(err)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: customerCookie, Value: "", Path: "/c", HttpOnly: true, Secure: a.secure(r), MaxAge: -1})
	customerHeaders(w)
	http.Redirect(w, r, "/c", http.StatusSeeOther)
}

// customerControls fills in, for the members' pages, who has been let see each
// onboarding and who at its company could be: anyone picked, with an email address.
func (a *App) customerControls(org int64, rows []OnboardingRow) error {
	if len(rows) == 0 {
		return nil
	}
	invites, err := a.s.CustomerInvites(org)
	if err != nil {
		return err
	}
	var companies []int64
	for _, r := range rows {
		companies = append(companies, r.CompanyID)
	}
	var people []Contact
	if err := a.s.in(org).Where("company_id in ? and status <> ? and key not like ?", companies, ContactSuggested, "org:%").
		Where("coalesce(email, \x27\x27) <> \x27\x27").Order("id").Find(&people).Error; err != nil {
		return err
	}
	mailOn := a.customerMailOn(org)
	for i := range rows {
		rows[i].Invites, rows[i].MailOn = invites[rows[i].ID], mailOn
		for _, p := range people {
			if p.CompanyID != nil && *p.CompanyID == rows[i].CompanyID {
				rows[i].Invitable = append(rows[i].Invitable, p)
			}
		}
	}
	return nil
}

// customerActor is who a step says finished it when the customer marked it themselves.
// A fixed word, not their name: the person is on the history entry, which erasure removes.
const customerActor = "customer"

// customerSetStep marks one of the customer's own steps done, or takes back a mark they
// made. A step a member marked stays as the member left it, and so does a step that
// isn't theirs or belongs to another onboarding.
func (s *Store) customerSetStep(a *CustomerAccess, step int64, done bool) error {
	org := a.Grant.OrgID
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
			return tx.Model(&OnboardingStep{}).Where("id = ?", st.ID).Update("done_entry", entry.ID).Error
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

func (a *App) customerStep(done bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerHeaders(w)
		acc, _ := a.customerSession(r)
		if acc != nil {
			id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
			if err := a.s.customerSetStep(acc, id, done); err != nil && !errors.Is(err, errNoCustomer) {
				log.Print(err)
				http.Error(w, "that couldn't be saved", http.StatusInternalServerError)
				return
			}
		}
		// The same answer whether or not anything changed: the page shows what is so.
		http.Redirect(w, r, "/c", http.StatusSeeOther)
	}
}
