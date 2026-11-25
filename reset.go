package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	resetLife  = time.Hour
	inviteLife = 7 * 24 * time.Hour
	// resetGap is the least time between two reset emails to one account, so the form
	// can't be used to fill somebody's inbox.
	resetGap = 5 * time.Minute
)

// PasswordToken lets whoever holds its link choose the account's password, once.
type PasswordToken struct {
	ID        int64  `gorm:"primaryKey"`
	TokenHash string `gorm:"uniqueIndex;not null"`
	UserID    int64  `gorm:"index;not null"`
	Invite    bool   `gorm:"not null;default:false"`
	ExpiresAt time.Time
	CreatedAt time.Time
}

// OrgMail is an organisation's own mail server. Password is sealed, never plain.
type OrgMail struct {
	OrgID     int64 `gorm:"primaryKey;autoIncrement:false"`
	Host      string
	Port      int
	Username  string
	Password  string
	Sender    string
	UpdatedAt time.Time
}

var (
	errTooSoon = errors.New("a reset link was sent to this account a moment ago")
	errBadLink = errors.New("this link has expired or has been used: ask for a new one")
)

func (s *Store) newPasswordToken(user int64, invite bool) (string, error) {
	life := resetLife
	if invite {
		life = inviteLife
	}
	token := randomToken(32)
	return token, s.db.Create(&PasswordToken{TokenHash: hashToken(token), UserID: user, Invite: invite, ExpiresAt: time.Now().Add(life)}).Error
}

// ResetToken refuses a second one inside resetGap. It counts from the table, so a
// restart doesn't reset the wait.
func (s *Store) ResetToken(user int64) (string, error) {
	var recent int64
	if err := s.db.Model(&PasswordToken{}).Where("user_id = ? and invite = ? and created_at > ?", user, false, time.Now().Add(-resetGap)).Count(&recent).Error; err != nil {
		return "", err
	}
	if recent > 0 {
		return "", errTooSoon
	}
	logged(s.db.Where("expires_at < ?", time.Now()).Delete(&PasswordToken{}))
	return s.newPasswordToken(user, false)
}

// InviteToken replaces any earlier invitation, and refuses a second inside resetGap, so
// adding someone over and over can't fill their inbox.
func (s *Store) InviteToken(user int64) (string, error) {
	var recent int64
	if err := s.db.Model(&PasswordToken{}).Where("user_id = ? and invite = ? and created_at > ?", user, true, time.Now().Add(-resetGap)).Count(&recent).Error; err != nil {
		return "", err
	}
	if recent > 0 {
		return "", errTooSoon
	}
	if err := s.dropInvites(user); err != nil {
		return "", err
	}
	return s.newPasswordToken(user, true)
}

func (s *Store) dropInvites(user int64) error {
	return s.db.Where("user_id = ? and invite = ?", user, true).Delete(&PasswordToken{}).Error
}

func (s *Store) PasswordTokenUser(token string) (*User, error) {
	var t PasswordToken
	if err := s.db.Where("token_hash = ? and expires_at > ?", hashToken(token), time.Now()).First(&t).Error; err != nil {
		return nil, errBadLink
	}
	var u User
	return &u, s.db.First(&u, t.UserID).Error
}

// RedeemPasswordToken sets the password and signs the account out everywhere. The
// token is spent first, inside the transaction, so two requests can't both use it.
func (s *Store) RedeemPasswordToken(token, password string) error {
	u, err := s.PasswordTokenUser(token)
	if err != nil {
		return err
	}
	if len(password) < 10 {
		return errors.New("passwords need at least 10 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		spent := tx.Where("token_hash = ? and expires_at > ?", hashToken(token), time.Now()).Delete(&PasswordToken{})
		if spent.Error != nil {
			return spent.Error
		}
		if spent.RowsAffected != 1 {
			return errBadLink
		}
		return s.savePasswordIn(tx, u.ID, string(hash), "")
	})
}

func (s *Store) OrgMail(org int64) (*OrgMail, error) {
	var m OrgMail
	return &m, s.db.First(&m, "org_id = ?", org).Error
}

func (s *Store) SaveOrgMail(m *OrgMail) error { return s.db.Save(m).Error }

