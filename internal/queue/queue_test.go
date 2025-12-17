package queue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueue_AcquireRelease(t *testing.T) {
	q := New()

	err := q.Acquire(t.Context())
	require.NoError(t, err)

	q.Release()
}

func TestQueue_Serialization(t *testing.T) {
	q := New()
	var counter atomic.Int32
	var maxConcurrent atomic.Int32

	const goroutines = 10

	done := make(chan struct{})
	errCh := make(chan error, goroutines)
	for range goroutines {
		go func() {
			err := q.Acquire(t.Context())
			if err != nil {
				errCh <- err
				return
			}

			current := counter.Add(1)
			if current > maxConcurrent.Load() {
				maxConcurrent.Store(current)
			}

			// Simulate work
			time.Sleep(time.Millisecond)

			counter.Add(-1)
			q.Release()
			done <- struct{}{}
		}()
	}

	for range goroutines {
		<-done
	}

	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), maxConcurrent.Load(), "max concurrent should be 1")
}

func TestQueue_ContextCancellation(t *testing.T) {
	q := New()

	// Acquire first
	err := q.Acquire(t.Context())
	require.NoError(t, err)

	// Try to acquire with cancelled context
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err = q.Acquire(ctx)
	require.ErrorIs(t, err, context.Canceled)

	q.Release()
}

func TestQueue_ContextTimeout(t *testing.T) {
	q := New()

	// Acquire first
	err := q.Acquire(t.Context())
	require.NoError(t, err)

	// Try to acquire with timeout
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()

	err = q.Acquire(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	q.Release()
}

func TestQueue_Close(t *testing.T) {
	q := New()

	// Acquire first
	err := q.Acquire(t.Context())
	require.NoError(t, err)

	// Close the queue
	q.Close()

	// New acquire should fail
	err = q.Acquire(t.Context())
	require.ErrorIs(t, err, ErrQueueClosed)

	q.Release()
}

func TestQueue_ReleasePanicWithoutAcquire(t *testing.T) {
	q := New()

	assert.Panics(t, func() {
		q.Release()
	})
}

func TestQueue_DoubleCloseDoesNotPanic(t *testing.T) {
	q := New()

	// First close should succeed
	q.Close()

	// Second close should not panic
	assert.NotPanics(t, func() {
		q.Close()
	})
}

func TestQueue_CloseIsIdempotent(t *testing.T) {
	q := New()

	// Close multiple times from different goroutines should be safe
	done := make(chan struct{})
	for range 10 {
		go func() {
			q.Close()
			done <- struct{}{}
		}()
	}

	for range 10 {
		<-done
	}

	// Queue should still be closed and work correctly
	err := q.Acquire(t.Context())
	assert.ErrorIs(t, err, ErrQueueClosed)
}
