package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLimiter_Allow(t *testing.T) {
	l := New(2) // 2 requests per second

	// Should allow initial burst
	assert.True(t, l.Allow())
	assert.True(t, l.Allow())

	// Third should fail (bucket empty)
	assert.False(t, l.Allow())
}

func TestLimiter_AllowRefill(t *testing.T) {
	l := New(10) // 10 requests per second

	// Drain the bucket
	for range 10 {
		require.True(t, l.Allow())
	}

	// Should be empty
	assert.False(t, l.Allow())

	// Wait for refill (100ms = 1 token at 10/sec)
	time.Sleep(110 * time.Millisecond)

	// Should have at least 1 token
	assert.True(t, l.Allow())
}

func TestLimiter_Wait(t *testing.T) {
	l := New(10) // 10 requests per second

	// Drain the bucket
	for range 10 {
		require.True(t, l.Allow())
	}

	// Wait should succeed after refill
	start := time.Now()
	err := l.Wait(t.Context())
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, elapsed, 90*time.Millisecond)
}

func TestLimiter_WaitContextCancellation(t *testing.T) {
	l := New(1) // 1 request per second

	// Drain the bucket
	require.True(t, l.Allow())

	// Cancel context immediately
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := l.Wait(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestLimiter_WaitContextTimeout(t *testing.T) {
	l := New(1) // 1 request per second

	// Drain the bucket
	require.True(t, l.Allow())

	// Timeout before refill
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()

	err := l.Wait(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestLimiter_MaxTokens(t *testing.T) {
	l := New(5)

	// Wait to accumulate tokens
	time.Sleep(200 * time.Millisecond)

	// Should still only allow maxTokens (5)
	count := 0
	for l.Allow() {
		count++
		if count > 10 {
			t.Fatal("infinite loop - tokens not capped")
		}
	}

	assert.Equal(t, 5, count)
}

func TestLimiter_ConcurrentWait(t *testing.T) {
	// Test that concurrent Wait calls don't cause issues
	// This exercises the race condition fix
	l := New(10) // 10 requests per second

	// Drain the bucket first
	for range 10 {
		require.True(t, l.Allow())
	}

	const goroutines = 20
	done := make(chan struct{}, goroutines)
	errCh := make(chan error, goroutines)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// Launch many concurrent Wait calls
	for range goroutines {
		go func() {
			err := l.Wait(ctx)
			if err != nil {
				errCh <- err
			}
			done <- struct{}{}
		}()
	}

	// Wait for all to complete
	for range goroutines {
		<-done
	}

	close(errCh)
	for err := range errCh {
		// Only context errors are acceptable
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

func TestLimiter_WaitReturnsImmediatelyWithToken(t *testing.T) {
	// Test that Wait returns immediately when token is available
	l := New(10)

	start := time.Now()
	err := l.Wait(t.Context())
	elapsed := time.Since(start)

	require.NoError(t, err)
	// Should return in less than 10ms when token is available
	assert.Less(t, elapsed, 10*time.Millisecond)
}

func TestNew_PanicsOnInvalidRate(t *testing.T) {
	tests := []struct {
		name string
		rate float64
	}{
		{"zero", 0},
		{"negative", -1},
		{"negative_fraction", -0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.PanicsWithValue(t, "ratelimit: requestsPerSecond must be > 0", func() {
				New(tt.rate)
			})
		})
	}
}
