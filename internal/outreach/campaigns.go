package outreach

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/shutthegoatup/prospector/internal/store"
)

const KindMarketing = "marketing"

const AudienceConversations = "conversations"

const CampaignDraft = "draft"

const CampaignSending = "sending"

const CampaignPaused = "paused"

const CampaignSent = "sent"

const CampaignCancelled = "cancelled"

const campaignSubjectLimit = 200

const campaignBodyLimit = 20000

var CampaignAudiences = []struct{ Key, Label string }{
	{AudienceConversations, "Everyone in conversation"},
	{store.AudienceCustomers, "Everyone at a customer"},
}

func audienceLabel(key string) string {
	for _, a := range CampaignAudiences {
		if a.Key == key {
			return a.Label
		}
	}
	return ""
}

type CampaignIn struct {
	Name     string `json:"name"`
	Audience string `json:"audience"`
	Kind     string `json:"kind"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
}

// campaignField checks one field as a draft holds it.
func campaignField(field, value string) (string, error) {
	switch field {
	case "name":
		value = strings.TrimSpace(value)
		if value == "" {
			return "", errors.New("a campaign needs a name")
		}
		if utf8.RuneCountInString(value) > 120 {
			return "", errors.New("a campaign's name is 120 characters at most")
		}
	case "audience":
		if audienceLabel(value) == "" {
			return "", fmt.Errorf("a campaign goes to %q or %q", AudienceConversations, store.AudienceCustomers)
		}
	case "kind":
		if value != KindMarketing && value != store.KindService {
			return "", fmt.Errorf("a campaign is %q or %q", KindMarketing, store.KindService)
		}
	case "subject":
		// One line: a subject is a header.
		value = strings.Join(strings.Fields(value), " ")
		if utf8.RuneCountInString(value) > campaignSubjectLimit {
			return "", fmt.Errorf("a subject is %d characters at most", campaignSubjectLimit)
		}
	case "body":
		// Only trailing blank lines go: a line that ends in a space, as "-- " does, keeps it.
		value = strings.TrimRight(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
		if utf8.RuneCountInString(value) > campaignBodyLimit {
			return "", fmt.Errorf("a campaign is %d characters at most", campaignBodyLimit)
		}
	default:
		return "", fmt.Errorf("a campaign has no %q to set", field)
	}
	return value, nil
}

// Recipient is one person a campaign is for, and why they are left out when they are.
type Recipient struct {
	ContactID int64  `json:"contact_id"`
	Name      string `json:"name"`
	Company   string `json:"company"`
	Address   string `json:"address"`
	// LeftOut is empty for someone it goes to, and otherwise says why it doesn't.
	LeftOut string `json:"left_out"`
}

// audienceWork is what every campaign's audience is checked against, read once for a
// page of them.
type audienceWork struct {
	gate      *MailGate
	customers []int64
	noMail    map[string]bool
}

// LeftOutBecause says why someone is left out, for the reasons there are. Any other
// error is a failure to check, and is not passed off as a reason.
func LeftOutBecause(err error) (string, bool) {
	switch {
	case errors.Is(err, ErrSuppressed):
		return "on the list not to be mailed", true
	case errors.Is(err, ErrBounced):
		return "mail to them bounced", true
	case errors.Is(err, ErrComplained):
		return "marked a mail as spam", true
	case errors.Is(err, store.ErrErased):
		return "erased on request", true
	case errors.Is(err, store.ErrNeverContact):
		return "on the never-contact list", true
	}
	return "", false
}
