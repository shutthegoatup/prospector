package app

import (
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/internal/store"
)

// TestANewHookSecretOverlapsTheOld: a new secret signs in at once, the one it replaces
// for a day more, then only the new one.
func TestANewHookSecretOverlapsTheOld(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	signs := func(secret string) bool {
		t.Helper()
		_, ok := s.BounceHookOrg("alpha", secret)
		return ok
	}
	first := must(s.NewBounceHook(o.ID, "Ada Lovelace"))
	second := must(s.NewBounceHook(o.ID, "Ada Lovelace"))
	if !signs(first) || !signs(second) || signs("a guess") {
		t.Fatalf("while they overlap: first %v, second %v", signs(first), signs(second))
	}
	var keys []store.HookKey
	s.DB.Where("org_id = ?", o.ID).Order("id").Find(&keys)
	if len(keys) != 2 || keys[0].ExpiresAt == nil || keys[1].ExpiresAt != nil || keys[0].Kid == keys[1].Kid {
		t.Fatalf("the key set: %+v", keys)
	}
	if d := time.Until(*keys[0].ExpiresAt); d < store.HookKeyOverlap-time.Minute || d > store.HookKeyOverlap {
		t.Fatalf("the old one lives on for %v", d)
	}
	s.DB.Model(&keys[0]).Update("expires_at", time.Now().Add(-time.Second))
	if signs(first) || !signs(second) {
		t.Fatalf("once the overlap is over: first %v, second %v", signs(first), signs(second))
	}
	if !s.HasBounceHook(o.ID) {
		t.Fatal("the hook is gone while a secret signs in")
	}
	// Stopping it stops every secret.
	if err := s.DropBounceHook(o.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if signs(second) || s.HasBounceHook(o.ID) {
		t.Fatal("a stopped hook still signs in")
	}
}

// TestAHookFromBeforeTheKeySetKeepsWorking: one made by the release before has only its
// bounce_hooks row; it signs in, and overlaps the first new secret like any other.
func TestAHookFromBeforeTheKeySetKeepsWorking(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	old := store.RandomToken(32)
	if err := s.DB.Create(&store.BounceHook{OrgID: o.ID, SecretHash: store.HashToken(old), CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if _, ok := s.BounceHookOrg("alpha", old); !ok || !s.HasBounceHook(o.ID) {
		t.Fatal("a hook from before doesn't sign in")
	}
	fresh := must(s.NewBounceHook(o.ID, "Ada Lovelace"))
	_, oldOK := s.BounceHookOrg("alpha", old)
	_, freshOK := s.BounceHookOrg("alpha", fresh)
	if !oldOK || !freshOK {
		t.Fatalf("after the first new secret: old %v, new %v", oldOK, freshOK)
	}
	// The release before reads bounce_hooks, so a rollback finds the new secret there.
	var h store.BounceHook
	s.DB.Where("org_id = ?", o.ID).First(&h)
	if h.SecretHash != store.HashToken(fresh) {
		t.Fatal("bounce_hooks isn't kept in step for a rollback")
	}
}
