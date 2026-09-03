package ratelimit

import (
	"sync"
	"testing"
	"time"
)

// @spec ACCT-017
func TestAllow_BurstThenDenyThenRefill(t *testing.T) {
	now := time.Unix(0, 0)
	l := New(10, 6*time.Second)
	l.now = func() time.Time { return now }

	for i := 0; i < 10; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("request %d within capacity was denied", i+1)
		}
	}
	if l.Allow("1.2.3.4") {
		t.Fatal("11th request in the burst was allowed, want denied")
	}

	// Not quite one interval later: still denied.
	now = now.Add(5 * time.Second)
	if l.Allow("1.2.3.4") {
		t.Fatal("request before one refill interval was allowed, want denied")
	}

	// One full interval past the last refill: exactly one token back.
	now = now.Add(1 * time.Second)
	if !l.Allow("1.2.3.4") {
		t.Fatal("request after a refill interval was denied, want allowed")
	}
	if l.Allow("1.2.3.4") {
		t.Fatal("second request after a single refill was allowed, want denied")
	}
}

// @spec ACCT-017
func TestAllow_KeysAreIndependent(t *testing.T) {
	now := time.Unix(0, 0)
	l := New(2, time.Minute)
	l.now = func() time.Time { return now }

	if !l.Allow("a") || !l.Allow("a") {
		t.Fatal("key a should have 2 tokens")
	}
	if l.Allow("a") {
		t.Fatal("key a should be exhausted")
	}
	if !l.Allow("b") || !l.Allow("b") {
		t.Fatal("key b should be unaffected by key a")
	}
}

// @spec ACCT-017
func TestAllow_RefillCapsAtCapacity(t *testing.T) {
	now := time.Unix(0, 0)
	l := New(3, time.Second)
	l.now = func() time.Time { return now }

	l.Allow("k") // 2 left
	now = now.Add(time.Hour)

	allowed := 0
	for i := 0; i < 10; i++ {
		if l.Allow("k") {
			allowed++
		}
	}
	if allowed != 3 {
		t.Fatalf("after a long idle the bucket allowed %d in a row, want capped at 3", allowed)
	}
}

func TestAllow_ConcurrentUseIsRaceFree(t *testing.T) {
	l := New(1000, time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				l.Allow("shared")
			}
		}()
	}
	wg.Wait()
}
