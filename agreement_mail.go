package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/mail"
	"strings"
	"time"

	"gorm.io/gorm"
)

// The mail an agreement sends goes through a queue: a row is written in the transaction
// that calls for the mail, and a loop sends it through the organisation's own mail
// server. What the record says was sent is what the server took. A link's token is made
// as its mail goes, so none is ever stored but as a hash.

const (
	mailLink      = "link"      // a signer's link to sign, or a member's to the app
	mailCompleted = "completed" // the sealed copy, to every signer and copy
	mailVoided    = "voided"    // withdrawn, to signers already sent it
	mailDeclined  = "declined"  // to the sender, when a signer declines

	mailQueued  = "queued"
	mailSending = "sending"
	mailSent    = "sent"
	mailFailed  = "failed"

	mailTries = 8
)

// AgreementMail is one mail an agreement sends, queued until it goes.
type AgreementMail struct {
	ID          int64     `gorm:"primaryKey"`
	OrgID       int64     `gorm:"index;not null"`
	AgreementID int64     `gorm:"index;not null"`
	RecipientID int64     `gorm:"index;not null;default:0"`
	Kind        string    `gorm:"not null;default:''"`
	Status      string    `gorm:"index;not null;default:'queued'"`
	Tries       int       `gorm:"not null;default:0"`
	NextAt      time.Time `gorm:"not null"`
	Error       string    `gorm:"not null;default:''"`
	CreatedAt   time.Time
	SentAt      *time.Time
}

func queueAgreementMail(tx *gorm.DB, a Agreement, r AgreementRecipient, kind string) error {
	return tx.Create(&AgreementMail{OrgID: a.OrgID, AgreementID: a.ID, RecipientID: r.ID, Kind: kind, Status: mailQueued, NextAt: time.Now()}).Error
}

