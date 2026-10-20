package models

import (
	"testing"
	"time"
)

func TestCounterBaselinePrefersLastVisit(t *testing.T) {
	a := &Actor{}
	if a.CounterBaseline() != nil {
		t.Fatal("baseline should be nil when the actor was never seen")
	}

	first := time.Unix(1000, 0).UTC()
	a.FirstSeenAt = &first
	if got := a.CounterBaseline(); got == nil || !got.Equal(first) {
		t.Fatalf("baseline = %v, want the activation moment", got)
	}

	last := time.Unix(2000, 0).UTC()
	a.LastSeenAt = &last
	if got := a.CounterBaseline(); got == nil || !got.Equal(last) {
		t.Fatalf("baseline = %v, want the previous visit", got)
	}
}

func TestActorExpired(t *testing.T) {
	now := time.Unix(5000, 0).UTC()
	a := &Actor{}
	if a.Expired(now) {
		t.Fatal("actor without expires_at must never be expired")
	}
	past := time.Unix(4000, 0).UTC()
	a.ExpiresAt = &past
	if !a.Expired(now) {
		t.Fatal("actor with a past expires_at must be expired")
	}
	future := time.Unix(6000, 0).UTC()
	a.ExpiresAt = &future
	if a.Expired(now) {
		t.Fatal("actor with a future expires_at must not be expired")
	}
}
