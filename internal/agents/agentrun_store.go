package agents

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

var runGoing = []string{store.RunWorking, store.RunWaiting}

func (s *AgentStore) TaskOpen(org, task int64) error {
	_, err := store.OpenTask(s.DB, org, task)
	return err
}

// RunsOn counts the runs of one agent working or waiting on a task.
func (s *AgentStore) RunsOn(org, task, agent int64) (int64, error) {
	var n int64
	return n, s.InOrg(org).Model(&store.AgentRun{}).Where("task_id = ? and agent_id = ? and status in ?", task, agent, runGoing).Count(&n).Error
}

// AddRun fails if the agent already has a run going on the task, which an index keeps.
func (s *AgentStore) AddRun(org int64, run store.AgentRun) (*store.AgentRun, error) {
	run.ID, run.OrgID = 0, org
	return &run, s.DB.Create(&run).Error
}

func (s *AgentStore) RunByID(org, id int64) (*store.AgentRun, error) {
	var run store.AgentRun
	if err := s.InOrg(org).First(&run, id).Error; err != nil {
		return nil, store.Missing(err, "no run %d in this organisation", id)
	}
	return &run, nil
}

// AskRunStop reports whether the run was still going; one that has ended is as
// stopped as it will get.
func (s *AgentStore) AskRunStop(org, id int64) (going bool, err error) {
	res := s.InOrg(org).Model(&store.AgentRun{}).Where("id = ? and status in ?", id, runGoing).Update("stop_asked", true)
	if res.Error != nil || res.RowsAffected > 0 {
		return res.RowsAffected > 0, res.Error
	}
	_, err = s.RunByID(org, id)
	return false, err
}

// The rest change a run only while the server named as its owner still holds it, each
// in one statement, so of two servers only one ever drives a run.

// RenewRun reports false once a person has stopped the run or another server took it.
func (s *AgentStore) RenewRun(org, id int64, owner string, until time.Time) (bool, error) {
	res := s.InOrg(org).Model(&store.AgentRun{}).Where("id = ? and owner = ? and stop_asked = ? and status in ?", id, owner, false, runGoing).
		Update("lease_until", until.UnixMilli())
	return res.RowsAffected > 0, res.Error
}

// HoldRun renews the lease as RenewRun does, and keeps the run's status and counts.
func (s *AgentStore) HoldRun(org int64, run store.AgentRun, status string, waitUntil *time.Time, until time.Time) (bool, error) {
	res := s.InOrg(org).Model(&store.AgentRun{}).Where("id = ? and owner = ? and stop_asked = ? and status in ?", run.ID, run.Owner, false, runGoing).
		Updates(map[string]any{"lease_until": until.UnixMilli(), "status": status, "wait_until": waitUntil,
			"turns": run.Turns, "episodes": run.Episodes, "tool_calls": run.ToolCalls, "added": run.Added,
			"tokens_in": run.TokensIn, "tokens_out": run.TokensOut, "request_id": run.RequestID})
	return res.RowsAffected == 1, res.Error
}

func (s *AgentStore) EndRun(org int64, run store.AgentRun, status, reason string, at time.Time) error {
	return s.InOrg(org).Model(&store.AgentRun{}).Where("id = ? and owner = ?", run.ID, run.Owner).
		Updates(map[string]any{"status": status, "stop_reason": reason, "ended_at": &at, "lease_until": 0, "wait_until": nil,
			"turns": run.Turns, "episodes": run.Episodes, "tool_calls": run.ToolCalls, "added": run.Added,
			"tokens_in": run.TokensIn, "tokens_out": run.TokensOut, "request_id": run.RequestID}).Error
}

// LapsedRuns are those, in every organisation, whose server stopped renewing them.
func (s *AgentStore) LapsedRuns(now time.Time) ([]store.AgentRun, error) {
	var lapsed []store.AgentRun
	return lapsed, s.DB.Where("status in ? and lease_until < ?", runGoing, now.UnixMilli()).Find(&lapsed).Error
}

