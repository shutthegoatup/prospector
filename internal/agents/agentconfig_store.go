package agents

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// AgentStore is the agents area's queries: Prospector's own agents, their settings and keys, and the runs they work tasks in.
type AgentStore struct{ *store.Core }

// Agents are an organisation's own agents, by name.
func (s *AgentStore) Agents(org int64) ([]store.Agent, error) {
	var out []store.Agent
	return out, s.DB.Where("org_id = ?", org).Order("name, id").Find(&out).Error
}

func (s *AgentStore) AgentByID(org, id int64) (*store.Agent, error) {
	var c store.Agent
	if err := s.DB.Where("org_id = ? and id = ?", org, id).Limit(1).Find(&c).Error; err != nil || c.ID == 0 {
		return nil, store.Missing(err, "no agent %d in this organisation", id)
	}
	return &c, nil
}

func (s *AgentStore) AgentNamed(org int64, name string) (*store.Agent, error) {
	var c store.Agent
	if err := s.DB.Where("org_id = ? and name = ?", org, name).Limit(1).Find(&c).Error; err != nil || c.ID == 0 {
		return nil, errors.Join(err, say.Errorf("no agent called %q in this organisation", name))
	}
	return &c, nil
}

// SaveAgentRow stores an agent's settings: a new one is made; an existing one has only
// what an owner edits written, and only while it is still there. Checking an endpoint
// takes seconds, and an agent deleted meanwhile mustn't come back, nor a key its first
// run made meanwhile be lost.
func (s *AgentStore) SaveAgentRow(org int64, c *store.Agent) error {
	c.OrgID = org
	return s.DB.Transaction(func(tx *gorm.DB) error {
		action := store.ActAgentChange
		if c.ID == 0 {
			if err := tx.Create(c).Error; err != nil {
				return err
			}
			action = store.ActAgentNew
		} else {
			res := tx.Model(&store.Agent{}).Where("id = ? and org_id = ?", c.ID, org).
				Select("name", "kind", "base_url", "fallback_id", "fallback_minutes", "thinking_off", "api_key", "model", "tools_url", "search_url", "models", "tools", "allowed", "problem", "checked_at", "updated_by", "updated_at").
				Updates(c)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return say.Errorf("%s was deleted meanwhile", c.Name)
			}
		}
		return s.Log(tx, org, 0, nil, c.UpdatedBy, action, &c.Name)
	})
}

func (s *AgentStore) AgentNameTaken(org int64, name string, except int64) (bool, error) {
	var taken int64
	err := s.InOrg(org).Model(&store.Agent{}).Where("name = ? and id <> ?", name, except).Count(&taken).Error
	return taken > 0, err
}

func (s *AgentStore) RenameKey(org, id int64, name string) error {
	return s.InOrg(org).Model(&store.APIKey{}).Where("id = ?", id).Update("name", name).Error
}

// AgentRunsGoing is what an agent is working or waiting on.
func (s *AgentStore) AgentRunsGoing(org, agent int64) ([]store.AgentRun, error) {
	var going []store.AgentRun
	return going, s.InOrg(org).Where("agent_id = ? and status in ?", agent, []string{store.RunWorking, store.RunWaiting}).Find(&going).Error
}

// DeleteAgent revokes its key and forgets its settings, and any agent falling back to it
// falls back to nothing.
func (s *AgentStore) DeleteAgent(org, id int64, actor string) error {
	now := time.Now()
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var c store.Agent
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
			return store.Missing(err, "no agent %d in this organisation", id)
		}
		if c.KeyID != 0 {
			if err := tx.Model(&store.APIKey{}).Where("org_id = ? and id = ? and revoked_at is null", org, c.KeyID).Update("revoked_at", &now).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&store.Agent{}).Where("org_id = ? and fallback_id = ?", org, c.ID).Update("fallback_id", 0).Error; err != nil {
			return err
		}
		if err := tx.Delete(&c).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, nil, actor, store.ActAgentRemove, &c.Name)
	})
}
