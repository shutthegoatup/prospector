package main

import (
	"strconv"
	"time"
)

// RequestClaim says an agent is on an open request, so another picking up the task
// takes a different one. It is advice, not a lock: anyone's writes still go through.
// It lapses by itself, because agents stop without saying so; times are Unix
// milliseconds, compared the same way on both databases.
type RequestClaim struct {
	RequestID int64  `gorm:"primaryKey;autoIncrement:false"`
	OrgID     int64  `gorm:"index;not null"`
	TaskID    int64  `gorm:"index;not null"`
	Holder    string `gorm:"not null"`
	Actor     string `gorm:"not null"`
	ClaimedAt int64  `gorm:"not null"`
	ExpiresAt int64  `gorm:"not null"`
}

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
