package main

import (
	"fmt"
	"time"
)

var runGoing = []string{RunWorking, RunWaiting}

func (s *Store) TaskOpen(org, task int64) error {
	_, err := openTask(s.db, org, task)
	return err
}

// RunsOn counts the runs of one agent working or waiting on a task.
func (s *Store) RunsOn(org, task, agent int64) (int64, error) {
	var n int64
	return n, s.in(org).Model(&AgentRun{}).Where("task_id = ? and agent_id = ? and status in ?", task, agent, runGoing).Count(&n).Error
}

// AddRun fails if the agent already has a run going on the task, which an index keeps.
func (s *Store) AddRun(org int64, run AgentRun) (*AgentRun, error) {
	run.ID, run.OrgID = 0, org
	return &run, s.db.Create(&run).Error
}

func (s *Store) RunByID(org, id int64) (*AgentRun, error) {
	var run AgentRun
	if err := s.in(org).First(&run, id).Error; err != nil {
		return nil, missing(err, "no run %d in this organisation", id)
	}
	return &run, nil
}

// AskRunStop reports whether the run was still going; one that has ended is as
// stopped as it will get.
func (s *Store) AskRunStop(org, id int64) (going bool, err error) {
	res := s.in(org).Model(&AgentRun{}).Where("id = ? and status in ?", id, runGoing).Update("stop_asked", true)
	if res.Error != nil || res.RowsAffected > 0 {
		return res.RowsAffected > 0, res.Error
	}
	_, err = s.RunByID(org, id)
	return false, err
}

// The rest change a run only while the server named as its owner still holds it, each
// in one statement, so of two servers only one ever drives a run.

// RenewRun reports false once a person has stopped the run or another server took it.
func (s *Store) RenewRun(org, id int64, owner string, until time.Time) (bool, error) {
	res := s.in(org).Model(&AgentRun{}).Where("id = ? and owner = ? and stop_asked = ? and status in ?", id, owner, false, runGoing).
		Update("lease_until", until.UnixMilli())
	return res.RowsAffected > 0, res.Error
}

// HoldRun renews the lease as RenewRun does, and keeps the run's status and counts.
func (s *Store) HoldRun(org int64, run AgentRun, status string, waitUntil *time.Time, until time.Time) (bool, error) {
	res := s.in(org).Model(&AgentRun{}).Where("id = ? and owner = ? and stop_asked = ? and status in ?", run.ID, run.Owner, false, runGoing).
		Updates(map[string]any{"lease_until": until.UnixMilli(), "status": status, "wait_until": waitUntil,
			"turns": run.Turns, "episodes": run.Episodes, "tool_calls": run.ToolCalls, "added": run.Added,
			"tokens_in": run.TokensIn, "tokens_out": run.TokensOut, "request_id": run.RequestID})
	return res.RowsAffected == 1, res.Error
}

func (s *Store) EndRun(org int64, run AgentRun, status, reason string, at time.Time) error {
	return s.in(org).Model(&AgentRun{}).Where("id = ? and owner = ?", run.ID, run.Owner).
		Updates(map[string]any{"status": status, "stop_reason": reason, "ended_at": &at, "lease_until": 0, "wait_until": nil,
			"turns": run.Turns, "episodes": run.Episodes, "tool_calls": run.ToolCalls, "added": run.Added,
			"tokens_in": run.TokensIn, "tokens_out": run.TokensOut, "request_id": run.RequestID}).Error
}

// LapsedRuns are those, in every organisation, whose server stopped renewing them.
func (s *Store) LapsedRuns(now time.Time) ([]AgentRun, error) {
	var lapsed []AgentRun
	return lapsed, s.db.Where("status in ? and lease_until < ?", runGoing, now.UnixMilli()).Find(&lapsed).Error
}

// A lapsed run is ended or taken over only if its lease is still the one that lapsed,
// so of two servers sweeping, one does it.

func (s *Store) StopLapsedRun(org, id, lease int64, at time.Time) error {
	return s.in(org).Model(&AgentRun{}).Where("id = ? and lease_until = ?", id, lease).
		Updates(map[string]any{"status": RunStopped, "stop_reason": "stopped by a person", "ended_at": &at, "lease_until": 0}).Error
}

func (s *Store) TakeOverRun(org, id, lease int64, owner string, until time.Time, resumes int) (bool, error) {
	res := s.in(org).Model(&AgentRun{}).Where("id = ? and lease_until = ?", id, lease).
		Updates(map[string]any{"owner": owner, "lease_until": until.UnixMilli(), "resumes": resumes, "status": RunWorking})
	return res.RowsAffected > 0, res.Error
}

// DropKeyClaims lets go of the requests a key holds on a task, under any session.
func (s *Store) DropKeyClaims(org, task, key int64) error {
	mine := fmt.Sprintf("key:%d", key)
	return s.in(org).Where("task_id = ? and (holder = ? or holder like ?)", task, mine, mine+" %").Delete(&RequestClaim{}).Error
}

func (s *Store) KeyRevoked(org, id int64) (bool, error) {
	var revoked int64
	err := s.in(org).Model(&APIKey{}).Where("id = ? and revoked_at is not null", id).Count(&revoked).Error
	return revoked > 0, err
}

func (s *Store) RequestDone(org, id int64) (bool, error) {
	var r TaskRequest
	err := s.in(org).Where("id = ?", id).Limit(1).Find(&r).Error
	return r.DoneAt != nil, err
}