// A lapsed run is ended or taken over only if its lease is still the one that lapsed,
// so of two servers sweeping, one does it.
func (s *AgentStore) StopLapsedRun(org, id, lease int64, at time.Time) error {
	return s.InOrg(org).Model(&store.AgentRun{}).Where("id = ? and lease_until = ?", id, lease).
		Updates(map[string]any{"status": RunStopped, "stop_reason": "stopped by a person", "ended_at": &at, "lease_until": 0}).Error
}

func (s *AgentStore) TakeOverRun(org, id, lease int64, owner string, until time.Time, resumes int) (bool, error) {
	res := s.InOrg(org).Model(&store.AgentRun{}).Where("id = ? and lease_until = ?", id, lease).
		Updates(map[string]any{"owner": owner, "lease_until": until.UnixMilli(), "resumes": resumes, "status": store.RunWorking})
	return res.RowsAffected > 0, res.Error
}

// DropKeyClaims lets go of the requests a key holds on a task, under any session.
func (s *AgentStore) DropKeyClaims(org, task, key int64) error {
	mine := fmt.Sprintf("key:%d", key)
	return s.InOrg(org).Where("task_id = ? and (holder = ? or holder like ?)", task, mine, mine+" %").Delete(&store.RequestClaim{}).Error
}

func (s *AgentStore) KeyRevoked(org, id int64) (bool, error) {
	var revoked int64
	err := s.InOrg(org).Model(&store.APIKey{}).Where("id = ? and revoked_at is not null", id).Count(&revoked).Error
	return revoked > 0, err
}

func (s *AgentStore) RequestDone(org, id int64) (bool, error) {
	var r store.TaskRequest
	err := s.InOrg(org).Where("id = ?", id).Limit(1).Find(&r).Error
	return r.DoneAt != nil, err
}

// AgentKey is the agent as an API key that no request can present: its hash is
// nothing a secret could be looked up as. One revoked is replaced, so a deleted
// agent's runs stop while a new one called the same starts clean.
func (s *AgentStore) AgentKey(org int64, c *store.Agent) (*store.APIKey, error) {
	var k store.APIKey
	if c.KeyID != 0 {
		if err := s.InOrg(org).Where("id = ?", c.KeyID).Limit(1).Find(&k).Error; err != nil {
			return nil, err
		}
		if k.ID != 0 && k.RevokedAt == nil {
			return &k, nil
		}
	}
	k = store.APIKey{OrgID: org, Name: store.BuiltinKeyName(c.Name), Prefix: store.BuiltinPrefix, Hash: "builtin:" + store.RandomToken(24),
		Scopes: "read research:write", CreatedBy: "prospector"}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&k).Error; err != nil {
			return err
		}
		res := tx.Model(&store.Agent{}).Where("org_id = ? and id = ?", org, c.ID).Update("key_id", k.ID)
		if res.Error == nil && res.RowsAffected == 0 {
			return say.Errorf("no agent %d in this organisation", c.ID)
		}
		return res.Error
	})
	if err != nil {
		return nil, err
	}
	c.KeyID = k.ID
	return &k, nil
}

// AgentRuns are a task's runs, newest first: every one going, however old, so each
// can be stopped, and the latest that ended, up to limit.
// AgentRuns is a task's runs still going, then the latest limit that ended; every one
// that ended when limit is 0.
func (s *AgentStore) AgentRuns(org, task int64, limit int) ([]store.AgentRun, error) {
	going := []string{store.RunWorking, store.RunWaiting}
	var out, ended []store.AgentRun
	if err := s.DB.Where("org_id = ? and task_id = ? and status in ?", org, task, going).Order("id desc").Find(&out).Error; err != nil {
		return nil, err
	}
	q := s.DB.Where("org_id = ? and task_id = ? and status not in ?", org, task, going).Order("id desc")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&ended).Error; err != nil {
		return nil, err
	}
	return append(out, ended...), nil
}

const RunStopped = "stopped"
