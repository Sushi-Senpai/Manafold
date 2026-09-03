// Package ratelimit is a small in-process per-key token bucket. account-access
// uses it to blunt credential stuffing on the login and register endpoints
// before any database work runs (see
// docs/intent/account-access/account-access-design.md § Rate limiting). It is
// single-instance only; a shared store is needed before the backend scales past
// one instance.
//
// @spec ACCT-017
package ratelimit

import (
	"sync"
	"time"
)

// Limiter hands out tokens from a bucket per key. Each key's bucket starts full
// at capacity and refills one token every interval. Allow is safe for
// concurrent use.
type Limiter struct {
	capacity int
	interval time.Duration
	now      func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens     float64
	lastRefill time.Time
}

// New returns a Limiter whose buckets hold capacity tokens and gain one token
// every interval. A capacity below 1 is raised to 1.
func New(capacity int, interval time.Duration) *Limiter {
	if capacity < 1 {
		capacity = 1
	}
	return &Limiter{
		capacity: capacity,
		interval: interval,
		now:      time.Now,
		buckets:  make(map[string]*bucket),
	}
}

// Allow consumes one token for key and reports whether one was available. A
// key seen for the first time starts with a full bucket.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(l.capacity), lastRefill: now}
		l.buckets[key] = b
	}

	if l.interval > 0 {
		elapsed := now.Sub(b.lastRefill)
		if refill := float64(elapsed) / float64(l.interval); refill > 0 {
			b.tokens += refill
			if b.tokens > float64(l.capacity) {
				b.tokens = float64(l.capacity)
			}
			b.lastRefill = now
		}
	}

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
