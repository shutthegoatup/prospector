package main

import (
	"testing"
	"time"
)

// Runs are worked out from events, so a page of them ends where a run does, and a run
// the page before showed isn't shown again in part on the next.
func TestRecentRunsHavePages(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	start := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	var events []Event
	// Thirty runs for two agents in turn, an hour apart, each of three changes a
	// minute apart: every page boundary cuts between two agents' interleaved work.
	for i := range 30 {
		agent, key := "agent:scout", int64(1)
		if i%2 == 1 {
			agent, key = "agent:second", 2
		}
		for j := range 3 {
			events = append(events, Event{OrgID: o.ID, User: agent, KeyID: key, Action: "add:fact",
				At: start.Add(time.Duration(i)*time.Hour + time.Duration(j)*time.Minute)})
		}
	}
	if err := s.DB.CreateInBatches(events, 50).Error; err != nil {
		t.Fatal(err)
	}

	var all []Run
	before, pages := time.Time{}, 0
	for {
		runs, _, next := s.Runs(o.ID, before, 7)
		all, pages = append(all, runs...), pages+1
		if next.IsZero() || pages > 20 {
			break
		}
		before = next
	}
	changes := 0
	for i, r := range all {
		changes += r.Changes
		if r.Changes != 3 {
			t.Errorf("run %d of %s holds %d changes, want 3: a run was split across pages", i, r.Actor, r.Changes)
		}
		if i > 0 && !r.End.Before(all[i-1].End) {
			t.Fatalf("runs aren't newest first across pages: %v after %v", r.End, all[i-1].End)
		}
	}
	if len(all) != 30 || changes != len(events) || pages != 5 {
		t.Fatalf("walking runs seven at a time: %d runs, %d of %d changes, %d pages", len(all), changes, len(events), pages)
	}

	// A run the page before ended with began earlier: its start isn't shown as a run of its own.
	long := Event{OrgID: o.ID, User: "agent:third", KeyID: 3, Action: "add:fact", At: start.Add(100 * time.Hour)}
	more := []Event{long}
	for j := 1; j < 20; j++ {
		e := long
		e.At = long.At.Add(time.Duration(j) * 10 * time.Minute)
		more = append(more, e)
	}
	if err := s.DB.CreateInBatches(more, 50).Error; err != nil {
		t.Fatal(err)
	}
	first, _, next := s.Runs(o.ID, time.Time{}, 1)
	if len(first) != 1 || first[0].Changes != 20 || next.IsZero() {
		t.Fatalf("the latest run: %+v, next %v", first, next)
	}
	// Ask for the page before a moment inside that run, as a stale link might.
	inside := long.At.Add(95 * time.Minute)
	for _, r := range must(runsOnly(s.Runs(o.ID, inside, 50))) {
		if r.Actor == "agent:third" {
			t.Fatalf("the start of a run that carries on past the page was shown as a run: %+v", r)
		}
	}

	// The Agents page walks the same stretches through the API: each whole on one page,
	// newest first, the long one that carries on first.
	var work []map[string]any
	for _, pg := range apiPages(t, client, srv.URL+"/o/alpha/api/v1/agent-work?limit=7") {
		work = append(work, pg.Items...)
	}
	if len(work) != 31 || work[0]["actor"] != "agent:third" || work[0]["changes"] != float64(20) {
		t.Fatalf("the agents' work through the API: %d stretches, first %v", len(work), work[0])
	}
	for i, w := range work[1:] {
		if w["changes"] != float64(3) {
			t.Errorf("stretch %d holds %v changes, want 3: one was split across pages", i+1, w["changes"])
		}
	}
}

// A page is worked out from at most runsRead events; the run they cut short is left
// for the next page, where it is whole.
func TestARunCutShortByWhatIsReadWaitsForTheNextPage(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	const runs, each = 40, 60
	start := time.Now().Add(-200 * time.Hour).Truncate(time.Second)
	var events []Event
	for i := range runs {
		for j := range each {
			events = append(events, Event{OrgID: o.ID, User: "agent:scout", KeyID: 1, Action: "add:fact",
				At: start.Add(time.Duration(i)*2*time.Hour + time.Duration(j)*time.Minute)})
		}
	}
	if len(events) <= runsRead {
		t.Fatalf("the test needs more than %d events", runsRead)
	}
	if err := s.DB.CreateInBatches(events, 200).Error; err != nil {
		t.Fatal(err)
	}
	seen, before, pages := 0, time.Time{}, 0
	for {
		page, _, next := s.Runs(o.ID, before, runs)
		for _, r := range page {
			if r.Changes != each {
				t.Fatalf("a run of %d changes on page %d, want %d", r.Changes, pages+1, each)
			}
		}
		seen, pages = seen+len(page), pages+1
		if next.IsZero() || pages > 10 {
			break
		}
		before = next
	}
	if seen != runs {
		t.Fatalf("walked %d runs in %d pages, want %d", seen, pages, runs)
	}
}

// runsOnly keeps the runs of a page, for must.
func runsOnly(runs []Run, _ map[int64]string, _ time.Time) ([]Run, error) { return runs, nil }
