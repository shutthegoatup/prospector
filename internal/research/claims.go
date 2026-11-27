package research

import (
	"strconv"
	"time"

	"github.com/shutthegoatup/prospector/internal/store"
)

// ClaimFor is long enough for an agent between two writes, short enough that a
// request left by one that stopped is free again soon.
const ClaimFor = 15 * time.Minute

// holderOf is who a claim belongs to: the key, since key names can be reused, and
// the session, since two clients can share a key.
func holderOf(who store.Actor) string {
	holder := "name:" + who.Name
	if who.KeyID != 0 {
		holder = "key:" + strconv.FormatInt(who.KeyID, 10)
	}
	if who.Session != "" {
		holder += " session:" + who.Session
	}
	return holder
}
