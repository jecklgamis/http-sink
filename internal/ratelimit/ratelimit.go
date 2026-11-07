// Package ratelimit implements a global fixed-window request limiter.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter allows at most limit calls to Allow per window.
type Limiter struct {
	mu          sync.Mutex
	limit       int
	window      time.Duration
	now         func() time.Time
	windowStart time.Time
	count       int
}

// New returns a Limiter allowing limit requests per window.
func New(limit int, window time.Duration) *Limiter {
	return newWithClock(limit, window, time.Now)
}

func newWithClock(limit int, window time.Duration, now func() time.Time) *Limiter {
	return &Limiter{limit: limit, window: window, now: now, windowStart: now()}
}

// Limit reports the configured maximum requests per window.
func (l *Limiter) Limit() int { return l.limit }

// Allow records a request and reports whether it is within the limit.
func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.windowStart) >= l.window {
		l.windowStart = now
		l.count = 0
	}
	l.count++
	return l.count <= l.limit
}

// Reset starts a fresh window.
func (l *Limiter) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.windowStart = l.now()
	l.count = 0
}
