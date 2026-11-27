package main

import (
	"testing"
	"time"
)

func walkRuns(t *testing.T, s *Store, org int64, limit int) []Run {
	var all []Run
	before := time.Time{}
	for i := 0; i < 50; i++ {
		runs, _, next := s.Runs(org, before, limit)
		all = append(all, runs...)
		if next.IsZero() {
			break
		}
		before = next
	}
	return all
}

func TestARunLongerThanAPageReadsIsShownNewestFirst(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	start := time.Now().Add(-100 * time.Hour).Truncate(time.Second)
	var ev []Event
	for j := range 2500 {
		ev = append(ev, Event{OrgID: o.ID, User: "agent:a", KeyID: 1, Action: "add:fact", At: start.Add(time.Duration(j) * time.Minute)})
	}
	for i := range 3 {
		ev = append(ev, Event{OrgID: o.ID, User: "agent:b", KeyID: 2, Action: "add:fact", At: start.Add(2000*time.Minute + time.Duration(i)*2*time.Hour)})
	}
	mustDone(t, s.DB.CreateInBatches(ev, 200).Error)
	long, short := 0, 0
	for _, r := range walkRuns(t, s, o.ID, 12) {
		switch r.Actor {
		case "agent:a":
			long++
		case "agent:b":
			short++
		}
	}
	// The long one is shown once, cut to what a page reads; the short ones within it all.
	if long != 1 || short != 3 {
		t.Fatalf("the long run was shown %d times and the short ones %d, want 1 and 3", long, short)
	}
}

func TestRunsThatEndedTogetherShareAPage(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	at := time.Now().Add(-10 * time.Hour).Truncate(time.Second)
	ev := []Event{
		{OrgID: o.ID, User: "agent:a", KeyID: 1, Action: "add:fact", At: at.Add(-time.Minute)},
		{OrgID: o.ID, User: "agent:a", KeyID: 1, Action: "add:fact", At: at},
		{OrgID: o.ID, User: "agent:b", KeyID: 2, Action: "add:fact", At: at.Add(-time.Minute)},
		{OrgID: o.ID, User: "agent:b", KeyID: 2, Action: "add:fact", At: at},
	}
	mustDone(t, s.DB.Create(&ev).Error)
	if n := len(walkRuns(t, s, o.ID, 1)); n != 2 {
		t.Fatalf("walked %d runs, want 2", n)
	}
}

func TestWalkingRunsCarriesOnPastALongOne(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	start := time.Now().Add(-200 * time.Hour).Truncate(time.Second)
	var ev []Event
	// an old run, then a long one of 4500 events
	ev = append(ev, Event{OrgID: o.ID, User: "agent:a", KeyID: 1, Action: "add:fact", At: start})
	for j := range 4500 {
		ev = append(ev, Event{OrgID: o.ID, User: "agent:a", KeyID: 1, Action: "add:fact", At: start.Add(2*time.Hour + time.Duration(j)*time.Minute)})
	}
	mustDone(t, s.DB.CreateInBatches(ev, 200).Error)
	all := walkRuns(t, s, o.ID, 12)
	for _, r := range all {
		if r.Changes == 1 {
			return
		}
	}
	t.Fatal("the old run was never reached")
}

func mustDone(t *testing.T, err error) {
	if err != nil {
		t.Fatal(err)
	}
}
