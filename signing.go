package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
)

// Sending an agreement and having it signed. Every change of state is a conditional
// update on the state it leaves, inside a transaction that first locks the agreement's
// row, so two signers, a void and the sealing loop can't step on each other.

const (
	SignerWaiting  = "waiting"
	SignerDue      = "due"
	SignerSigned   = "signed"
	SignerDeclined = "declined"

	agreementDaysDefault = 14
	agreementDaysMost    = 60
)

var (
	errNotSent      = errors.New("this agreement isn't out for signature")
	errNotYourTurn  = errors.New("it isn't your turn to sign this agreement yet")
	errAlreadyDone  = errors.New("you have already signed or declined this agreement")
	errExpiredToSig = errors.New("the time to sign this agreement has run out")
)

// AgreementSealed is a sealed agreement's PDF, kept apart so lists never carry it.
type AgreementSealed struct {
	AgreementID int64  `gorm:"primaryKey;autoIncrement:false"`
	OrgID       int64  `gorm:"index;not null"`
	Data        []byte `gorm:"not null;default:''"`
}

// lockAgreement takes the agreement's row for the rest of the transaction: Postgres
// holds it until commit, and SQLite serialises every write anyway.
func lockAgreement(tx *gorm.DB, org, id int64) (*Agreement, error) {
	res := tx.Exec("update agreements set updated_at = updated_at where org_id = ? and id = ?", org, id)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, noSuch("no agreement %d", id)
	}
	var a Agreement
	return &a, tx.Where("org_id = ? and id = ?", org, id).First(&a).Error
}

// SendIn is what sending says to the signers.
type SendIn struct {
	Subject, Message string
	Days             int
}

// sendable is why a draft can't go out: anything missing from it, anyone on it who
// can't be mailed, any text the seal couldn't draw.
func (s *Store) sendable(o *Org, a *Agreement) error {
	missingParts, err := s.Readiness(o.ID, *a)
	if err != nil {
		return err
	}
	if len(missingParts) > 0 {
		return errors.New(strings.Join(missingParts, " "))
	}
	for _, text := range []string{o.Name, a.Name} {
		if err := drawable(text); err != nil {
			return err
		}
	}
	parties, err := s.AgreementRecipients(o.ID, a.ID)
	if err != nil {
		return err
	}
	for _, r := range parties {
		if r.ContactID != nil {
			var c Contact
			if err := s.in(o.ID).Where("id = ?", *r.ContactID).First(&c).Error; err != nil {
				return missing(err, "no contact %d", *r.ContactID)
			}
			if err := signable(o, a.CompanyID, c); err != nil {
				return fmt.Errorf("%s: %w", r.Name, err)
			}
		}
		if err := drawable(r.Name); err != nil {
			return err
		}
	}
	return s.TrySeal(o, a.ID)
}

