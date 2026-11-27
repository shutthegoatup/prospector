package identity

import (
	"time"

	"github.com/shutthegoatup/prospector/internal/say"
)

const resetLife = time.Hour

const inviteLife = 7 * 24 * time.Hour

const // ResetGap is the least time between two reset emails to one account, so the form
// can't be used to fill somebody's inbox.
ResetGap = 5 * time.Minute

var ErrTooSoon = say.New("a reset link was sent to this account a moment ago")

var ErrBadLink = say.New("this link has expired or has been used: ask for a new one")
