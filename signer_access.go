package main

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Someone outside the organisation signs from a link mailed to the address the agreement
// was sent to. The link only says who they are meant to be: each browser opens a session
// from it, and the session does nothing until it is given a code mailed to that same
// address. Nothing about what they may see comes from their request: the session names a
// link, the link a recipient, the recipient one agreement.

const (
	signerCookie = "signer"
	// sessionUnproven is how long a browser has to enter a code; sessionProven how long it
	// stays able to sign once it has.
	sessionUnproven = 30 * time.Minute
	sessionProven   = 24 * time.Hour
	codeLife        = 10 * time.Minute
	codeTries       = 5
	codeGap         = time.Minute
	codesADay       = 10
	// linkFailures is how many wrong codes, all told, kill a link: a new one has to be sent.
	linkFailures = 30
	// copyLife is how long the link to a sealed copy lasts.
	copyLife = 90 * 24 * time.Hour
)

var errNoSigning = errors.New("that link doesn't open anything: it may have run out or been withdrawn")

// SignerLink is a mailed link, stored as its hash. Signers' links last as long as the
// agreement is open; those sent with the sealed copy, 90 days.
type SignerLink struct {
	ID          int64     `gorm:"primaryKey"`
	OrgID       int64     `gorm:"index;not null"`
	AgreementID int64     `gorm:"index;not null"`
	RecipientID int64     `gorm:"index;not null"`
	TokenHash   string    `gorm:"uniqueIndex;not null"`
	ExpiresAt   time.Time `gorm:"not null"`
	Failures    int       `gorm:"not null;default:0"`
	CreatedAt   time.Time
}

// SignerSession is one browser a link was opened in, stored as its cookie's hash. Proven
// is set once it has given the code mailed for it.
type SignerSession struct {
	ID          int64     `gorm:"primaryKey"`
	OrgID       int64     `gorm:"index;not null"`
	LinkID      int64     `gorm:"index;not null"`
	TokenHash   string    `gorm:"uniqueIndex;not null"`
	ExpiresAt   time.Time `gorm:"not null"`
	Proven      bool      `gorm:"not null;default:false"`
	CodeHash    string    `gorm:"not null;default:''"`
	CodeExpires *time.Time
	CodeTries   int `gorm:"not null;default:0"`
	CodeSentAt  *time.Time
	Opened      bool `gorm:"not null;default:false"`
	Downloaded  bool `gorm:"not null;default:false"`
	CreatedAt   time.Time
}

// newSignerLink makes a link for a recipient, returning its token, which is never stored.
func newSignerLink(tx *gorm.DB, a Agreement, r AgreementRecipient, until time.Time) (string, error) {
	token := randomToken(24)
	return token, tx.Create(&SignerLink{OrgID: a.OrgID, AgreementID: a.ID, RecipientID: r.ID, TokenHash: hashToken(token), ExpiresAt: until}).Error
}

// Signing is everything one browser's session stands on.
type Signing struct {
	Session   SignerSession
	Link      SignerLink
	Recipient AgreementRecipient
	Agreement Agreement
	Org       Org
}

func (s *Store) signingFor(sess SignerSession) (*Signing, error) {
	g := Signing{Session: sess}
	if err := s.db.First(&g.Link, sess.LinkID).Error; err != nil || time.Now().After(g.Link.ExpiresAt) || g.Link.Failures >= linkFailures {
		return nil, errNoSigning
	}
	if err := s.db.Where("org_id = ? and id = ?", g.Link.OrgID, g.Link.RecipientID).First(&g.Recipient).Error; err != nil || g.Recipient.Erased {
		return nil, errNoSigning
	}
	if err := s.db.Where("org_id = ? and id = ?", g.Link.OrgID, g.Link.AgreementID).First(&g.Agreement).Error; err != nil {
		return nil, errNoSigning
	}
	if err := s.db.First(&g.Org, g.Link.OrgID).Error; err != nil {
		return nil, errNoSigning
	}
	return &g, nil
}