// SendAgreement sends a draft for signature: who it goes to is frozen, the first signer
// (or every signer, when they sign in any order) is due, and their mail is queued.
func (s *Store) SendAgreement(o *Org, id int64, in SendIn, user *User, actor string) (*Agreement, error) {
	a, err := s.Agreement(o.ID, id)
	if err != nil {
		return nil, err
	}
	if a.Status != AgreementDraft {
		return nil, errNotDraft
	}
	if in.Days == 0 {
		in.Days = agreementDaysDefault
	}
	if in.Days < 1 || in.Days > agreementDaysMost {
		return nil, fmt.Errorf("an agreement can be open for signing for 1 to %d days", agreementDaysMost)
	}
	in.Subject = strings.Join(strings.Fields(in.Subject), " ")
	if in.Subject == "" {
		in.Subject = "Please sign: " + a.Name
	}
	if utf8.RuneCountInString(in.Subject) > 200 || utf8.RuneCountInString(in.Message) > 2000 {
		return nil, errors.New("a subject is 200 characters at most and a message 2,000")
	}
	if err := s.sendable(o, a); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	ends := now.AddDate(0, 0, in.Days)
	var userID int64
	if user != nil {
		userID = user.ID
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		a, err = lockAgreement(tx, o.ID, id)
		if err != nil {
			return err
		}
		if a.Status != AgreementDraft {
			return errNotDraft
		}
		parties, err := s.recipientsIn(tx, o.ID, id)
		if err != nil {
			return err
		}
		first := true
		for _, p := range parties {
			status := ""
			if p.Role == RecipientSigner {
				status = SignerWaiting
				if !a.InOrder || first {
					status = SignerDue
				}
				first = false
			}
			if err := tx.Model(&AgreementRecipient{}).Where("id = ?", p.ID).
				Updates(map[string]any{"sent_name": p.Name, "sent_email": p.Email, "status": status}).Error; err != nil {
				return err
			}
			if status == SignerDue {
				if err := queueAgreementMail(tx, *a, p.AgreementRecipient, mailLink); err != nil {
					return err
				}
			}
		}
		if err := tx.Model(&Agreement{}).Where("id = ? and status = ?", id, AgreementDraft).Updates(map[string]any{
			"status": AgreementSent, "sent_at": now, "sent_by": actor, "sent_by_user": userID, "sent_org": o.Name,
			"expires_at": ends, "subject": in.Subject, "message": strings.TrimSpace(in.Message), "updated_by": actor,
		}).Error; err != nil {
			return err
		}
		_, err = recordAgreementEvent(tx, AgreementEvent{OrgID: o.ID, AgreementID: id, Round: a.Round, At: now, Kind: EventSent, Actor: actor,
			Detail: fmt.Sprintf("Sent by %s to %d signers, open until %s", actor, countSigners(parties), ends.Format("2 Jan 2006"))})
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.Agreement(o.ID, id)
}

func countSigners(ps []Party) int {
	n := 0
	for _, p := range ps {
		if p.Role == RecipientSigner {
			n++
		}
	}
	return n
}

// recipientsIn are an agreement's recipients with who they are, read inside a transaction.
func (s *Store) recipientsIn(tx *gorm.DB, org, id int64) ([]Party, error) {
	out := []Party{}
	err := tx.Table("agreement_recipients").
		Select("agreement_recipients.*, coalesce(users.name, contacts.person, '') as name, coalesce(users.email, contacts.email, '') as email").
		Joins("left join contacts on contacts.id = agreement_recipients.contact_id and contacts.org_id = agreement_recipients.org_id").
		Joins("left join users on users.id = agreement_recipients.user_id").
		Where("agreement_recipients.org_id = ? and agreement_recipients.agreement_id = ?", org, id).
		Order("case when agreement_recipients.role = 'signer' then 0 else 1 end, agreement_recipients.position, agreement_recipients.id").
		Scan(&out).Error
	for i := range out {
		out[i].Email = strings.TrimSpace(out[i].Email)
	}
	return out, err
}

// SignIn is what a signer gives when they sign: the name they type, that they agree to
// sign electronically, and what they filled in, by field.
type SignIn struct {
	Typed   string
	Consent bool
	Values  map[int64]string
}

// Who is where a signature or a decline came from, for the record.
type Who struct {
	IP, Browser, Via, Actor string
}

// sign records one recipient's signature on a sent agreement, and moves it on: the next
// signer becomes due, or, with nobody left, it goes to be sealed.
func (s *Store) sign(org, id, recipient int64, in SignIn, who Who) (*Agreement, error) {
	typed := strings.Join(strings.Fields(in.Typed), " ")
	switch {
	case !in.Consent:
		return nil, errors.New("tick to agree to sign electronically first")
	case typed == "":
		return nil, errors.New("type your full name to sign")
	case utf8.RuneCountInString(typed) > 120:
		return nil, errors.New("a name is 120 characters at most")
	}
	if err := drawable(typed); err != nil {
		return nil, fmt.Errorf("your name: %w", err)
	}
	var a *Agreement
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		if a, err = lockAgreement(tx, org, id); err != nil {
			return err
		}
		if err := openToSign(a); err != nil {
			return err
		}
		var r AgreementRecipient
		if err := tx.Where("org_id = ? and agreement_id = ? and id = ?", org, id, recipient).First(&r).Error; err != nil {
			return missing(err, "no recipient %d on agreement %d", recipient, id)
		}
		switch r.Status {
		case SignerDue:
		case SignerWaiting:
			return errNotYourTurn
		default:
			return errAlreadyDone
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		var fields []AgreementField
		if err := tx.Where("org_id = ? and agreement_id = ? and round = ? and recipient_id = ?", org, id, a.Round, r.ID).Find(&fields).Error; err != nil {
			return err
		}
		for _, f := range fields {
			value, err := fieldValue(f, typed, in.Values[f.ID], now)
			if err != nil {
				return err
			}
			if err := tx.Model(&AgreementField{}).Where("id = ?", f.ID).Update("value", value).Error; err != nil {
				return err
			}
		}
		res := tx.Model(&AgreementRecipient{}).Where("id = ? and status = ?", r.ID, SignerDue).
			Updates(map[string]any{"status": SignerSigned, "signed_at": now, "typed_name": typed, "signed_via": who.Via})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errAlreadyDone
		}
		rid := r.ID
		for _, e := range []AgreementEvent{
			{Kind: EventConsented, Detail: r.SentName + " agreed to sign electronically, in the wording below"},
			{Kind: EventSigned, Detail: fmt.Sprintf("Signed by %s as %q", r.SentName, typed)},
		} {
			e.OrgID, e.AgreementID, e.Round, e.At, e.Actor, e.RecipientID, e.IP, e.UserAgent = org, id, a.Round, now, who.Actor, &rid, who.IP, who.Browser
			if _, err := recordAgreementEvent(tx, e); err != nil {
				return err
			}
		}
		return s.moveOn(tx, a)
	})
	return a, err
}

