package store

import "time"

// AgentRun is one go of Prospector's own agent at a task. It keeps counts, never what
// was said: the task and what its events point at are the record, as for any agent.
type AgentRun struct {
	ID        int64  `gorm:"primaryKey"`
	OrgID     int64  `gorm:"index;not null"`
	TaskID    int64  `gorm:"index;not null"`
	AgentID   int64  `gorm:"index;not null;default:0"`
	AgentName string `gorm:"not null;default:''"`
	RequestID int64  `gorm:"not null;default:0"`
	StartedBy string `gorm:"not null"`
	Model     string `gorm:"not null"`
	Status    string `gorm:"index;not null"`
	StopAsked bool   `gorm:"not null;default:false"`
	// Owner is the server process driving the run; LeaseUntil (Unix ms) is how long it
	// may before another takes over, which is how a run outlives a restart.
	Owner      string `gorm:"not null;default:''"`
	LeaseUntil int64  `gorm:"not null;default:0"`
	StartedAt  time.Time
	EndedAt    *time.Time
	WaitUntil  *time.Time
	Turns      int    `gorm:"not null;default:0"`
	Episodes   int    `gorm:"not null;default:0"`
	ToolCalls  int    `gorm:"not null;default:0"`
	Added      int    `gorm:"not null;default:0"`
	Resumes    int    `gorm:"not null;default:0"`
	TokensIn   int64  `gorm:"not null;default:0"`
	TokensOut  int64  `gorm:"not null;default:0"`
	StopReason string `gorm:"not null;default:''"`
}

const RunWorking = "running"

const RunWaiting = "waiting"

func (r AgentRun) Going() bool { return r.Status == RunWorking || r.Status == RunWaiting }

const BuiltinPrefix = "builtin"

// BuiltinKeyName is an agent's name as its key carries it into the record, marked so
// it isn't taken for a key someone made for an agent of their own.
func BuiltinKeyName(agent string) string { return agent + " (built in)" }
