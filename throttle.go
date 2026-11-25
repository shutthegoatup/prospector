package main

import (
	"crypto/hmac"
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
	forgetAfter  = 15 * time.Minute
	maxTracked   = 10000
)

// loginGuard slows repeated failed sign-ins to one email. It is keyed on the email
// alone, known or not, because behind a proxy every request shares an address. That
// would let someone guessing at an address keep its owner waiting too, so a browser
// that has signed in before is counted on its own (see deviceCookie).
type loginGuard struct {
	mu    sync.Mutex
	fails map[string]*failure
	now   func() time.Time
}

type failure struct {
	n           int
	last, until time.Time
}

func (g *loginGuard) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

// loginKey is a hash so that a huge email field can't be held in memory.
func loginKey(email string) string { return hashToken(strings.ToLower(strings.TrimSpace(email))) }

// loginWait doubles with each failure past the free ones, up to maxLoginWait.
func loginWait(failures int) time.Duration {
	if failures < freeAttempts {
		return 0
	}
	return min(maxLoginWait, time.Second<<min(failures-freeAttempts, 10))
}

func (g *loginGuard) wait(email string) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	if f := g.fails[loginKey(email)]; f != nil {
		return max(0, f.until.Sub(g.clock()))
	}
	return 0
}

// begin returns how long this attempt must wait, or 0 to let it through. Letting one
// through books the wait a failure would earn, so attempts made in parallel are refused.
func (g *loginGuard) begin(email string) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	f := g.fails[loginKey(email)]
	if f == nil {
		return 0
	}
	now := g.clock()
	if now.Sub(f.last) > forgetAfter {
		f.n = 0
	}
	if wait := f.until.Sub(now); wait > 0 {
		return wait
	}
	f.until = now.Add(loginWait(f.n + 1))
	return 0
}

func (g *loginGuard) record(email string, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	key := loginKey(email)
	if ok {
		delete(g.fails, key)
		return
	}
	now := g.clock()
	f := g.fails[key]
	if f == nil {
		if len(g.fails) >= maxTracked {
			g.dropIdle(now)
		}
		if len(g.fails) >= maxTracked {
			return
		}
		if g.fails == nil {
			g.fails = map[string]*failure{}
		}
		f = &failure{}
		g.fails[key] = f
	}
	f.n++
	f.last = now
	f.until = now.Add(loginWait(f.n))
}

// dropIdle makes room by forgetting emails that aren't being made to wait, which
// costs them nothing a new guesser doesn't already get.
func (g *loginGuard) dropIdle(now time.Time) {
	for key, f := range g.fails {
		if !f.until.After(now) {
			delete(g.fails, key)
		}
	}
}

const (
	deviceCookie = "device"
	deviceLife   = 365 * 24 * time.Hour
)

// A device token is what a browser keeps after signing in: a random value and its
// MAC under the account's password hash. Only this server can make one, it names
// nobody, and it stops working when the password changes. It isn't signed under the
// server's secret key: whoever can read the password hash could make one, but all it
// buys is being counted apart from other guessers, and a year of browsers would
// otherwise be forgotten the day a key is set.
func newDeviceToken(u *User) string {
	nonce := randomToken(16)
	return nonce + "." + deviceMAC(u, nonce)
}

func deviceMAC(u *User, nonce string) string {
	return mac([]byte(u.PasswordHash), "device\x00"+nonce)
}

// knownDevice reports whether token was issued to this email's account, and if so
// the name its attempts are counted under: the email and the device together, so
// guesses made elsewhere don't hold this browser up, and its own are still slowed.
func (s *Store) knownDevice(email, token string) (who string, ok bool) {
	nonce, signature, found := strings.Cut(token, ".")
	if !found || len(token) > 200 {
		return "", false
	}
	u, err := s.UserByEmail(email)
	if err != nil || !hmac.Equal([]byte(signature), []byte(deviceMAC(u, nonce))) {
		return "", false
	}
	return signInBucket(u.Email) + "\x00" + nonce, true
}

// The guard counts under names that say what is being guessed at, so the sign-in
// form, whatever is typed into it, can't reach the count for anything else.
func signInBucket(email string) string {
	return "sign-in\x00" + strings.ToLower(strings.TrimSpace(email))
}

func changeBucket(u *User) string { return "change\x00" + u.Email }
