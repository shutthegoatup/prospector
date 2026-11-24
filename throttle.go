package main

import (
	"strings"
	"sync"
	"time"
)

// hashSlots caps how many password hashes run at once, so a burst of sign-in
// attempts can't take every core.
var hashSlots = make(chan struct{}, 4)

const (
	freeAttempts = 5
	maxLoginWait = time.Minute
	maxTracked   = 10000
)

// loginGuard slows repeated failed sign-ins to one email. It is keyed on the email
// alone, known or not, because behind a proxy every request shares an address.
type loginGuard struct {
	mu    sync.Mutex
	fails map[string]*failure
	now   func() time.Time
}

type failure struct {
	n     int
	until time.Time
}

func (g *loginGuard) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

func loginKey(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

func (g *loginGuard) wait(email string) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	if f := g.fails[loginKey(email)]; f != nil {
		return max(0, f.until.Sub(g.clock()))
	}
	return 0
}

// record doubles the wait after each failure past the free ones, up to maxLoginWait:
// long enough to stop guessing, short enough that nobody can be locked out for long.
func (g *loginGuard) record(email string, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	key := loginKey(email)
	if ok {
		delete(g.fails, key)
		return
	}
	if g.fails == nil || len(g.fails) >= maxTracked {
		g.fails = map[string]*failure{}
	}
	f := g.fails[key]
	if f == nil {
		f = &failure{}
		g.fails[key] = f
	}
	f.n++
	if over := f.n - freeAttempts; over >= 0 {
		f.until = g.clock().Add(min(maxLoginWait, time.Second<<min(over, 10)))
	}
}
