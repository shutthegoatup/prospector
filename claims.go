package main

import (
	"strconv"
	"time"
)

// claimFor is long enough for an agent between two writes, short enough that a
// request left by one that stopped is free again soon.
const claimFor = 15 * time.Minute

// holderOf is who a claim belongs to: the key, since key names can be reused, and
// the session, since two clients can share a key.
func holderOf(who Actor) string {
	holder := "name:" + who.Name
	if who.KeyID != 0 {
		holder = "key:" + strconv.FormatInt(who.KeyID, 10)
	}
	if who.Session != "" {
		holder += " session:" + who.Session
	}
	return holder
}
