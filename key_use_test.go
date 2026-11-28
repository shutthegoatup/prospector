package main

import (
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/internal/store"
)

// TestAKeysLastUseIsWrittenOnceAMinute: a key busy all day doesn't turn every read
// into a write, and its last use is still never more than a minute behind.
func TestAKeysLastUseIsWrittenOnceAMinute(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	secret, key, err := s.CreateKey(o.ID, "bot", []string{"read"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	lastUse := func() time.Time {
		t.Helper()
		var k store.APIKey
		if err := s.DB.First(&k, key.ID).Error; err != nil || k.LastUsedAt == nil {
			t.Fatalf("no last use: %+v %v", k, err)
		}
		return *k.LastUsedAt
	}
	setLastUse := func(at time.Time) {
		t.Helper()
		if err := s.DB.Model(&store.APIKey{}).Where("id = ?", key.ID).Update("last_used_at", at).Error; err != nil {
			t.Fatal(err)
		}
	}

	must(s.KeyFor(secret))
	first := lastUse()

	recent := time.Now().Add(-30 * time.Second).Truncate(time.Microsecond)
	setLastUse(recent)
	must(s.KeyFor(secret))
	if got := lastUse(); !got.Equal(recent) {
		t.Errorf("used again within the minute, its last use was written: %v, was %v", got, recent)
	}

	stale := time.Now().Add(-2 * time.Minute).Truncate(time.Microsecond)
	setLastUse(stale)
	must(s.KeyFor(secret))
	if got := lastUse(); !got.After(stale.Add(time.Minute)) {
		t.Errorf("used after a minute, its last use stayed %v (first %v)", got, first)
	}
}