// moveOn makes the next signer due once the one before has signed, or sends the
// agreement to be sealed once nobody is left to sign.
func (s *Store) moveOn(tx *gorm.DB, a *Agreement) error {
	var rest []AgreementRecipient
	if err := tx.Where("org_id = ? and agreement_id = ? and role = ? and status in ?", a.OrgID, a.ID, RecipientSigner, []string{SignerWaiting, SignerDue}).
		Order("position, id").Find(&rest).Error; err != nil {
		return err
	}
	if len(rest) == 0 {
		return tx.Model(&Agreement{}).Where("id = ? and status = ?", a.ID, AgreementSent).Update("status", AgreementSealing).Error
	}
	if slices.ContainsFunc(rest, func(r AgreementRecipient) bool { return r.Status == SignerDue }) {
		return nil
	}
	next := rest[0]
	if err := tx.Model(&AgreementRecipient{}).Where("id = ? and status = ?", next.ID, SignerWaiting).Update("status", SignerDue).Error; err != nil {
		return err
	}
	return queueAgreementMail(tx, *a, next, mailLink)
}

// openToSign is whether an agreement can be signed now.
func openToSign(a *Agreement) error {
	if a.Status != AgreementSent {
		return errNotSent
	}
	if a.ExpiresAt != nil && time.Now().After(*a.ExpiresAt) {
		return errExpiredToSig
	}
	return nil
}

// fieldValue is what a field shows once its signer signs: their typed name in a
// signature, its initials, the day, or what they filled in.
func fieldValue(f AgreementField, typed, given string, now time.Time) (string, error) {
	switch f.Kind {
	case "signature", "name":
		return typed, nil
	case "initials":
		return initialsOf(typed), nil
	case "date":
		return now.Format("2 Jan 2006"), nil
	case "tick":
		if given == "" {
			return "", nil
		}
		return "✓", nil
	}
	given = strings.Join(strings.Fields(given), " ")
	switch {
	case given == "":
		return "", fmt.Errorf("fill in %q", orElse(f.Label, "the text field"))
	case utf8.RuneCountInString(given) > 200:
		return "", fmt.Errorf("%q is 200 characters at most", orElse(f.Label, "a text field"))
	}
	if err := drawable(given); err != nil {
		return "", err
	}
	return given, nil
}

func initialsOf(name string) string {
	var out []rune
	for _, word := range strings.Fields(name) {
		r, _ := utf8.DecodeRuneInString(word)
		if unicode.IsLetter(r) {
			out = append(out, unicode.ToUpper(r))
		}
	}
	return string(out)
}

