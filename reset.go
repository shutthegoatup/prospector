package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/mailer"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
)

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

func (a *App) deliver(ctx context.Context, via mailer.Server, m mailer.Message) error {
	if a.send != nil {
		return a.send(ctx, via, m)
	}
	return mailer.Send(ctx, via, m)
}

// MailFor is the organisation's own server when it has one, else the app's, else nil.
func (a *App) MailFor(org int64) (*mailer.Server, error) {
	m, err := a.s.OrgMail(org)
	if err != nil {
		return a.mail, nil
	}
	via := &mailer.Server{Host: m.Host, Port: m.Port, Username: m.Username, From: m.Sender}
	if m.Password != "" {
		if via.Password, err = a.s.Keyring.Open(m.Password); err != nil {
			return nil, fmt.Errorf("this organisation's mail password can't be read: %w", err)
		}
	}
	return via, nil
}

// sendReset mails a link to choose a new password, in the language its owner chose, or
// else in that of whoever asked for it, who is most likely the one who reads it.
func (a *App) sendReset(email, lang string) {
	u, err := a.s.UserByEmail(email)
	if err != nil {
		return
	}
	if u.Language != "" {
		lang = u.Language
	}
	token, err := a.s.ResetToken(u.ID)
	if err != nil {
		if !errors.Is(err, identity.ErrTooSoon) {
			log.Print("password reset: ", err)
		}
		return
	}
	err = a.deliver(context.Background(), *a.mail, mailer.Message{To: u.Email, Subject: say.New("Reset your Prospector password").In(lang),
		Body: say.Errorf("Someone asked to reset the password for this address at %s.\n\n"+
			"Choose a new one here, within the hour:\n%s\n\n"+
			"If that wasn't you, ignore this email: your password stays as it is.\n", a.base, a.base+"/reset/"+token).In(lang)})
	if err != nil {
		log.Print("password reset: ", err)
	}
}

// invite emails a new member a link to choose their password. It reports false when
// there is no way to send one, and then the first password is shown instead.
//
// The organisation's own server carries it only for an account this request created.
// Whoever runs that server reads the link, and for an account that was already here
// the link would hand them someone else's account.
func (a *App) invite(ctx context.Context, q *request.Req, u *User, created bool) (bool, error) {
	via, err := a.mail, error(nil)
	if created {
		via, err = a.MailFor(q.Org.ID)
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
	// Not the language of whoever added them, who may read another.
	lang := u.Language
	if lang == "" {
		lang = q.Org.Settings.WithDefaults().CustomerLanguage
	}
	err = a.deliver(ctx, *via, mailer.Message{To: u.Email, Subject: say.Errorf("%s added you to %s on Prospector", q.Actor(), q.Org.Name).In(lang),
		Body: say.Errorf("%s added you to %s on Prospector, at %s.\n\n"+
			"Choose your password here, within 7 days:\n%s\n\n"+
			"Then sign in with %s.\n", q.Actor(), q.Org.Name, a.base, a.base+"/reset/"+token, u.Email).In(lang)})
	if err != nil {
		// Nothing was sent, so the wait before another try doesn't apply.
		if err := a.s.DropInvites(u.ID); err != nil {
			log.Printf("user %d: unsent invite not dropped: %v", u.ID, err)
		}
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

type mailIn struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Sender   string `json:"sender"`
}

// saveMail keeps an organisation's own mail server, or drops it for an empty host.
func (a *App) saveMail(org int64, in mailIn) (cleared bool, err error) {
	in.Host = strings.TrimSpace(in.Host)
	if in.Host == "" {
		return true, a.s.DeleteOrgMail(org)
	}
	m := &OrgMail{OrgID: org, Host: in.Host, Port: in.Port, Username: strings.TrimSpace(in.Username), Sender: strings.TrimSpace(in.Sender)}
	err = (mailer.Server{Host: m.Host, Port: m.Port, From: m.Sender}).Check()
	// Only the operator may name this machine, where any port and no TLS are allowed.
	if err == nil && mailer.Loopback(m.Host) {
		err = say.New("an organisation's mail server can't be this machine")
	}
	if err != nil {
		return false, err
	}
	// An empty password field means the one already saved, which is never sent back.
	if old, err := a.s.OrgMail(org); err == nil && in.Password == "" && old.Host == m.Host && old.Username == m.Username {
		m.Password = old.Password
	} else if in.Password != "" {
		if a.s.Keyring == nil {
			return false, say.New("this server has no secret key, so it can't keep a mail password safely: ask whoever runs it")
		}
		if m.Password, err = a.s.Keyring.Seal(in.Password); err != nil {
			return false, err
		}
	}
	return false, a.s.SaveOrgMail(m)
}

func (a *App) testMail(ctx context.Context, q *request.Req) (string, error) {
	via, err := a.MailFor(q.Org.ID)
	if err == nil && via == nil {
		err = say.New("there is no mail server to test: save one first")
	}
	if err != nil {
		return "", err
	}
	// It goes to whoever asked for it, so it is in the language they asked in.
	lang := request.LanguageOf(requestFrom(ctx))
	err = a.deliver(ctx, *via, mailer.Message{To: q.User.Email, Subject: say.Errorf("Prospector can send mail for %s", q.Org.Name).In(lang),
		Body: say.Errorf("This is the test %s asked for in Settings.\n\nIt was sent through %s as %s.\n", q.Actor(), via.Host, via.From).In(lang)})
	if err != nil {
		return "", notSent{Err: err}
	}
	return say.Errorf("Sent to %s through %s.", q.User.Email, via.Host).In(lang), nil
}
