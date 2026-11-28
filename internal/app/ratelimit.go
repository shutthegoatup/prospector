package app

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// A token bucket per caller: a burst of requests goes straight through, after which they
// are let through at the steady rate. A browser loads several things at once on each
// page, and a person clicks faster than a script polls, so members get more.
type rateLimit struct {
	perSecond rate.Limit
	burst     int
}

var (
	keyRate    = rateLimit{100, 1000}
	memberRate = rateLimit{200, 2000}
)

// rateGuard holds the buckets. serve turns it on; off, the tests can make as many
// requests as they need.
type rateGuard struct {
	on      bool
	mu      sync.Mutex
	buckets map[string]*rate.Limiter
	now     func() time.Time
}

func (g *rateGuard) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

// take spends a token from who's bucket, or says how long until there is one. A refused
// request spends nothing, so a caller that waits as told gets through.
func (g *rateGuard) take(who string, limit rateLimit) time.Duration {
	if !g.on {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.clock()
	b := g.buckets[who]
	if b == nil {
		if len(g.buckets) >= maxTracked {
			g.dropFull(now)
		}
		if g.buckets == nil {
			g.buckets = map[string]*rate.Limiter{}
		}
		b = rate.NewLimiter(limit.perSecond, limit.burst)
		// Past the cap a caller still gets a bucket, just one that isn't kept.
		if len(g.buckets) < maxTracked {
			g.buckets[who] = b
		}
	}
	res := b.ReserveN(now, 1)
	wait := res.DelayFrom(now)
	if wait > 0 {
		res.CancelAt(now)
	}
	return wait
}

// dropFull forgets buckets that have filled up again: a new one starts just as full.
func (g *rateGuard) dropFull(now time.Time) {
	for who, b := range g.buckets {
		if b.TokensAt(now) >= float64(b.Burst()) {
			delete(g.buckets, who)
		}
	}
}

func keyBucket(k *store.APIKey) string { return "key\x00" + strconv.FormatInt(k.ID, 10) }
func userBucket(u *store.User) string  { return "user\x00" + strconv.FormatInt(u.ID, 10) }

// limited lets the request on, or refuses it for the whole seconds Retry-After takes.
func (a *App) limited(w http.ResponseWriter, r *http.Request, who string, limit rateLimit) bool {
	wait := a.rates.take(who, limit)
	if wait <= 0 {
		return true
	}
	seconds := int(math.Ceil(wait.Seconds()))
	apiFail(w, r, typed{http.StatusTooManyRequests, api.ProblemTypeThrottled, say.Errorf("too many requests: try again in %d seconds", seconds), seconds})
	return false
}