// decline records a signer turning an agreement down: it ends, and the sender hears.
func (s *Store) decline(org, id, recipient int64, reason string, who Who) (*Agreement, error) {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > 1000 {
		return nil, errors.New("a reason is 1,000 characters at most")
	}
	var a *Agreement
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		if a, err = lockAgreement(tx, org, id); err != nil {
			return err
		}
		if err := openToSign(a); err != nil {
			return err
		}
		res := tx.Model(&AgreementRecipient{}).Where("org_id = ? and agreement_id = ? and id = ? and status in ?", org, id, recipient, []string{SignerDue, SignerWaiting}).
			Updates(map[string]any{"status": SignerDeclined, "decline_reason": reason})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errAlreadyDone
		}
		if err := tx.Model(&Agreement{}).Where("id = ? and status = ?", id, AgreementSent).Update("status", AgreementDeclined).Error; err != nil {
			return err
		}
		var r AgreementRecipient
		if err := tx.First(&r, recipient).Error; err != nil {
			return err
		}
		rid := r.ID
		if _, err := recordAgreementEvent(tx, AgreementEvent{OrgID: org, AgreementID: id, Round: a.Round, Kind: EventDeclined, Actor: who.Actor, RecipientID: &rid,
			Detail: "Declined by " + r.SentName, IP: who.IP, UserAgent: who.Browser}); err != nil {
			return err
		}
		return queueAgreementMail(tx, *a, r, mailDeclined)
	})
	return a, err
}

// VoidAgreement withdraws a sent agreement: nobody can sign it, and those already sent
// it hear so.
func (s *Store) VoidAgreement(org, id int64, reason, actor string) (*Agreement, error) {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > 1000 {
		return nil, errors.New("a reason is 1,000 characters at most")
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		a, err := lockAgreement(tx, org, id)
		if err != nil {
			return err
		}
		switch a.Status {
		case AgreementSent:
		case AgreementSealing:
			return errors.New("everyone has signed: it is being sealed, and can't be withdrawn")
		default:
			return errNotSent
		}
		if err := tx.Model(&Agreement{}).Where("id = ? and status = ?", id, AgreementSent).Updates(map[string]any{"status": AgreementVoided, "updated_by": actor}).Error; err != nil {
			return err
		}
		detail := "Withdrawn by " + actor
		if reason != "" {
			detail += ": " + reason
		}
		if _, err := recordAgreementEvent(tx, AgreementEvent{OrgID: org, AgreementID: id, Round: a.Round, Kind: EventVoided, Actor: actor, Detail: detail}); err != nil {
			return err
		}
		return tellMailed(tx, *a, mailVoided)
	})
	if err != nil {
		return nil, err
	}
	return s.Agreement(org, id)
}

// tellMailed queues a mail of a kind to every signer already sent the agreement.
func tellMailed(tx *gorm.DB, a Agreement, kind string) error {
	var mailed []AgreementRecipient
	if err := tx.Where("org_id = ? and agreement_id = ? and role = ? and status <> ?", a.OrgID, a.ID, RecipientSigner, SignerWaiting).Find(&mailed).Error; err != nil {
		return err
	}
	for _, r := range mailed {
		if err := queueAgreementMail(tx, a, r, kind); err != nil {
			return err
		}
	}
	return nil
}

// MemberSign is a member signing their own place on an agreement, signed in: only the
// member the place is for, never another, an owner included.
func (s *Store) MemberSign(o *Org, id int64, u *User, in SignIn, from Who) (*Agreement, error) {
	var rec AgreementRecipient
	if err := s.in(o.ID).Where("agreement_id = ? and user_id = ? and role = ?", id, u.ID, RecipientSigner).First(&rec).Error; err != nil {
		return nil, missing(err, "you aren't one of this agreement's signers")
	}
	return s.sign(o.ID, id, rec.ID, in, Who{IP: from.IP, Browser: from.Browser, Via: "typed name · Prospector sign-in", Actor: u.Name})
}

// MemberDecline is a member declining their own place.
func (s *Store) MemberDecline(o *Org, id int64, u *User, reason string, from Who) (*Agreement, error) {
	var rec AgreementRecipient
	if err := s.in(o.ID).Where("agreement_id = ? and user_id = ? and role = ?", id, u.ID, RecipientSigner).First(&rec).Error; err != nil {
		return nil, missing(err, "you aren't one of this agreement's signers")
	}
	return s.decline(o.ID, id, rec.ID, reason, Who{IP: from.IP, Browser: from.Browser, Actor: u.Name})
}

