package store

import (
	"net/mail"
	"strings"
	"time"

	"golang.org/x/net/idna"

	"github.com/shutthegoatup/prospector/internal/say"
)

// Suppression is an address the organisation's marketing mail never goes to again. One
// list serves every mailing: someone who unsubscribed from one has unsubscribed.
type Suppression struct {
	ID      int64  `gorm:"primaryKey" json:"id"`
	OrgID   int64  `gorm:"uniqueIndex:idx_suppression_address,priority:1;not null" json:"-"`
	Address string `gorm:"uniqueIndex:idx_suppression_address,priority:2;not null" json:"address"`
	// Reason is "unsubscribed" when they asked, by a link in a mail; "complained" when
	// they marked one as spam; "added" when a member or key put them here; and "bounced"
	// when it is here only because mail to it came back for good. Only an added or
	// bounced one can be taken off.
	Reason    string    `gorm:"not null;default:'added'" json:"reason"`
	By        string    `gorm:"not null;default:''" json:"by"`
	CreatedAt time.Time `json:"created_at"`
	// BouncedAt is when mail to it last came back for good, whatever the reason: a bounce
	// is about the address, not their wishes, so it is kept beside them.
	BouncedAt *time.Time `json:"bounced_at"`
}

// UnsubscribeLink is the secret in one mail's unsubscribe link, kept as its hash. It
// never runs out, because the mail it is in doesn't.
type UnsubscribeLink struct {
	ID        int64     `gorm:"primaryKey"`
	OrgID     int64     `gorm:"index;not null"`
	Address   string    `gorm:"index;not null"`
	TokenHash string    `gorm:"uniqueIndex;not null"`
	CreatedAt time.Time `gorm:"index"`
}

var errNotAnAddress = say.New("that isn't one email address")

// EmailKey is an address as it is looked up and compared: trimmed and in lower case.
func EmailKey(address string) string { return strings.ToLower(strings.TrimSpace(address)) }

// AddressDomain is the domain an address's mail goes to, in its ASCII form, or "" when it
// has none: after the last @, since a quoted local part may hold one too.
func AddressDomain(address string) string {
	_, after, ok := strings.CutLast(address, "@")
	if !ok {
		return ""
	}
	domain, err := idna.Lookup.ToASCII(strings.Trim(strings.ToLower(after), ". "))
	if err != nil || !strings.Contains(domain, ".") {
		return ""
	}
	return domain
}

func BareAddress(address string) (string, error) {
	at, err := mail.ParseAddress(strings.TrimSpace(address))
	if err != nil {
		return "", errNotAnAddress
	}
	return strings.ToLower(at.Address), nil
}

var ErrNeverContact = say.New("on the never-contact list")

// BounceHook is the secret Postmark signs its webhook in with, kept as its hash.
// Postmark doesn't sign what it posts, so the secret is all that says a bounce is real.
type BounceHook struct {
	OrgID      int64 `gorm:"primaryKey;autoIncrement:false"`
	SecretHash string
	CreatedAt  time.Time
}
