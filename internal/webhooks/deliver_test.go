package webhooks

import (
	"testing"
	"time"
)

// TestASignatureIsStandardWebhooks checks Sign against the example in the Standard
// Webhooks spec, so a receiver's library of it accepts what is sent.
func TestASignatureIsStandardWebhooks(t *testing.T) {
	t.Parallel()
	got, err := Sign("whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw", "msg_p5jXN8AQM9LWM0D4loKWxJek", time.Unix(1614265330, 0), []byte(`{"test": 2432232314}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE="; got != want {
		t.Fatalf("Sign = %s, want %s", got, want)
	}
	if _, err := Sign("whsec_not base64!", "msg_1", time.Now(), nil); err == nil {
		t.Fatal("a secret that isn't one signed")
	}
}

// TestRetriesSpreadOut: further apart each time, a tenth either way, and never sooner
// than the endpoint asked.
func TestRetriesSpreadOut(t *testing.T) {
	t.Parallel()
	at := time.Unix(0, 0)
	for _, c := range []struct {
		n      int
		jitter float64
		asked  time.Duration
		want   time.Duration
	}{
		{1, 0.5, 0, 5 * time.Second},
		{2, 0.5, 0, 5 * time.Minute},
		{6, 0.5, 0, 10 * time.Hour},
		{9, 0.5, 0, 10 * time.Hour},
		{2, 0, 0, 270 * time.Second},
		{2, 1, 0, 330 * time.Second},
		{1, 0.5, time.Hour, time.Hour},
	} {
		d := &Deliverer{Jitter: func() float64 { return c.jitter }}
		if got := d.next(c.n, at, c.asked).Sub(at); got != c.want {
			t.Errorf("after attempt %d (jitter %v, asked %v): %v, want %v", c.n, c.jitter, c.asked, got, c.want)
		}
	}
}