// OpenSigning starts a session in a browser from a mailed link. Opening it does nothing
// else: mail scanners open links too.
func (s *Store) OpenSigning(link string) (string, *Signing, error) {
	var l SignerLink
	if err := s.db.Where("token_hash = ?", hashToken(link)).First(&l).Error; err != nil {
		return "", nil, errNoSigning
	}
	token := randomToken(24)
	sess := SignerSession{OrgID: l.OrgID, LinkID: l.ID, TokenHash: hashToken(token), ExpiresAt: time.Now().Add(sessionUnproven)}
	g, err := s.signingFor(sess)
	if err != nil {
		return "", nil, err
	}
	if err := s.db.Create(&sess).Error; err != nil {
		return "", nil, err
	}
	g.Session = sess
	return token, g, nil
}

// SigningBySession is the session a browser's cookie names, if it hasn't run out.
func (s *Store) SigningBySession(token string) (*Signing, error) {
	if token == "" {
		return nil, errNoSigning
	}
	var sess SignerSession
	if err := s.db.Where("token_hash = ? and expires_at > ?", hashToken(token), time.Now()).First(&sess).Error; err != nil {
		return nil, errNoSigning
	}
	return s.signingFor(sess)
}

// SigningCode makes a code for a session to give back, and says where to mail it. A
// session has one live code; codes go a minute apart at most, and ten a day to a person.
func (s *Store) SigningCode(g *Signing, token, ip, browser string) (code string, err error) {
	now := time.Now()
	if g.Session.CodeSentAt != nil && now.Sub(*g.Session.CodeSentAt) < codeGap {
		return "", errors.New("a code went a moment ago: give it a minute, then ask again")
	}
	var today int64
	if err := s.db.Model(&AgreementEvent{}).Where("org_id = ? and agreement_id = ? and recipient_id = ? and kind = ? and at > ?",
		g.Org.ID, g.Agreement.ID, g.Recipient.ID, EventCodeSent, now.Add(-24*time.Hour)).Count(&today).Error; err != nil {
		return "", err
	}
	if today >= codesADay {
		return "", errors.New("that's as many codes as can go in a day: try again tomorrow")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	code = fmt.Sprintf("%06d", n.Int64())
	expires := now.Add(codeLife)
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&SignerSession{}).Where("id = ?", g.Session.ID).Updates(map[string]any{
			"code_hash": codeHash(token, code), "code_expires": expires, "code_tries": 0, "code_sent_at": now, "opened": true,
		}).Error; err != nil {
			return err
		}
		rid := g.Recipient.ID
		if !g.Session.Opened {
			if _, err := recordAgreementEvent(tx, AgreementEvent{OrgID: g.Org.ID, AgreementID: g.Agreement.ID, Round: g.Agreement.Round, Kind: EventOpened,
				RecipientID: &rid, Detail: "Link opened by " + g.Recipient.SentName, IP: ip, UserAgent: browser}); err != nil {
				return err
			}
		}
		_, err := recordAgreementEvent(tx, AgreementEvent{OrgID: g.Org.ID, AgreementID: g.Agreement.ID, Round: g.Agreement.Round, Kind: EventCodeSent,
			RecipientID: &rid, Detail: "Code emailed to " + g.Recipient.SentEmail, IP: ip, UserAgent: browser})
		return err
	})
	return code, err
}

// codeHash keys a code by its session's secret, which is only ever stored hashed itself:
// six digits alone would be found from their hash in moments.
func codeHash(session, code string) string { return hashToken(session + ":" + code) }

