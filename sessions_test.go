package main

import (
	"testing"
	"time"
)

func TestSigningInClearsSessionsThatHaveExpired(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	alpha := mustOrg(t, s, "alpha")
	ada := mustUser(t, s, alpha, "ada@alpha.example", "Ada Lovelace", "owner")
	grace := mustUser(t, s, alpha, "grace@alpha.example", "Grace Hopper", "member")
	lapsed := must(s.NewSession(grace.ID, 0))
	open := must(s.NewSession(grace.ID, 0))
	if err := s.DB.Model(&Session{}).Where("token_hash = ?", s.Keyring.Lookup(sessions, lapsed)).
		Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}

	must(s.NewSession(ada.ID, 0))
	var left int64
	if s.DB.Model(&Session{}).Where("token_hash = ?", s.Keyring.Lookup(sessions, lapsed)).Count(&left); left != 0 {
		t.Fatal("a session that expired was kept after someone signed in")
	}
	if _, err := s.SessionUser(open); err != nil {
		t.Fatalf("a session still open went with the expired ones: %v", err)
	}
}
