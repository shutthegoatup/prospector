package main

import (
	"strconv"
	"time"

	"gorm.io/gorm"
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

// holderOf is who a claim belongs to: the key, since key names can be reused.
func holderOf(who Actor) string {
	if who.KeyID != 0 {
		return "key:" + strconv.FormatInt(who.KeyID, 10)
	}
	return "name:" + who.Name
}

// claim takes a request for who unless another holds it and it hasn't lapsed; who's
// own claim is renewed. One statement, so of two agents claiming at once only one wins.
func (s *Store) claim(org, task, request int64, who Actor, now time.Time) (bool, error) {
	res := s.db.Exec(`insert into request_claims (request_id, org_id, task_id, holder, actor, claimed_at, expires_at)
		values (?, ?, ?, ?, ?, ?, ?)
		on conflict (request_id) do update set holder = excluded.holder, actor = excluded.actor, expires_at = excluded.expires_at,
			claimed_at = case when request_claims.holder = excluded.holder then request_claims.claimed_at else excluded.claimed_at end
		where request_claims.expires_at <= ? or request_claims.holder = excluded.holder`,
		request, org, task, holderOf(who), who.Name, now.UnixMilli(), now.Add(claimFor).UnixMilli(), now.UnixMilli())
	return res.RowsAffected == 1, res.Error
}

// renewClaims is the heartbeat an agent can't forget: each write it makes for a task
// keeps the requests it holds there.
func (s *Store) renewClaims(tx *gorm.DB, org, task int64, who Actor, now time.Time) error {
	return tx.Model(&RequestClaim{}).Where("org_id = ? and task_id = ? and holder = ? and expires_at > ?", org, task, holderOf(who), now.UnixMilli()).
		Update("expires_at", now.Add(claimFor).UnixMilli()).Error
}

func liveClaims(tx *gorm.DB, org, task int64, now time.Time) (map[int64]RequestClaim, error) {
	var rows []RequestClaim
	if err := tx.Where("org_id = ? and task_id = ? and expires_at > ?", org, task, now.UnixMilli()).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := map[int64]RequestClaim{}
	for _, c := range rows {
		out[c.RequestID] = c
	}
	return out, nil
}

// claimNext keeps an open request for who: one it already holds, or else the oldest
// nobody else does. It returns 0 when other agents hold every open request.
func (s *Store) claimNext(v *TaskView, who Actor, now time.Time) (int64, error) {
	open := v.Open()
	for _, r := range open {
		if r.Holder == holderOf(who) {
			_, err := s.claim(v.OrgID, v.ID, r.ID, who, now)
			return r.ID, err
		}
	}
	for _, r := range open {
		if r.Holder != "" {
			continue
		}
		// Someone else may have taken it since the task was read; then try the next.
		won, err := s.claim(v.OrgID, v.ID, r.ID, who, now)
		if err != nil || won {
			return r.ID, err
		}
	}
	return 0, nil
}
