package app

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/internal/store"
)

// TestTheSameEventPostedTwiceAtOnceIsTakenOnce: the second waits for the first's
// receipt, then sees it.
func TestTheSameEventPostedTwiceAtOnceIsTakenOnce(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	var handled atomic.Int32
	var wg sync.WaitGroup
	seen := make([]bool, 2)
	for i := range 2 {
		wg.Go(func() {
			var err error
			seen[i], err = s.Receive(o.ID, "postmark", "Bounce:7", func() error {
				handled.Add(1)
				time.Sleep(100 * time.Millisecond)
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if handled.Load() != 1 || seen[0] == seen[1] {
		t.Fatalf("handled %d times; seen %v", handled.Load(), seen)
	}
	// One whose handling failed leaves no receipt, so it is taken when it comes again.
	if _, err := s.Receive(o.ID, "postmark", "Bounce:8", func() error { return errors.New("the database is away") }); err == nil {
		t.Fatal("a failure wasn't said")
	}
	if seen, err := s.Receive(o.ID, "postmark", "Bounce:8", func() error { return nil }); seen || err != nil {
		t.Fatalf("after a failure: seen %v, %v", seen, err)
	}
	// Another organisation's same id is its own.
	beta := mustOrg(t, s, "beta")
	if seen, _ := s.Receive(beta.ID, "postmark", "Bounce:7", func() error { return nil }); seen {
		t.Fatal("another organisation's event was taken as this one's")
	}
}

// TestPostmarkPostingABounceAgainIsTakenOnce: the same record twice is answered alike
// and recorded once.
func TestPostmarkPostingABounceAgainIsTakenOnce(t *testing.T) {
	t.Parallel()
	_, s, o, list, _, _ := campaignSite(t)
	site := strings.TrimSuffix(list, "/o/alpha/api/v1/campaigns")
	secret := must(s.NewBounceHook(o.ID, "Ada Lovelace"))
	history := func() int64 {
		var n int64
		s.DB.Model(&store.Event{}).Where("org_id = ?", o.ID).Count(&n)
		return n
	}
	if code := postmark(t, site, "alpha", secret, bounce("HardBounce", "hannah@larkmoor.example")); code != http.StatusNoContent {
		t.Fatalf("the first: %d", code)
	}
	after := history()
	if code := postmark(t, site, "alpha", secret, bounce("HardBounce", "hannah@larkmoor.example")); code != http.StatusNoContent {
		t.Fatalf("again: %d", code)
	}
	if history() != after {
		t.Fatal("the same bounce was recorded twice")
	}
	var receipts []store.HookReceipt
	s.DB.Where("org_id = ?", o.ID).Find(&receipts)
	if len(receipts) != 1 || receipts[0].EventID != "Bounce:4242" {
		t.Fatalf("receipts: %+v", receipts)
	}
}
