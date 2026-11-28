package app

import (
	"context"
	"log"

	"github.com/shutthegoatup/prospector/internal/mailer"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

var errNoReplyMail = say.New("mail isn't set up, so a reply can't be sent from here: an owner sets a mail server in Settings")

// ReplyEnquiry emails a reply to whoever wrote in, at the address they gave and no
// other. The mail carries only what was typed for it: nothing they sent us is quoted
// back, so a form filled in with someone else's address can't be used to send that
// person words of the sender's choosing. answers is where their answer should go.
func (a *App) ReplyEnquiry(ctx context.Context, o *store.Org, enquiry int64, text, actor, answers string) (*store.EnquiryMessage, error) {
	via, err := a.MailFor(o.ID)
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
		return nil, store.NotSent{Err: say.New("the mail server didn't take the reply, and nothing was recorded; try again, or ask an owner to check the mail settings")}
	}
	return m, nil
}
