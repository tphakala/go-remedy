// Package ratelimit provides a token bucket rate limiter for API requests.
package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Limiter implements a token bucket rate limiter.
// It is safe for concurrent use.
type Limiter struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
	mu         sync.Mutex
}

// New creates a new rate limiter with the specified requests per second.
// The bucket starts full with capacity equal to requestsPerSecond,
// allowing initial burst up to that limit.
func New(requestsPerSecond float64) *Limiter {
	return &Limiter{
		tokens:     requestsPerSecond,
		maxTokens:  requestsPerSecond,
		refillRate: requestsPerSecond,
		lastRefill: time.Now(),
	}
}

// Allow checks if a request can proceed without waiting.
// Returns true if a token was available and consumed, false otherwise.
func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.refill()

	if l.tokens >= 1 {
		l.tokens--
		return true
	}

	return false
}

// Wait blocks until a token is available or the context is cancelled.
// Returns nil if a token was acquired, or the context error if cancelled.
func (l *Limiter) Wait(ctx context.Context) error {
	for {
		// Atomically try to get a token or calculate wait time
		// This prevents TOCTOU race where token state changes between check and wait
		waitTime := l.tryAcquireOrGetWaitTime()
		if waitTime == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
			// Try again
		}
	}
}

// tryAcquireOrGetWaitTime atomically tries to acquire a token.
// Returns 0 if token was acquired, otherwise returns duration to wait.
func (l *Limiter) tryAcquireOrGetWaitTime() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.refill()

	if l.tokens >= 1 {
		l.tokens--
		return 0
	}

	tokensNeeded := 1 - l.tokens
	seconds := tokensNeeded / l.refillRate

	return time.Duration(seconds * float64(time.Second))
}

// refill adds tokens based on elapsed time since last refill.
// Must be called with l.mu held.
func (l *Limiter) refill() {
	now := time.Now()
	elapsed := now.Sub(l.lastRefill).Seconds()
	l.lastRefill = now

	l.tokens += elapsed * l.refillRate
	if l.tokens > l.maxTokens {
		l.tokens = l.maxTokens
	}
}