func (s *Store) DeleteOrgMail(org int64) error {
	return s.db.Delete(&OrgMail{}, "org_id = ?", org).Error
}

// mailSlots bounds the sends running at once; a request past that is dropped rather
// than queued, so a flood of them can't pile up.
var mailSlots = make(chan struct{}, 4)

func (a *App) background(f func()) {
	if a.later != nil {
		a.later(f)
		return
	}
	select {
	case mailSlots <- struct{}{}:
		a.sending.Add(1)
		go func() {
			defer a.sending.Done()
			defer func() { <-mailSlots }()
			f()
		}()
	default:
		log.Print("mail is backed up: a password reset wasn't sent")
	}
}

// mailOn is whether the app can email a link back to itself: it needs a server, and
// PROSPECTOR_URL, because a link is never built from what a request claims its host is.
func (a *App) mailOn() bool { return a.mail != nil && a.base != "" }

func (a *App) deliver(ctx context.Context, via MailServer, m Message) error {
	if a.send != nil {
		return a.send(ctx, via, m)
	}
	return smtpSend(ctx, via, m)
}

// mailFor is the organisation's own server when it has one, else the app's, else nil.
func (a *App) mailFor(org int64) (*MailServer, error) {
	m, err := a.s.OrgMail(org)
	if err != nil {
		return a.mail, nil
	}
	via := &MailServer{Host: m.Host, Port: m.Port, Username: m.Username, From: m.Sender}
	if m.Password != "" {
		if via.Password, err = a.s.keys.open(m.Password); err != nil {
			return nil, fmt.Errorf("this organisation's mail password can't be read: %w", err)
		}
	}
	return via, nil
}

func (a *App) forgotPage(w http.ResponseWriter, r *http.Request) {
	if !a.mailOn() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("content-type", "text/html; charset=utf-8")
	ForgotPage(false).Render(r.Context(), w)
}

