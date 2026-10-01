package server

import (
	"sync"
	"time"
)

// rateLimiter is a fixed-window-with-sliding-refill token bucket per key.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	last    time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newRateLimiter() *rateLimiter { return &rateLimiter{buckets: map[string]*bucket{}} }

// allow consumes one token from a bucket of size limit that refills fully
// every window.
func (l *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	ok, _ := l.take(key, limit, window)
	return ok
}

// take returns whether the call is allowed and, if not, how long to wait.
func (l *rateLimiter) take(key string, limit int, window time.Duration) (bool, time.Duration) {
	if limit <= 0 {
		return true, 0
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.last) > 5*time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.at) > time.Hour {
				delete(l.buckets, k)
			}
		}
		l.last = now
	}
	rate := float64(limit) / window.Seconds()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(limit), at: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.at).Seconds() * rate
	if b.tokens > float64(limit) {
		b.tokens = float64(limit)
	}
	b.at = now
	if b.tokens < 1 {
		wait := time.Duration((1 - b.tokens) / rate * float64(time.Second))
		return false, wait
	}
	b.tokens--
	return true, 0
}
