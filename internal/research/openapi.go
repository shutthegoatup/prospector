package research

import (
	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/store"
)

func RunOut(r store.AgentRun) api.Run {
	return api.Run{Id: r.ID, Task: r.TaskID, Agent: r.AgentID, AgentName: r.AgentName, Request: r.RequestID, StartedBy: r.StartedBy, Model: r.Model,
		Status: api.RunStatus(r.Status), StopAsked: r.StopAsked, StartedAt: r.StartedAt, EndedAt: r.EndedAt, WaitUntil: r.WaitUntil, Turns: r.Turns,
		Episodes: r.Episodes, ToolCalls: r.ToolCalls, Added: r.Added, Resumes: r.Resumes, TokensIn: r.TokensIn, TokensOut: r.TokensOut, StopReason: r.StopReason}
}