// forgot answers the same way, and as fast, whether or not the address has an
// account: the lookup and the send happen after the answer.
func (a *App) forgot(w http.ResponseWriter, r *http.Request) {
	if !a.mailOn() {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	email := r.PostForm.Get("email")
	a.background(func() { a.sendReset(email) })
	w.Header().Set("content-type", "text/html; charset=utf-8")
	ForgotPage(true).Render(r.Context(), w)
}

func (a *App) sendReset(email string) {
	u, err := a.s.UserByEmail(email)
	if err != nil {
		return
	}
	token, err := a.s.ResetToken(u.ID)
	if err != nil {
		if !errors.Is(err, errTooSoon) {
			log.Print("password reset: ", err)
		}
		return
	}
	err = a.deliver(context.Background(), *a.mail, Message{To: u.Email, Subject: "Reset your Prospector password",
		Body: "Someone asked to reset the password for this address at " + a.base + ".\n\n" +
			"Choose a new one here, within the hour:\n" + a.base + "/reset/" + token + "\n\n" +
			"If that wasn't you, ignore this email: your password stays as it is.\n"})
	if err != nil {
		log.Print("password reset: ", err)
	}
}

func (a *App) resetPage(w http.ResponseWriter, r *http.Request) {
	u, err := a.s.PasswordTokenUser(r.PathValue("token"))
	if err != nil {
		fail(w, errBadLink, http.StatusGone)
		return
	}
	w.Header().Set("content-type", "text/html; charset=utf-8")
	ResetPage(r.PathValue("token"), u.Email, "").Render(r.Context(), w)
}

func (a *App) reset(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	token := r.PathValue("token")
	u, err := a.s.PasswordTokenUser(token)
	if err != nil {
		fail(w, errBadLink, http.StatusGone)
		return
	}
	select {
	case hashSlots <- struct{}{}:
		defer func() { <-hashSlots }()
	case <-r.Context().Done():
		return
	}
	if err := a.s.RedeemPasswordToken(token, r.PostForm.Get("password")); errors.Is(err, errBadLink) {
		fail(w, err, http.StatusGone)
		return
	} else if err != nil {
		w.Header().Set("content-type", "text/html; charset=utf-8")
		code, text := telling("password reset", err, http.StatusBadRequest)
		w.WriteHeader(code)
		ResetPage(token, u.Email, text).Render(r.Context(), w)
		return
	}
	http.Redirect(w, r, "/login?reset=1", http.StatusSeeOther)
}

// invite emails a new member a link to choose their password. It reports false when
// there is no way to send one, and then the first password is shown instead.
//
// The organisation's own server carries it only for an account this request created.
// Whoever runs that server reads the link, and for an account that was already here
// the link would hand them someone else's account.
func (a *App) invite(ctx context.Context, q *Req, u *User, created bool) (bool, error) {
	via, err := a.mail, error(nil)
	if created {
		via, err = a.mailFor(q.Org.ID)
	}
	if a.base == "" || (via == nil && err == nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	token, err := a.s.InviteToken(u.ID)
	if err != nil {
		return false, err
	}
	err = a.deliver(ctx, *via, Message{To: u.Email, Subject: q.Actor() + " added you to " + q.Org.Name + " on Prospector",
		Body: q.Actor() + " added you to " + q.Org.Name + " on Prospector, at " + a.base + ".\n\n" +
			"Choose your password here, within 7 days:\n" + a.base + "/reset/" + token + "\n\n" +
			"Then sign in with " + u.Email + ".\n"})
	if err != nil {
		// Nothing was sent, so the wait before another try doesn't apply.
		a.s.dropInvites(u.ID)
	}
	return err == nil, err
}

// MailView is what Settings shows of an organisation's server: never the password.
type MailView struct {
	Host, Username, Sender string
	Port                   int
	HasPassword            bool
	// SiteMail is whether the app has a server of its own to fall back to.
	SiteMail bool
}

func (a *App) mailView(org int64) MailView {
	v := MailView{Port: 587, SiteMail: a.mailOn()}
	if m, err := a.s.OrgMail(org); err == nil {
		v.Host, v.Port, v.Username, v.Sender, v.HasPassword = m.Host, m.Port, m.Username, m.Sender, m.Password != ""
	}
	return v
}

func (a *App) uiMail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
		Sender   string `json:"sender"`
	}
	if !decode(w, r, &in) {
		return
	}
	org := reqOf(r).Org.ID
	in.Host = strings.TrimSpace(in.Host)
	if in.Host == "" {
		if err := a.s.DeleteOrgMail(org); err != nil {
			fail(w, err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"message": "No mail server of this organisation's own."})
		return
	}
	m := &OrgMail{OrgID: org, Host: in.Host, Port: in.Port, Username: strings.TrimSpace(in.Username), Sender: strings.TrimSpace(in.Sender)}
	err := (MailServer{Host: m.Host, Port: m.Port, From: m.Sender}).check()
	// Only the operator may name this machine, where any port and no TLS are allowed.
	if err == nil && loopback(m.Host) {
		err = errors.New("an organisation's mail server can't be this machine")
	}
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	// An empty password field means the one already saved, which is never sent back.
	if old, err := a.s.OrgMail(org); err == nil && in.Password == "" && old.Host == m.Host && old.Username == m.Username {
		m.Password = old.Password
	} else if in.Password != "" {
		if a.s.keys == nil {
			fail(w, errors.New("this server has no secret key, so it can't keep a mail password safely: ask whoever runs it"), http.StatusBadRequest)
			return
		}
		kept, err := a.s.keys.seal(in.Password)
		if err != nil {
			fail(w, err, http.StatusInternalServerError)
			return
		}
		m.Password = kept
	}
	done(w, a.s.SaveOrgMail(m))
}

// uiMailTest sends the owner a message through whichever server invitations would use.
func (a *App) uiMailTest(w http.ResponseWriter, r *http.Request) {
	q := reqOf(r)
	via, err := a.mailFor(q.Org.ID)
	if err == nil && via == nil {
		err = errors.New("there is no mail server to test: save one first")
	}
	if err == nil {
		err = a.deliver(r.Context(), *via, Message{To: q.User.Email, Subject: "Prospector can send mail for " + q.Org.Name,
			Body: "This is the test " + q.Actor() + " asked for in Settings.\n\nIt was sent through " + via.Host + " as " + via.From + ".\n"})
	}
	if err != nil {
		fail(w, err, http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"message": fmt.Sprintf("Sent to %s through %s.", q.User.Email, via.Host)})
}
