package marketing

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/shutthegoatup/prospector/internal/hooks"
	"github.com/shutthegoatup/prospector/internal/store"
)

// PostmarkHook takes Postmark's bounce and spam complaint webhooks. Postmark doesn't
// sign what it posts, so it signs in with basic auth: the organisation's slug, and the
// secret it was given.
type PostmarkHook struct{ Mail *OutreachStore }

// postmarkRecord is the part of Postmark's bounce and spam complaint webhooks that is used.
type postmarkRecord struct {
	RecordType string `json:"RecordType"`
	Type       string `json:"Type"`
	Email      string `json:"Email"`
}

// permanent is whether the bounce says the address won't take mail at all. A soft
// bounce (a full mailbox, a server down) is left alone: the address may recover.
func (b postmarkRecord) permanent() bool {
	switch b.Type {
	case "HardBounce", "Blocked", "Suppressed":
		return true
	}
	return false
}

func (h PostmarkHook) Verify(r *http.Request, _ []byte) (int64, error) {
	slug, secret, _ := r.BasicAuth()
	o, ok := h.Mail.BounceHookOrg(slug, secret)
	if !ok {
		return 0, hooks.ErrUnauthorized
	}
	return o.ID, nil
}

func (h PostmarkHook) Handle(_ context.Context, org int64, _ *http.Request, body []byte) error {
	var b postmarkRecord
	if err := json.Unmarshal(body, &b); err != nil || b.RecordType == "" {
		return hooks.ErrMalformed
	}
	var reason, said string
	switch {
	case b.RecordType == "Bounce" && b.permanent():
		reason, said = SuppressBounced, "it bounced ("+b.Type+")"
	case b.RecordType == "SpamComplaint":
		reason = SuppressComplained
	default:
		return nil
	}
	addr, err := store.BareAddress(b.Email)
	if err != nil {
		return nil //nolint:nilerr // An address no mail could reach is nothing to keep, nor to send again.
	}
	return h.Mail.MailFailed(org, addr, reason, said)
}
