package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

// EnquiryMessage is one turn in answering an enquiry: a reply we emailed from here, or
// what they said back, which someone pasted in because their answer arrives in a
// mailbox and not here. It belongs to its enquiry and goes wherever that goes.
type EnquiryMessage struct {
	ID        int64 `gorm:"primaryKey" json:"id"`
	OrgID     int64 `gorm:"index;not null" json:"-"`
	EnquiryID int64 `gorm:"index;not null" json:"enquiry_id"`
	// Ours is true for a reply we sent, false for what they said.
	Ours bool   `gorm:"not null;default:false" json:"ours"`
	Text string `gorm:"not null;default:''" json:"text"`
	By   string `gorm:"not null;default:''" json:"by"`
	// SentAt is when a reply of ours left. One still without it was never sent, and is
	// not part of the thread.
	SentAt    *time.Time `json:"sent_at"`
	CreatedAt time.Time  `json:"created_at"`
}

const (
	replyLimit = 10000
	// repliesUnanswered is how many replies can go to one enquiry with no answer recorded between.
	repliesUnanswered = 3
	// replyGap is how long the same words to the same enquiry count as a second press
	// of the button and not a second reply.
	replyGap = 2 * time.Minute
	// replyStale is when a reply that never got its SentAt is given up on.
	replyStale = 10 * time.Minute
)

// notSent is a failure of the mail server, not of what was asked.
type notSent struct{ error }

func (e notSent) Unwrap() error { return e.error }

var inThread = "(ours = false or sent_at is not null)"

func threadText(text string) (string, error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	switch {
	case text == "":
		return "", errors.New("write something first")
	case utf8.RuneCountInString(text) > replyLimit:
		return "", fmt.Errorf("that is longer than %d characters", replyLimit)
	}
	return text, nil
}

// EnquiryThread is what has been said in answering one enquiry, oldest first.
func (s *Store) EnquiryThread(org, enquiry int64) ([]EnquiryMessage, error) {
	if err := s.in(org).Where("id = ?", enquiry).First(&Enquiry{}).Error; err != nil {
		return nil, missing(err, "no enquiry %d", enquiry)
	}
	threads, err := s.EnquiryThreads(org, []int64{enquiry})
	if threads[enquiry] == nil {
		return []EnquiryMessage{}, err
	}
	return threads[enquiry], err
}

func (s *Store) EnquiryThreads(org int64, enquiries []int64) (map[int64][]EnquiryMessage, error) {
	out := map[int64][]EnquiryMessage{}
	if len(enquiries) == 0 {
		return out, nil
	}
	var all []EnquiryMessage
	if err := s.in(org).Where("enquiry_id in ?", enquiries).Where(inThread).Order("id").Find(&all).Error; err != nil {
		return nil, err
	}
	for _, m := range all {
		out[m.EnquiryID] = append(out[m.EnquiryID], m)
	}
	return out, nil
}

// answerable is whether an enquiry can be written back to: it wasn't spam, it gave one
// address, and that address isn't on the never-contact list.
func answerable(o *Org, e Enquiry) (string, error) {
	if e.Spam {
		return "", errors.New("that was marked as spam, so there is nobody to answer")
	}
	to, err := mail.ParseAddress(strings.TrimSpace(e.Email))
	if err != nil {
		return "", errors.New("it gave no single email address to answer")
	}
	if err := o.Settings.listed(to.Address, ""); err != nil {
		return "", err
	}
	return to.Address, nil
}

// StartReply keeps a reply before it is sent, so two presses of the button are one
// reply. It is not in the thread until settleReply says it went.
func (s *Store) StartReply(o *Org, enquiry int64, text, actor string) (*EnquiryMessage, string, error) {
	text, err := threadText(text)
	if err != nil {
		return nil, "", err
	}
	var m *EnquiryMessage
	var to string
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := hold(tx, &Enquiry{}, o.ID, enquiry); err != nil {
			return err
		}
		var e Enquiry
		if err := tx.Where("org_id = ? and id = ?", o.ID, enquiry).First(&e).Error; err != nil {
			return missing(err, "no enquiry %d", enquiry)
		}
		if to, err = answerable(o, e); err != nil {
			return err
		}
		if err := erased(tx, o.ID, "", "", to, ""); err != nil {
			return errors.New("that address belongs to someone who was erased on request, so nothing is sent to it")
		}
		// Replies in a row with no answer between: a card pressed again and again, or a
		// key in a loop, shouldn't be able to fill someone's inbox from here.
		var last EnquiryMessage
		heard := tx.Where("org_id = ? and enquiry_id = ? and ours = ?", o.ID, enquiry, false).Order("id desc").Limit(1).Find(&last)
		if heard.Error != nil {
			return heard.Error
		}
		var unanswered int64
		if err := tx.Model(&EnquiryMessage{}).Where("org_id = ? and enquiry_id = ? and ours = ? and id > ? and (sent_at is not null or created_at > ?)", o.ID, enquiry, true, last.ID, time.Now().Add(-replyStale)).Count(&unanswered).Error; err != nil {
			return err
		}
		if unanswered >= repliesUnanswered {
			return fmt.Errorf("%d replies have gone with no answer recorded; record what they said, or leave it there", repliesUnanswered)
		}
		now := time.Now()
		mine := tx.Where("org_id = ? and enquiry_id = ? and ours = ?", o.ID, enquiry, true)
		if err := mine.Where("sent_at is null and created_at < ?", now.Add(-replyStale)).Delete(&EnquiryMessage{}).Error; err != nil {
			return err
		}
		var same int64
		if err := tx.Model(&EnquiryMessage{}).Where("org_id = ? and enquiry_id = ? and ours = ? and text = ? and created_at > ?", o.ID, enquiry, true, text, now.Add(-replyGap)).Count(&same).Error; err != nil {
			return err
		}
		if same > 0 {
			return errors.New("that reply has just been sent; change it to send another")
		}
		m = &EnquiryMessage{OrgID: o.ID, EnquiryID: enquiry, Ours: true, Text: text, By: actor, CreatedAt: now}
		return tx.Create(m).Error
	})
	return m, to, err
}

