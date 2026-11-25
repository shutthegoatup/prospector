package main

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTwoAgentsOnATaskTakeDifferentRequests(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout, second := Actor{Name: "agent:scout", KeyID: 1}, Actor{Name: "agent:second", KeyID: 2}
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders with a configurator.", "Ada Lovelace"))
	now := time.Now()

	first := must(s.agentTask(o.ID, scout, task.ID, now))
	brief := first.Requests[0].ID
	if first.Claimed != brief || !first.Requests[0].Yours || first.Status != TaskWorking {
		t.Fatalf("the first agent should be kept the brief: %+v", first)
	}
	got := must(s.agentTask(o.ID, second, task.ID, now.Add(time.Minute)))
	if got.Claimed != 0 || got.Requests[0].HeldBy != "agent:scout" || got.Requests[0].Yours || got.Requests[0].Until == nil {
		t.Fatalf("the second agent should see the brief is the first's: %+v", got)
	}
	if list := must(s.AgentTasks(o.ID, second)); list[0].Status != TaskWorking || list[0].Open[0].HeldBy != "agent:scout" {
		t.Fatalf("list_tasks should say who holds the brief: %+v", list[0])
	}

	if err := s.AskTask(o.ID, task.ID, "Find ten more in Italy.", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	got = must(s.agentTask(o.ID, second, task.ID, now.Add(2*time.Minute)))
	more := got.Requests[1].ID
	if got.Claimed != more || !got.Requests[1].Yours || got.Requests[0].HeldBy != "agent:scout" {
		t.Fatalf("the second agent should take the new request: %+v", got)
	}
	if again := must(s.agentTask(o.ID, scout, task.ID, now.Add(3*time.Minute))); again.Claimed != brief {
		t.Fatalf("the first agent reading the task again should keep what it had, got %d", again.Claimed)
	}

	// A key under a name another key had is a different agent.
	renamed := Actor{Name: "agent:scout", KeyID: 9}
	if got := must(s.agentTask(o.ID, renamed, task.ID, now.Add(4*time.Minute))); got.Claimed != 0 || got.Requests[0].Yours {
		t.Fatalf("a new key took an old key's claim because they share a name: %+v", got)
	}

	// A key that can only read takes nothing.
	reader := Actor{Name: "agent:reader", KeyID: 3, ReadOnly: true}
	if err := s.SetRequestDone(o.ID, more, true, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if err := s.AskTask(o.ID, task.ID, "And Spain.", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if got := must(s.agentTask(o.ID, reader, task.ID, now.Add(5*time.Minute))); got.Claimed != 0 || got.Requests[2].HeldBy != "" {
		t.Fatalf("a read-only key was kept a request: %+v", got.Requests[2])
	}
	var claims []RequestClaim
	s.db.Where("org_id = ?", o.ID).Order("request_id").Find(&claims)
	if len(claims) != 1 || claims[0].RequestID != brief {
		t.Fatalf("finishing a request should end its claim, and reading take none: %+v", claims)
	}
}

func TestAClaimLapsesUnlessTheHolderKeepsAdding(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout, second := Actor{Name: "agent:scout", KeyID: 1}, Actor{Name: "agent:second", KeyID: 2}
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders.", "Ada Lovelace"))
	start := time.Now()
	brief := must(s.agentTask(o.ID, scout, task.ID, start)).Claimed

	expires := func() time.Time {
		var c RequestClaim
		if err := s.db.First(&c, "request_id = ?", brief).Error; err != nil {
			t.Fatal(err)
		}
		return time.UnixMilli(c.ExpiresAt)
	}
	// Ten minutes in, the holder adds a company for the task, which keeps the claim.
	if err := s.db.Model(&RequestClaim{}).Where("request_id = ?", brief).Update("expires_at", time.Now().Add(5*time.Minute).UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Task: task.ID, Name: "Larkmoor Yachts"}))
	if left := time.Until(expires()); left < claimFor-time.Minute {
		t.Fatalf("a write for the task should renew the claim, %s left", left)
	}
	// Another agent's write for the task leaves the claim where it was.
	before := expires()
	must(s.AgentAddCompany(o.ID, second, AddCompanyIn{Task: task.ID, Name: "Corvane"}))
	if !expires().Equal(before) {
		t.Fatal("another agent's write moved the holder's claim")
	}

	// Once it has lapsed the request is free for whoever picks the task up next.
	later := time.Now().Add(claimFor + time.Minute)
	got := must(s.agentTask(o.ID, second, task.ID, later))
	if got.Claimed != brief || !got.Requests[0].Yours {
		t.Fatalf("a lapsed claim should pass to the next agent: %+v", got)
	}
	if mine := must(s.agentTask(o.ID, scout, task.ID, later)); mine.Claimed != 0 || mine.Requests[0].HeldBy != "agent:second" {
		t.Fatalf("the agent that let it lapse should see it taken: %+v", mine)
	}
	if v := must(s.Task(o.ID, task.ID, later.Add(claimFor+runGap))); v.Status != TaskWaiting || v.Requests[0].HeldBy != "" {
		t.Fatalf("a task nobody has touched since its claim lapsed should be waiting: %s %+v", v.Status, v.Requests[0])
	}
}

func TestOnlyOneOfManyAgentsWinsARequest(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders.", "Ada Lovelace"))
	request := must(s.Task(o.ID, task.ID, time.Now())).Requests[0].ID
	now := time.Now()
	const agents = 8
	won := make([]bool, agents)
	errs := make([]error, agents)
	var wg sync.WaitGroup
	for i := range agents {
		wg.Add(1)
		go func() {
			defer wg.Done()
			won[i], errs[i] = s.claim(o.ID, task.ID, request, Actor{Name: "agent:" + strings.Repeat("x", i+1), KeyID: int64(i + 1)}, now)
		}()
	}
	wg.Wait()
	winners := 0
	for i := range agents {
		if errs[i] != nil {
			t.Fatalf("agent %d: %v", i, errs[i])
		}
		if won[i] {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("%d agents claimed the same request at once", winners)
	}
	var n int64
	if s.db.Model(&RequestClaim{}).Count(&n); n != 1 {
		t.Fatalf("%d claim rows", n)
	}
}
