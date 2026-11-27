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

	"github.com/shutthegoatup/prospector/internal/mailer"
)

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
	if err := o.Settings.Listed(to.Address, ""); err != nil {
		return "", err
	}
	return to.Address, nil
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
	sendCtx, cancel := context.WithTimeout(ctx, mailer.SendLimit)
	defer cancel()
	sendErr := a.deliver(sendCtx, *via, mailer.Message{To: to, ReplyTo: answers, Subject: "Your message to " + o.Name, Body: m.Text + "\n"})
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