// MailProblems are, by recipient, why the latest mail an agreement queued for them
// hasn't gone: still being tried again, or given up on.
func (s *Store) MailProblems(org, id int64) (map[int64]string, error) {
	var mails []AgreementMail
	if err := s.in(org).Where("agreement_id = ?", id).Order("id").Find(&mails).Error; err != nil {
		return nil, err
	}
	out := map[int64]string{}
	for _, m := range mails {
		if m.Status == mailSent || m.Tries == 0 {
			delete(out, m.RecipientID)
			continue
		}
		why := "not sent yet: " + m.Error
		if m.Status == mailFailed {
			why = "couldn't be sent: " + m.Error
		}
		out[m.RecipientID] = why
	}
	return out, nil
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// keepSigning sends queued agreement mail, seals agreements everyone has signed, and
// ends those whose time ran out, as long as the server runs.
func (a *App) keepSigning(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		a.signingRound(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// signingRound is one go of the loop; safe beside another server doing the same.
func (a *App) signingRound(ctx context.Context, now time.Time) {
	logged(a.s.db.Model(&AgreementMail{}).Where("status = ? and next_at < ?", mailSending, now.Add(-10*time.Minute)).Update("status", mailQueued))
	if err := a.s.expireAgreements(now); err != nil {
		log.Printf("ending agreements whose time ran out: %v", err)
	}
	var sealing []Agreement
	if err := a.s.db.Where("status = ?", AgreementSealing).Find(&sealing).Error; err != nil {
		log.Print(err)
	}
	for _, ag := range sealing {
		if err := a.sealAgreement(ag); err != nil {
			log.Printf("sealing agreement %d: %v", ag.ID, err)
			logged(a.s.db.Model(&Agreement{}).Where("id = ? and status = ?", ag.ID, AgreementSealing).Update("seal_problem", err.Error()))
		}
	}
	var queued []AgreementMail
	if err := a.s.db.Where("status = ? and next_at <= ?", mailQueued, now).Order("id").Limit(50).Find(&queued).Error; err != nil {
		log.Print(err)
	}
	for _, m := range queued {
		if ctx.Err() != nil {
			return
		}
		a.sendAgreementMail(ctx, m)
	}
}

// sealAgreement seals an agreement everyone has signed: fields filled in from what each
// signer gave, the certificate drawn from its events, then sealed and kept, and the
// sealed copy sent to everyone on it.
func (a *App) sealAgreement(ag Agreement) error {
	var o Org
	if err := a.s.db.First(&o, ag.OrgID).Error; err != nil {
		return err
	}
	_, doc, err := a.s.AgreementDocument(o.ID, ag.ID)
	if err != nil {
		return err
	}
	var fields []AgreementField
	if err := a.s.db.Where("org_id = ? and agreement_id = ? and round = ?", o.ID, ag.ID, ag.Round).Find(&fields).Error; err != nil {
		return err
	}
	stamps := make([]Stamp, 0, len(fields))
	for _, f := range fields {
		stamps = append(stamps, Stamp{Page: f.Page, X: f.X, Y: f.Y, W: f.W, H: f.H, Kind: f.Kind, Value: f.Value})
	}
	c, err := a.s.certificateFor(&o, ag)
	if err != nil {
		return err
	}
	out, err := a.s.seal(&o, doc, stamps, *c)
	if err != nil {
		return err
	}
	return a.s.db.Transaction(func(tx *gorm.DB) error {
		locked, err := lockAgreement(tx, o.ID, ag.ID)
		if err != nil {
			return err
		}
		if locked.Status != AgreementSealing {
			return nil
		}
		if err := tx.Create(&AgreementSealed{AgreementID: ag.ID, OrgID: o.ID, Data: out.PDF}).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := tx.Model(&Agreement{}).Where("id = ? and status = ?", ag.ID, AgreementSealing).
			Updates(map[string]any{"status": AgreementCompleted, "sealed_hash": out.Hash, "completed_at": now, "seal_problem": ""}).Error; err != nil {
			return err
		}
		if _, err := recordAgreementEvent(tx, AgreementEvent{OrgID: o.ID, AgreementID: ag.ID, Round: ag.Round, At: now, Kind: EventSealed,
			Detail: "Sealed, SHA-256 " + out.Hash}); err != nil {
			return err
		}
		var everyone []AgreementRecipient
		if err := tx.Where("org_id = ? and agreement_id = ? and erased = ?", o.ID, ag.ID, false).Find(&everyone).Error; err != nil {
			return err
		}
		for _, r := range everyone {
			if err := queueAgreementMail(tx, *locked, r, mailCompleted); err != nil {
				return err
			}
		}
		return nil
	})
}

// printed are the kinds of event a certificate shows; the rest stay in the record.
var printed = map[string]bool{EventSent: true, EventLinkSent: true, EventCodeEntered: true, EventConsented: true, EventSigned: true, EventDeclined: true, EventVoided: true}

// certificateFor draws a sealed agreement's certificate from its record: what was frozen
// when it was sent, and its events. Nothing on it is typed in afterwards.
func (s *Store) certificateFor(o *Org, ag Agreement) (*Certificate, error) {
	events, err := s.AgreementEvents(o.ID, ag.ID)
	if err != nil {
		return nil, err
	}
	var parties []AgreementRecipient
	if err := s.db.Where("org_id = ? and agreement_id = ? and role = ?", o.ID, ag.ID, RecipientSigner).Order("position, id").Find(&parties).Error; err != nil {
		return nil, err
	}
	pages, err := s.agreementPages(o.ID, &ag)
	if err != nil {
		return nil, err
	}
	c := &Certificate{Org: ag.SentOrg, Agreement: ag.Name, Ref: fmt.Sprintf("AGR-%04d", ag.ID), Pages: pages, SentBy: ag.SentBy, CompletedAt: time.Now().UTC(),
		Rounds: []CertificateRound{{N: ag.Round, Hash: ag.FileHash, Outcome: "signed by everyone"}}, Consent: signingConsent}
	if ag.SentAt != nil {
		c.SentAt = *ag.SentAt
	}
	signedFrom := map[int64]AgreementEvent{}
	for _, e := range events {
		if e.Kind == EventSigned && e.RecipientID != nil {
			signedFrom[*e.RecipientID] = e
		}
		if printed[e.Kind] && e.Round == ag.Round {
			c.Events = append(c.Events, CertificateEvent{At: e.At, Text: e.Detail})
		}
	}
	for _, r := range parties {
		e := signedFrom[r.ID]
		signer := CertificateSigner{Name: r.SentName, Email: r.SentEmail, Via: r.SignedVia, Typed: r.TypedName, IP: e.IP, Browser: e.UserAgent}
		if r.SignedAt != nil {
			signer.SignedAt = *r.SignedAt
		}
		c.Signers = append(c.Signers, signer)
	}
	if len(events) > 0 {
		c.LastEvent = events[len(events)-1].Hash
	}
	return c, nil
}

// sendAgreementMail claims a queued mail, writes it and sends it. What has to happen
// with it (a link made, an event recorded) happens once the server has taken it.
func (a *App) sendAgreementMail(ctx context.Context, m AgreementMail) {
	res := a.s.db.Model(&AgreementMail{}).Where("id = ? and status = ?", m.ID, mailQueued).Updates(map[string]any{"status": mailSending, "next_at": time.Now()})
	if res.Error != nil || res.RowsAffected == 0 {
		return
	}
	err := a.writeAndSend(ctx, m)
	if err == nil {
		return
	}
	m.Tries++
	status, next := mailQueued, time.Now().Add(time.Duration(1<<min(m.Tries, 8))*time.Minute)
	if m.Tries >= mailTries || errors.Is(err, errNoMailToSend) {
		status = mailFailed
	}
	logged(a.s.db.Model(&AgreementMail{}).Where("id = ?", m.ID).Updates(map[string]any{"status": status, "tries": m.Tries, "next_at": next, "error": err.Error()}))
	if !errors.Is(err, errNoMailToSend) {
		log.Printf("agreement %d's mail %d: %v", m.AgreementID, m.ID, err)
	}
}

// errNoMailToSend is a queued mail with nothing left to send: the agreement moved on, or
// whom it was for can't be mailed any more.
var errNoMailToSend = errors.New("nothing to send any more")

func (a *App) writeAndSend(ctx context.Context, m AgreementMail) error {
	var o Org
	if err := a.s.db.First(&o, m.OrgID).Error; err != nil {
		return err
	}
	ag, err := a.s.Agreement(o.ID, m.AgreementID)
	if err != nil {
		return err
	}
	var r AgreementRecipient
	if err := a.s.db.Where("org_id = ? and id = ?", o.ID, m.RecipientID).First(&r).Error; err != nil || r.Erased {
		return errNoMailToSend
	}
	// The never-contact list is checked as each mail goes, not only when it was sent:
	// someone added to it since hears nothing more.
	if m.Kind != mailDeclined && o.Settings.listed(r.SentEmail, "") != nil {
		return errNoMailToSend
	}
	via, err := a.campaignMail(o.ID)
	if err != nil {
		return err
	}
	if via == nil || a.base == "" {
		return errors.New("the organisation's own mail server, or this server's public address, has gone")
	}
	to, kind, event := r.SentEmail, EventLinkSent, ""
	var msg Message
	until := ""
	if ag.ExpiresAt != nil {
		until = ag.ExpiresAt.Format("2 January 2006")
	}
	// The link is made, and the mail written, first; it is sent with no transaction open,
	// since a mail server can take its time; then what went is recorded.
	err = a.s.db.Transaction(func(tx *gorm.DB) error {
		var link string
		switch m.Kind {
		case mailLink:
			if ag.Status != AgreementSent || r.Status != SignerDue {
				return errNoMailToSend
			}
			if r.UserID != nil {
				link = fmt.Sprintf("%s/o/%s/agreements/%d/sign", a.base, o.Slug, ag.ID)
			} else if link, err = newSignerLink(tx, *ag, r, *ag.ExpiresAt); err == nil {
				link = a.base + "/s/" + link
			}
			msg = Message{Subject: ag.Subject, Body: fmt.Sprintf("%s has sent you %s to sign.\n\n%s\n\nOpen it here:\n%s\n\nTo sign you'll be asked for a code we email to this address, and to type your name. "+
				"The link works until %s.\n\nIf this wasn't meant for you, you can ignore it.\n", ag.SentOrg, ag.Name, mailQuote(ag.Message), link, until)}
			event = fmt.Sprintf("Link emailed to %s", r.SentEmail)
		case mailCompleted:
			if ag.Status != AgreementCompleted {
				return errNoMailToSend
			}
			if r.UserID != nil {
				link = fmt.Sprintf("%s/o/%s/companies/%d", a.base, o.Slug, ag.CompanyID)
			} else if link, err = newSignerLink(tx, *ag, r, time.Now().Add(copyLife)); err == nil {
				link = a.base + "/s/" + link
			}
			msg = Message{Subject: "Signed: " + ag.Name, Body: fmt.Sprintf("%s has been signed by everyone and sealed. Download the signed copy, with its certificate, here:\n%s\n\n"+
				"You'll be asked for a code we email to this address. The link works for 90 days.\n", ag.Name, link)}
			kind, event = EventCopySent, fmt.Sprintf("Sealed copy emailed to %s", r.SentEmail)
		case mailVoided:
			msg = Message{Subject: "Withdrawn: " + ag.Name, Body: fmt.Sprintf("%s has withdrawn %s. There is nothing for you to sign, and the link you were sent no longer opens it.\n", ag.SentOrg, ag.Name)}
			kind = ""
		case mailDeclined:
			var sender User
			if ag.SentByUser == 0 || tx.First(&sender, ag.SentByUser).Error != nil {
				return errNoMailToSend
			}
			to = sender.Email
			reason := ""
			if r.DeclineReason != "" {
				reason = "\n\nTheir reason:\n" + mailQuote(r.DeclineReason)
			}
			msg = Message{Subject: "Declined: " + ag.Name, Body: fmt.Sprintf("%s declined to sign %s.%s\n\nIt can't be signed now. Start it again from %s/o/%s/companies/%d.\n",
				r.SentName, ag.Name, reason, a.base, o.Slug, ag.CompanyID)}
			kind = ""
		default:
			return errNoMailToSend
		}
		if err != nil {
			return err
		}
		if _, err := mail.ParseAddress(to); err != nil {
			return errNoMailToSend
		}
		msg.To = to
		if m.Kind != mailDeclined {
			var sender User
			if ag.SentByUser != 0 && tx.First(&sender, ag.SentByUser).Error == nil {
				msg.ReplyTo = sender.Email
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := a.deliver(ctx, *via, msg); err != nil {
		return err
	}
	now := time.Now().UTC()
	return a.s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&AgreementMail{}).Where("id = ?", m.ID).Updates(map[string]any{"status": mailSent, "sent_at": now, "error": ""}).Error; err != nil {
			return err
		}
		if kind == "" {
			return nil
		}
		if _, err := lockAgreement(tx, o.ID, ag.ID); err != nil {
			return err
		}
		rid := r.ID
		_, err := recordAgreementEvent(tx, AgreementEvent{OrgID: o.ID, AgreementID: ag.ID, Round: ag.Round, At: now, Kind: kind, RecipientID: &rid, Detail: event})
		return err
	})
}

// mailQuote is a sender's own words set apart in a plain-text mail.
func mailQuote(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "> " + l
	}
	return strings.Join(lines, "\n")
}
