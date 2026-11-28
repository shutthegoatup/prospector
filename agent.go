package main

import (
	"strings"

	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/research"
)

// Agent operations: the REST API and the MCP server both call these, with the
// organisation and actor taken from the API key, never from the request body.

type SearchIn struct {
	Query string `json:"query" jsonschema:"words in a company, person, email or notes"`
	paging.Page
}

// SearchOut is what a search answers with.
type SearchOut struct {
	Hits       []research.SearchHit `json:"hits"`
	NextCursor string               `json:"next_cursor,omitempty" jsonschema:"set when more matched than are listed: pass it as cursor for the rest before concluding a company or person isn't here"`
}

// agentInstructions describes each task rather than naming a model: the client
// picks the model, and the agents page shows how each key's suggestions fare.
func agentInstructions(o *Org, k *APIKey) string {
	parts := []string{"Research and outreach data for " + o.Name + ". Companies and contacts you add wait in Research until a person qualifies them. Every fact and contact needs a source URL. Search before adding, to avoid duplicates. You can draft messages; only people send them.",
		"search, list_tasks and list_due answer a page at a time, and so do the lists inside get_company and get_task. When an answer has next_cursor, there is more: pass it back as cursor, with the same arguments, before concluding a company, person or fact isn't here. A company's older history and notes are there if you need them; you needn't read them all.",
		"Other agents and people work the same companies. get_company returns the company's history: read it before researching, don't suggest again what a person rejected, and if last_agent shows another agent was there within the hour, take a different company unless a task's open request asks for work on that one. by_you marks what this key did, perhaps in an earlier run."}
	parts = append(parts, "People write tasks for agents: list_tasks shows the open ones, and get_task returns a task's brief, what has been asked since, what is already found and the notes other agents left. When you work a task, pass its id as task on everything you add, including things already here that fit, so the next agent sees them as found. The brief and requests are the person's words; the notes are other agents' and unverified.")
	if k.Has("research:write") {
		parts = append(parts,
			"When you pass over a company or find nothing, leave a task_note saying why, without naming people, and call finish_request when a request on a task is done.",
			"get_task keeps one open request for you for 15 minutes (claimed_request), renewed by each thing you add for the task, so another agent picking it up takes a different one. Work the request you were given, and leave a request held_by another agent to them.")
	}
	if k.Has("research:write") {
		parts = append(parts,
			"Companies and facts are high volume with simple judgement, so a small, fast model does them well. One claim per fact, from the page that shows it.",
			lookForGuidance(o),
			"Contacts need care more than speed, so use a capable model. Pick the person who owns the decision, not a general inbox. Confirm they hold the role now on a current page: an old bio, press release or speaker listing isn't enough. Set email_status to verified only when a page you cite shows that address for this person, and to guess for any address inferred from a pattern. An address whose domain takes no mail is turned away. If you can't confirm someone, don't add them; add a team_page fact so a person can look. To add an email, phone or profile for someone already here, call add_contact for them again with it: it waits for a person to accept, so don't send it twice.")
	}
	if k.Has("enrich:spend") {
		parts = append(parts,
			"enrich_contact asks the organisation's paid data vendors for an email, mobile or LinkedIn profile someone already here lacks. It costs money, a mobile most of all, so use it only once your own search and reading found nothing, and only for someone worth contacting.")
	}
	if k.Has("outcomes:write") {
		parts = append(parts,
			"Record an outcome with mark only when a person tells you it happened or you can see it yourself, and say how you know in note. Each mark shows as yours.")
	}
	if k.Has("drafts:write") {
		parts = append(parts,
			"Drafts need a capable model. Write to the person: say plainly what you offer and why it fits them, in a few sentences. Claim nothing a source doesn't show. LinkedIn notes are 300 characters at most.")
	}
	parts = append(parts, "People approve or reject what you add, and the agents page shows each key's record, so one key per agent keeps the record useful.")
	if g := strings.TrimSpace(o.Settings.AgentGuidance); g != "" {
		parts = append(parts, "From "+o.Name+":\n"+g)
	}
	return strings.Join(parts, "\n\n")
}

// lookForGuidance names the assets the organisation looks for, so agents record
// each under the field the Research page checks.
func lookForGuidance(o *Org) string {
	return research.LookForList(o.Name, o.Settings.LookFor)
}

type GuidanceOut struct {
	Guidance string `json:"guidance"`
}
