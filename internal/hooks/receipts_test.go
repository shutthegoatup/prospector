package hooks

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// numbered is a fake provider whose events carry an id, counting what it handles.
type numbered struct {
	fake
	handled int
	fail    bool
}

func (n *numbered) EventID(body []byte) string { return string(body) }

func (n *numbered) Handle(context.Context, int64, *http.Request, []byte) error {
	if n.fail {
		return errors.New("the database is away")
	}
	n.handled++
	return nil
}

// remembered are receipts kept in memory, as the store keeps them: none for a handle
// that failed.
type remembered map[string]bool

func (m remembered) Receive(_ int64, provider, event string, handle func() error) (bool, error) {
	if m[provider+event] {
		return true, nil
	}
	if err := handle(); err != nil {
		return false, err
	}
	m[provider+event] = true
	return false, nil
}

// TestAnEventPostedAgainIsTakenOnce: the same id twice is handled once and answered
// alike; one whose handling failed is handled when it comes again.
func TestAnEventPostedAgainIsTakenOnce(t *testing.T) {
	t.Parallel()
	p := &numbered{}
	h := Handler(map[string]Provider{"n": p}, remembered{})
	for range 2 {
		if code := post(t, h, "n", true, []byte("evt_1")); code != http.StatusNoContent {
			t.Fatalf("%d", code)
		}
	}
	if p.handled != 1 {
		t.Fatalf("handled %d times", p.handled)
	}
	p.fail = true
	if code := post(t, h, "n", true, []byte("evt_2")); code != http.StatusServiceUnavailable {
		t.Fatalf("a failure: %d", code)
	}
	p.fail = false
	if code := post(t, h, "n", true, []byte("evt_2")); code != http.StatusNoContent || p.handled != 2 {
		t.Fatalf("sent again after a failure: %d, handled %d", code, p.handled)
	}
}
