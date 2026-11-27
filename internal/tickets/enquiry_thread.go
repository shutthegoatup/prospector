package tickets

import (
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

const ReplyLimit = 10000

const // repliesUnanswered is how many replies can go to one enquiry with no answer recorded between.
repliesUnanswered = 3

const // replyGap is how long the same words to the same enquiry count as a second press
// of the button and not a second reply.
replyGap = 2 * time.Minute

const // replyStale is when a reply that never got its SentAt is given up on.
replyStale = 10 * time.Minute

var InThread = "(ours = false or sent_at is not null)"

func threadText(text string) (string, error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	switch {
	case text == "":
		return "", say.New("write something first")
	case utf8.RuneCountInString(text) > ReplyLimit:
		return "", say.Errorf("that is longer than %d characters", ReplyLimit)
	}
	return text, nil
}

// answerable is whether an enquiry can be written back to: it wasn't spam, it gave one
// address, and that address isn't on the never-contact list.
func answerable(o *store.Org, e store.Enquiry) (string, error) {
	if e.Spam {
		return "", say.New("that was marked as spam, so there is nobody to answer")
	}
	to, err := mail.ParseAddress(strings.TrimSpace(e.Email))
	if err != nil {
		return "", say.New("it gave no single email address to answer")
	}
	if err := o.Settings.Listed(to.Address, ""); err != nil {
		return "", err
	}
	return to.Address, nil
}

func EnquiryIDs(rows []store.Enquiry) []int64 {
	ids := make([]int64, 0, len(rows))
	for _, e := range rows {
		ids = append(ids, e.ID)
	}
	return ids
}
