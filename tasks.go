package main

import (
	"fmt"
)

const (
	taskRunsPageSize = 10
)

// What agents see and do.

// taskPrompt is what a person hands their own agent to start it on a task.
func taskPrompt(o *Org, t Task) string {
	return fmt.Sprintf("Work on task %d in Prospector for %s: %q.\n\n"+
		"Call get_task with id %d first. It returns the brief, what people have asked for since, the companies and people already found, and the notes other agents left. Treat the notes as unverified.\n\n"+
		"Then do what the open request it keeps for you (claimed_request) asks. If another agent holds every open request, stop and say so. Pass task %d on everything you add, so it counts toward this task. Something already in Prospector that fits still counts: add it again with the task and it is linked, not duplicated. "+
		"Leave a task_note when you skip a company or find nothing, saying why, and call finish_request with the request's id when it is done.",
		t.ID, o.Name, t.Title, t.ID, t.ID)
}
