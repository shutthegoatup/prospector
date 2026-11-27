package mailer

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
)

// TestASendSaysWhetherTheMailWent: what a failure means decides whether a campaign
// tries the address again, gives it up or must not send it twice. Refused before the
// message was handed over, it didn't go; an address refused for good is that, and only
// that; once the message is handed over, a goodbye that fails isn't a failed send.
func TestASendSaysWhetherTheMailWent(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		rcptReply, dataReply string
		hangUp               bool
		unsent, refused      bool
		sent                 bool
	}{
		"taken":                              {sent: true},
		"the address refused for good (5xx)": {rcptReply: "550 no such user", refused: true},
		"the address put off for now (4xx)":  {rcptReply: "451 try again later", unsent: true},
		"the message refused at its end":     {dataReply: "554 rejected"},
		"no goodbye after taking it":         {hangUp: true, sent: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFakeSMTP(t, nil)
			f.mu.Lock()
			f.rcptReply, f.dataReply, f.hangUp = c.rcptReply, c.dataReply, c.hangUp
			f.mu.Unlock()
			host, port, _ := net.SplitHostPort(f.addr)
			n, _ := strconv.Atoi(port)
			via := Server{Host: host, Port: n, From: "site@prospector.example"}
			err := Send(context.Background(), via, Message{To: "grace@example.com", Subject: "x", Body: "y"})
			if got := errors.Is(err, ErrUnsent); got != c.unsent {
				t.Errorf("not sent: %v, want %v (%v)", got, c.unsent, err)
			}
			if got := errors.Is(err, ErrRefusedRecipient); got != c.refused {
				t.Errorf("refused: %v, want %v (%v)", got, c.refused, err)
			}
			if got := err == nil; got != c.sent {
				t.Errorf("sent: %v, want %v (%v)", got, c.sent, err)
			}
		})
	}
}