// ResendLink queues a signer's link again, when the first didn't arrive.
func (s *Store) ResendLink(org, id, recipient int64, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		a, err := lockAgreement(tx, org, id)
		if err != nil {
			return err
		}
		if err := openToSign(a); err != nil {
			return err
		}
		var r AgreementRecipient
		if err := tx.Where("org_id = ? and agreement_id = ? and id = ? and status = ?", org, id, recipient, SignerDue).First(&r).Error; err != nil {
			return missing(err, "that person isn't due to sign this agreement")
		}
		var recent int64
		if err := tx.Model(&AgreementMail{}).Where("recipient_id = ? and kind = ? and created_at > ?", r.ID, mailLink, time.Now().Add(-5*time.Minute)).Count(&recent).Error; err != nil {
			return err
		}
		if recent > 0 {
			return errors.New("a link went to them in the last five minutes: give it a little longer")
		}
		return queueAgreementMail(tx, *a, r, mailLink)
	})
}

// expireAgreements ends agreements whose time to sign ran out.
func (s *Store) expireAgreements(now time.Time) error {
	var due []Agreement
	if err := s.db.Where("status = ? and expires_at < ?", AgreementSent, now).Find(&due).Error; err != nil {
		return err
	}
	for _, a := range due {
		err := s.db.Transaction(func(tx *gorm.DB) error {
			res := tx.Model(&Agreement{}).Where("id = ? and status = ?", a.ID, AgreementSent).Update("status", AgreementExpired)
			if res.Error != nil || res.RowsAffected == 0 {
				return res.Error
			}
			_, err := recordAgreementEvent(tx, AgreementEvent{OrgID: a.OrgID, AgreementID: a.ID, Round: a.Round, Kind: EventExpired, Detail: "The time to sign ran out"})
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// eraseFromAgreements marks someone erased on every agreement that went to them, which
// keeps their part as the agreement's record and out of exports. One still out for
// signature is withdrawn: it can't be completed without them.
func eraseFromAgreements(tx *gorm.DB, org, contact int64, actor string) error {
	var on []AgreementRecipient
	if err := tx.Where("org_id = ? and contact_id = ?", org, contact).Find(&on).Error; err != nil {
		return err
	}
	for _, r := range on {
		a, err := lockAgreement(tx, org, r.AgreementID)
		if _, gone := errors.AsType[notFound](err); gone {
			continue
		}
		if err != nil {
			return err
		}
		if a.Status == AgreementDraft {
			continue
		}
		if err := tx.Model(&AgreementRecipient{}).Where("id = ?", r.ID).Update("erased", true).Error; err != nil {
			return err
		}
		if a.Status != AgreementSent {
			continue
		}
		if err := tx.Model(&Agreement{}).Where("id = ? and status = ?", a.ID, AgreementSent).Update("status", AgreementVoided).Error; err != nil {
			return err
		}
		if _, err := recordAgreementEvent(tx, AgreementEvent{OrgID: org, AgreementID: a.ID, Round: a.Round, Kind: EventVoided, Actor: actor,
			Detail: "Withdrawn: someone on it was erased on their request"}); err != nil {
			return err
		}
		if err := tellMailed(tx, *a, mailVoided); err != nil {
			return err
		}
	}
	return nil
}

// clientIP is where a request came from: the connection's address, or what the proxy in
// front says when the connection is from the proxy itself, on this machine or the
// private network. A header from anywhere else is somebody's claim, not the address.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		if last := net.ParseIP(strings.TrimSpace(hops[len(hops)-1])); last != nil {
			return last.String()
		}
	}
	if ip == nil {
		return ""
	}
	return ip.String()
}

// browserOf is a browser's name and system in a few words, from what it says of itself:
// never the raw string, which anyone can make say anything.
func browserOf(r *http.Request) string {
	ua := r.UserAgent()
	name := "a browser"
	for _, b := range []struct{ token, name string }{{"Edg/", "Edge"}, {"OPR/", "Opera"}, {"Firefox/", "Firefox"}, {"Chrome/", "Chrome"}, {"Safari/", "Safari"}} {
		if strings.Contains(ua, b.token) {
			name = b.name
			break
		}
	}
	for _, os := range []struct{ token, name string }{{"iPhone", "iPhone"}, {"iPad", "iPad"}, {"Android", "Android"}, {"Mac OS X", "macOS"}, {"Windows", "Windows"}, {"Linux", "Linux"}} {
		if strings.Contains(ua, os.token) {
			return name + " on " + os.name
		}
	}
	return name
}
