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
	// Codes is how many codes have been mailed from this link on the day CodesOn: a link
	// sent again starts afresh, so someone who wastes one link's codes can't lock its signer out.
	Codes     int    `gorm:"not null;default:0"`
	CodesOn   string `gorm:"not null;default:''"`
	CreatedAt time.Time
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
	if err := s.db.First(&g.Link, sess.LinkID).Error; err != nil || time.Now().After(g.Link.ExpiresAt) {
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
func (s *Store) OpenSigning(link, current string) (string, *Signing, error) {
	var l SignerLink
	if err := s.db.Where("token_hash = ?", hashToken(link)).First(&l).Error; err != nil {
		return "", nil, errNoSigning
	}
	// A browser that has already given its code for this person keeps its session: opening
	// the link again doesn't make them ask for another.
	if g, err := s.SigningBySession(current); err == nil && g.Session.Proven && g.Link.RecipientID == l.RecipientID {
		return current, g, nil
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
func (s *Store) SigningCode(g *Signing, token, ip, browser string, send func(to, code string) error) (code string, err error) {
	if err := codesFor(g); err != nil {
		return "", err
	}
	now := time.Now()
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	code = fmt.Sprintf("%06d", n.Int64())
	// The minute between codes and the day's ten are claimed before a code is made, each
	// by an update that only one of two requests at once can make.
	res := s.db.Model(&SignerSession{}).Where("id = ? and (code_sent_at is null or code_sent_at < ?)", g.Session.ID, now.Add(-codeGap)).Updates(map[string]any{
		"code_hash": codeHash(token, code), "code_expires": now.Add(codeLife), "code_tries": 0, "code_sent_at": now,
	})
	if res.Error != nil {
		return "", res.Error
	}
	if res.RowsAffected == 0 {
		return "", errors.New("a code went a moment ago: give it a minute, then ask again")
	}
	day := now.UTC().Format(time.DateOnly)
	res = s.db.Model(&SignerLink{}).Where("id = ? and not (codes_on = ? and codes >= ?)", g.Link.ID, day, codesADay).
		Updates(map[string]any{"codes": gorm.Expr("case when codes_on = ? then codes + 1 else 1 end", day), "codes_on": day})
	if res.Error != nil || res.RowsAffected == 0 {
		_ = logged(s.db.Model(&SignerSession{}).Where("id = ?", g.Session.ID).Update("code_hash", ""))
		return "", errors.Join(res.Error, errors.New("that's as many codes as can go in a day: try again tomorrow"))
	}
	// The record says a code went only once the mail server has taken it. One that
	// didn't go leaves nothing to enter, and asking again isn't held back.
	if send != nil {
		if err := send(g.Recipient.SentEmail, code); err != nil {
			_ = logged(s.db.Model(&SignerSession{}).Where("id = ?", g.Session.ID).Updates(map[string]any{"code_hash": "", "code_sent_at": nil}))
			return "", fmt.Errorf("the code couldn't be emailed just now: try again in a moment (%w)", err)
		}
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&SignerSession{}).Where("id = ?", g.Session.ID).Update("opened", true).Error; err != nil {
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

// ProveSigning checks a code a browser gives back. Five wrong tries end that code; with
// ten codes a day from a link, that is fifty guesses a day at a million codes. Wrong
// guesses never end the link itself, which would let whoever had a forwarded copy of it
// lock out the person it was sent to.
func (s *Store) ProveSigning(g *Signing, token, code, ip, browser string) error {
	if err := codesFor(g); err != nil {
		return err
	}
	code = strings.TrimSpace(code)
	sess := g.Session
	// A try is claimed before the code is compared, so tries made at once still count
	// one each: the sixth finds none left, whichever arrived first.
	res := s.db.Model(&SignerSession{}).Where("id = ? and code_hash <> '' and code_expires > ? and code_tries < ?", sess.ID, time.Now(), codeTries).
		Update("code_tries", gorm.Expr("code_tries + 1"))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("that code has run out: ask for a new one")
	}
	if err := s.db.Where("id = ?", sess.ID).First(&sess).Error; err != nil {
		return err
	}
	rid := g.Recipient.ID
	if subtle.ConstantTimeCompare([]byte(codeHash(token, code)), []byte(sess.CodeHash)) != 1 {
		return errors.New("that isn't the code: check the latest email")
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

// codesFor is whether a code may be asked for or given back at all: only while the
// agreement can still be signed, or once it is sealed, for its copy. A withdrawn,
// declined or expired agreement opens nothing more.
func codesFor(g *Signing) error {
	switch g.Agreement.Status {
	case AgreementSent:
		if g.Agreement.ExpiresAt != nil && time.Now().After(*g.Agreement.ExpiresAt) {
			return errExpiredToSig
		}
		return nil
	case AgreementSealing, AgreementCompleted:
		return nil
	}
	return errNoSigning
}

// proven is a session that has given its code back.
func proven(g *Signing) error {
	if !g.Session.Proven {
		return errors.New("enter the code we emailed you first")
	}
	return codesFor(g)
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

// signingView is who signs a session's agreement, in order, and the places on it that
// are the session's signer's own.
func (s *Store) signingView(g *Signing) (signers []AgreementRecipient, fields []AgreementField, err error) {
	if err := s.in(g.Org.ID).Where("agreement_id = ? and role = ?", g.Agreement.ID, RecipientSigner).Order("position, id").Find(&signers).Error; err != nil {
		return nil, nil, err
	}
	err = s.in(g.Org.ID).Where("agreement_id = ? and round = ? and recipient_id = ?", g.Agreement.ID, g.Agreement.Round, g.Recipient.ID).Order("page, y, x, id").Find(&fields).Error
	return signers, fields, err
}
