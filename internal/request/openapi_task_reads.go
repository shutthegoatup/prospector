package request

import "github.com/shutthegoatup/prospector/internal/store"

// Worker is who reads or works a task through the API: a key as the agent it is, or a
// person signed in, who claims nothing by reading.
func Worker(q *Req) store.Actor {
	if q.Key != nil {
		return q.Agent()
	}
	return store.Actor{Name: q.Actor(), ReadOnly: true}
}
