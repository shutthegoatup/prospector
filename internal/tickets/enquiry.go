package tickets

import (
	"strings"
	"time"

	"github.com/shutthegoatup/prospector/internal/cleantext"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

const EnquiryOpen = "open"

const EnquiryWaiting = "waiting"

const EnquiryClosed = "closed"

var EnquiryStates = []struct{ Key, Label string }{
	{EnquiryOpen, "To answer"}, {EnquiryWaiting, "Waiting on them"}, {EnquiryClosed, "Closed"},
}

func enquiryStateLabel(state string) string {
	for _, s := range EnquiryStates {
		if s.Key == state {
			return s.Label
		}
	}
	return ""
}

var EnquiryKinds = []struct{ Key, Label string }{
	{"sales", "Sales"}, {"demo", "Demo"}, {"signup", "Sign-up"}, {"support", "Help"}, {"feature", "Feature request"}, {"other", "Other"},
}

func enquiryKind(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	for _, known := range EnquiryKinds {
		if known.Key == k {
			return k
		}
	}
	return "other"
}

func kindLabel(k string) string {
	for _, known := range EnquiryKinds {
		if known.Key == k {
			return known.Label
		}
	}
	return k
}

type EnquiryIn struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Company string `json:"company"`
	Phone   string `json:"phone"`
	Message string `json:"message"`
	Page    string `json:"page"`
	Site    string `json:"site"`
	// Trap is a field no person sees. A form filler that completes it is kept, but as spam.
	Trap string `json:"trap"`
}

const enquiryRepeat = 10 * time.Minute

var ErrEnquiryEmail = say.New("an email address is needed, so we can answer")

type EnquiryFilter struct {
	Kind string
	Spam bool
	// State narrows to one state of answering, when set.
	State string
}

const EnquiryPageSize = 50

// EnquiriesAround is what is shown beside a page of enquiries: the companies they
// matched, which senders are on the Conversations board, and what has been said since.
type EnquiriesAround struct {
	Companies map[int64]string
	Talking   map[int64]bool
	Threads   map[int64][]store.EnquiryMessage
}

// senderName is the name on an enquiry as a contact's: one line, short enough to be a
// name. The form's sender chose every character, and a contact's name is read by
// agents, so a paragraph of text isn't taken as one.
func senderName(e store.Enquiry) string {
	name := store.Clip(cleantext.OneLine(e.Name), 80)
	if name == "" {
		name, _, _ = strings.Cut(e.Email, "@")
	}
	return name
}
