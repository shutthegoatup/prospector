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

	"github.com/shutthegoatup/prospector/internal/mailer"
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

func (l CustomerLink) opens() bool {
	return l.UsedAt == nil && l.ExpiresAt.After(time.Now())
}

var errLinkTooSoon = errors.New("a link was sent a moment ago: it can take a minute to arrive")

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

// CustomerInvite is who has been let see an onboarding, for the members' pages.
type CustomerInvite struct {
	CustomerGrant
	Name, Email string
}

var errNoCustomerMail = errors.New("mail isn't set up, so the link can't be sent: an owner sets a mail server in Settings, and the server needs PROSPECTOR_URL")

// customerMailOn is whether a link can be mailed for this organisation: it needs a mail
// server, its own or the app's, and PROSPECTOR_URL, because a link is never built from
// what a request claims its host is.
func (a *App) customerMailOn(org int64) bool {
	via, err := a.mailFor(org)
	return err == nil && via != nil && a.base != ""
}

func (a *App) customerLinkMail(ctx context.Context, org *Org, to, token string) error {
	via, err := a.mailFor(org.ID)
	if err != nil {
		return err
	}
	if via == nil || a.base == "" {
		return errNoCustomerMail
	}
	return a.deliver(ctx, *via, mailer.Message{To: to, Subject: org.Name + ": your onboarding steps",
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
	writeShell(w, r, code, shown, "This page needs a browser with JavaScript.\n")
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
	session, err := a.s.RedeemCustomerLink(acc.Grant.OrgID, acc, token)
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
		if fresh, err := a.s.FreshCustomerLink(acc.Org.ID, acc.Grant.ID); err == nil {
			org, to := acc.Org, deref(acc.Contact.Email)
			a.background(func() {
				ctx, cancel := context.WithTimeout(context.Background(), mailer.SendLimit)
				defer cancel()
				if err := a.customerLinkMail(ctx, &org, to, fresh); err != nil {
					// Nothing was sent, so the wait before another try doesn't apply.
					a.s.DropCustomerLink(fresh)
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
	steps, err := a.s.CustomerSteps(acc.Grant.OrgID, acc)
	if err != nil {
		http.Error(w, "the page couldn't be read", http.StatusInternalServerError)
		return
	}
	v := CustomerView{State: "steps", Org: acc.Org.Name, Email: deref(acc.Contact.Email), Steps: steps, Day: today()}
	a.customerRender(w, r, http.StatusOK, v)
}

func (a *App) customerLeave(w http.ResponseWriter, r *http.Request) {
	if _, token := a.customerSession(r); token != "" {
		if err := a.s.EndCustomerSession(token); err != nil {
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
	people, err := a.s.Mailable(org, companies)
	if err != nil {
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

func (a *App) customerStep(done bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerHeaders(w)
		acc, _ := a.customerSession(r)
		if acc != nil {
			id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
			if err := a.s.CustomerSetStep(acc.Grant.OrgID, acc, id, done); err != nil && !errors.Is(err, errNoCustomer) {
				log.Print(err)
				http.Error(w, "that couldn't be saved", http.StatusInternalServerError)
				return
			}
		}
		// The same answer whether or not anything changed: the page shows what is so.
		http.Redirect(w, r, "/c", http.StatusSeeOther)
	}
}