// ProveSigning checks a code a browser gives back. Five wrong tries end that code;
// thirty, all told, end the link.
func (s *Store) ProveSigning(g *Signing, token, code, ip, browser string) error {
	code = strings.TrimSpace(code)
	sess := g.Session
	if sess.CodeHash == "" || sess.CodeExpires == nil || time.Now().After(*sess.CodeExpires) || sess.CodeTries >= codeTries {
		return errors.New("that code has run out: ask for a new one")
	}
	rid := g.Recipient.ID
	if subtle.ConstantTimeCompare([]byte(codeHash(token, code)), []byte(sess.CodeHash)) != 1 {
		return errors.Join(s.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&SignerSession{}).Where("id = ?", sess.ID).Update("code_tries", gorm.Expr("code_tries + 1")).Error; err != nil {
				return err
			}
			return tx.Model(&SignerLink{}).Where("id = ?", g.Link.ID).Update("failures", gorm.Expr("failures + 1")).Error
		}), errors.New("that isn't the code: check the latest email"))
	}
	until := time.Now().Add(sessionProven)
	if g.Agreement.ExpiresAt != nil && g.Agreement.Status == AgreementSent && g.Agreement.ExpiresAt.Before(until) {
		until = *g.Agreement.ExpiresAt
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&SignerSession{}).Where("id = ? and code_hash = ?", sess.ID, sess.CodeHash).
			Updates(map[string]any{"proven": true, "code_hash": "", "expires_at": until})
		if res.Error != nil || res.RowsAffected == 0 {
			return errors.Join(res.Error, errors.New("that code has been used"))
		}
		_, err := recordAgreementEvent(tx, AgreementEvent{OrgID: g.Org.ID, AgreementID: g.Agreement.ID, Round: g.Agreement.Round, Kind: EventCodeEntered,
			RecipientID: &rid, Detail: g.Recipient.SentName + " entered the emailed code", IP: ip, UserAgent: browser})
		return err
	})
}

// proven is a session that has given its code back.
func proven(g *Signing) error {
	if !g.Session.Proven {
		return errors.New("enter the code we emailed you first")
	}
	return nil
}

// SignerSigns is an outsider signing through their proven session.
func (s *Store) SignerSigns(g *Signing, in SignIn, from Who) (*Agreement, error) {
	if err := proven(g); err != nil {
		return nil, err
	}
	return s.sign(g.Org.ID, g.Agreement.ID, g.Recipient.ID, in, Who{IP: from.IP, Browser: from.Browser, Via: "typed name · email code", Actor: g.Recipient.SentName})
}

// SignerDeclines is an outsider declining through their proven session.
func (s *Store) SignerDeclines(g *Signing, reason string, from Who) (*Agreement, error) {
	if err := proven(g); err != nil {
		return nil, err
	}
	return s.decline(g.Org.ID, g.Agreement.ID, g.Recipient.ID, reason, Who{IP: from.IP, Browser: from.Browser, Actor: g.Recipient.SentName})
}

// SignerDocument is the document as sent, for a proven session: the first time from a
// browser is recorded.
func (s *Store) SignerDocument(g *Signing, from Who) (*Agreement, []byte, error) {
	if err := proven(g); err != nil {
		return nil, nil, err
	}
	a, data, err := s.AgreementDocument(g.Org.ID, g.Agreement.ID)
	if err != nil || g.Session.Downloaded {
		return a, data, err
	}
	rid := g.Recipient.ID
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&SignerSession{}).Where("id = ?", g.Session.ID).Update("downloaded", true).Error; err != nil {
			return err
		}
		_, err := recordAgreementEvent(tx, AgreementEvent{OrgID: g.Org.ID, AgreementID: g.Agreement.ID, Round: g.Agreement.Round, Kind: EventDownloaded,
			RecipientID: &rid, Detail: "Document opened by " + g.Recipient.SentName, IP: from.IP, UserAgent: from.Browser})
		return err
	})
	return a, data, err
}

// SealedDocument is a completed agreement's sealed PDF.
func (s *Store) SealedDocument(org, id int64) (*Agreement, []byte, error) {
	a, err := s.Agreement(org, id)
	if err != nil {
		return nil, nil, err
	}
	if a.Status != AgreementCompleted {
		return nil, nil, noSuch("agreement %d hasn't been sealed", id)
	}
	var sealedCopy AgreementSealed
	if err := s.db.Where("org_id = ? and agreement_id = ?", org, id).First(&sealedCopy).Error; err != nil {
		return nil, nil, missing(err, "agreement %d has no sealed copy", id)
	}
	if hashBytes(sealedCopy.Data) != a.SealedHash {
		return nil, nil, fmt.Errorf("agreement %d's sealed copy: %w", id, errTampered)
	}
	return a, sealedCopy.Data, nil
}

// masked is an address with most of its local part hidden, to show whom a code went to.
func masked(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" {
		return email
	}
	r := []rune(local)
	return string(r[0]) + strings.Repeat("•", max(len(r)-1, 2)) + "@" + domain
}
