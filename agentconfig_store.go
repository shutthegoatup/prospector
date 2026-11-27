package main

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// agentStore is the agents area's queries: Prospector's own agents, their settings and keys, and the runs they work tasks in.
type agentStore struct{ *core }

// Agents are an organisation's own agents, by name.
func (s *agentStore) Agents(org int64) ([]Agent, error) {
	var out []Agent
	return out, s.DB.Where("org_id = ?", org).Order("name, id").Find(&out).Error
}

func (s *agentStore) AgentByID(org, id int64) (*Agent, error) {
	var c Agent
	if err := s.DB.Where("org_id = ? and id = ?", org, id).Limit(1).Find(&c).Error; err != nil || c.ID == 0 {
		return nil, missing(err, "no agent %d in this organisation", id)
	}
	return &c, nil
}

func (s *agentStore) AgentNamed(org int64, name string) (*Agent, error) {
	var c Agent
	if err := s.DB.Where("org_id = ? and name = ?", org, name).Limit(1).Find(&c).Error; err != nil || c.ID == 0 {
		return nil, errors.Join(err, fmt.Errorf("no agent called %q in this organisation", name))
	}
	return &c, nil
}

// SaveAgentRow stores an agent's settings: a new one is made; an existing one has only
// what an owner edits written, and only while it is still there. Checking an endpoint
// takes seconds, and an agent deleted meanwhile mustn't come back, nor a key its first
// run made meanwhile be lost.
func (s *agentStore) SaveAgentRow(org int64, c *Agent) error {
	c.OrgID = org
	if c.ID == 0 {
		return s.DB.Create(c).Error
	}
	res := s.DB.Model(&Agent{}).Where("id = ? and org_id = ?", c.ID, org).
		Select("name", "kind", "base_url", "fallback_id", "fallback_minutes", "thinking_off", "api_key", "model", "tools_url", "search_url", "models", "tools", "allowed", "problem", "checked_at", "updated_by", "updated_at").
		Updates(c)
	if res.Error == nil && res.RowsAffected == 0 {
		return fmt.Errorf("%s was deleted meanwhile", c.Name)
	}
	return res.Error
}

func (s *agentStore) AgentNameTaken(org int64, name string, except int64) (bool, error) {
	var taken int64
	err := s.InOrg(org).Model(&Agent{}).Where("name = ? and id <> ?", name, except).Count(&taken).Error
	return taken > 0, err
}

func (s *agentStore) RenameKey(org, id int64, name string) error {
	return s.InOrg(org).Model(&APIKey{}).Where("id = ?", id).Update("name", name).Error
}

// AgentRunsGoing is what an agent is working or waiting on.
func (s *agentStore) AgentRunsGoing(org, agent int64) ([]AgentRun, error) {
	var going []AgentRun
	return going, s.InOrg(org).Where("agent_id = ? and status in ?", agent, []string{RunWorking, RunWaiting}).Find(&going).Error
}

// DeleteAgent revokes its key and forgets its settings, and any agent falling back to it
// falls back to nothing.
func (s *agentStore) DeleteAgent(org, id int64) error {
	now := time.Now()
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var c Agent
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
			return missing(err, "no agent %d in this organisation", id)
		}
		if c.KeyID != 0 {
			if err := tx.Model(&APIKey{}).Where("org_id = ? and id = ? and revoked_at is null", org, c.KeyID).Update("revoked_at", &now).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&Agent{}).Where("org_id = ? and fallback_id = ?", org, c.ID).Update("fallback_id", 0).Error; err != nil {
			return err
		}
		return tx.Delete(&c).Error
	})
}