// SettleReply records how sending went. A reply that went joins the thread, and the
// enquiry waits on them, with an owner if it had none. One that didn't is forgotten,
// and the enquiry stays as it was.
func (s *Store) SettleReply(org int64, m *EnquiryMessage, sent bool, actor string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if !sent {
			return tx.Where("org_id = ? and id = ? and sent_at is null", org, m.ID).Delete(&EnquiryMessage{}).Error
		}
		var e Enquiry
		if err := tx.Where("org_id = ? and id = ?", org, m.EnquiryID).First(&e).Error; err != nil {
			// Deleted while the mail was on its way: what was sent has nothing to belong to.
			return tx.Where("org_id = ? and id = ?", org, m.ID).Delete(&EnquiryMessage{}).Error
		}
		now := time.Now()
		m.SentAt = &now
		if err := tx.Model(&EnquiryMessage{}).Where("org_id = ? and id = ?", org, m.ID).Update("sent_at", now).Error; err != nil {
			return err
		}
		updates := map[string]any{"state": EnquiryWaiting}
		if e.Owner == "" && !strings.HasPrefix(actor, "agent:") {
			updates["owner"] = actor
		}
		if err := tx.Model(&Enquiry{}).Where("org_id = ? and id = ?", org, e.ID).Updates(updates).Error; err != nil {
			return err
		}
		return s.logEnquiry(tx, e, actor, "enquiry:reply")
	})
}

// logEnquiry puts a turn in the history of whoever wrote in, when they are held here.
// It says that it happened, not what was said: the words are in the thread.
func (s *Store) logEnquiry(tx *gorm.DB, e Enquiry, actor, action string) error {
	if e.ContactID == nil && e.CompanyID == nil {
		return nil
	}
	var contact int64
	if e.ContactID != nil {
		contact = *e.ContactID
	}
	return s.log(tx, e.OrgID, contact, e.CompanyID, actor, action, nil)
}

// HearEnquiry records what they said back, pasted in by whoever read it. Nothing is
// sent. The enquiry is ours to answer again.
func (s *Store) HearEnquiry(org, enquiry int64, text, actor string) (*EnquiryMessage, error) {
	text, err := threadText(text)
	if err != nil {
		return nil, err
	}
	var m *EnquiryMessage
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var e Enquiry
		if err := tx.Where("org_id = ? and id = ?", org, enquiry).First(&e).Error; err != nil {
			return missing(err, "no enquiry %d", enquiry)
		}
		if e.Spam {
			return errors.New("that was marked as spam, so there is nothing to follow")
		}
		m = &EnquiryMessage{OrgID: org, EnquiryID: enquiry, Text: text, By: actor, CreatedAt: time.Now()}
		if err := tx.Create(m).Error; err != nil {
			return err
		}
		if err := tx.Model(&Enquiry{}).Where("org_id = ? and id = ?", org, enquiry).Update("state", EnquiryOpen).Error; err != nil {
			return err
		}
		return s.logEnquiry(tx, e, actor, "enquiry:heard")
	})
	return m, err
}

var errNoReplyMail = errors.New("mail isn't set up, so a reply can't be sent from here: an owner sets a mail server in Settings")

// replyEnquiry emails a reply to whoever wrote in, at the address they gave and no
// other. The mail carries only what was typed for it: nothing they sent us is quoted
// back, so a form filled in with someone else's address can't be used to send that
// person words of the sender's choosing. answers is where their answer should go.
func (a *App) replyEnquiry(ctx context.Context, o *Org, enquiry int64, text, actor, answers string) (*EnquiryMessage, error) {
	via, err := a.mailFor(o.ID)
	if err != nil {
		return nil, err
	}
	if via == nil {
		return nil, errNoReplyMail
	}
	m, to, err := a.s.StartReply(o, enquiry, text, actor)
	if err != nil {
		return nil, err
	}
	sendCtx, cancel := context.WithTimeout(ctx, sendLimit)
	defer cancel()
	sendErr := a.deliver(sendCtx, *via, Message{To: to, ReplyTo: answers, Subject: "Your message to " + o.Name, Body: m.Text + "\n"})
	if err := a.s.SettleReply(o.ID, m, sendErr == nil, actor); err != nil {
		return nil, err
	}
	if sendErr != nil {
		// The mail server's own words name hosts and addresses, some of them the host's operator's.
		log.Printf("a reply to enquiry %d wasn't sent: %v", enquiry, sendErr)
		return nil, notSent{errors.New("the mail server didn't take the reply, and nothing was recorded; try again, or ask an owner to check the mail settings")}
	}
	return m, nil
}

func enquiryIDs(rows []Enquiry) []int64 {
	ids := make([]int64, 0, len(rows))
	for _, e := range rows {
		ids = append(ids, e.ID)
	}
	return ids
}
