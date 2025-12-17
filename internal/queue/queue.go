// Package queue provides request serialization for BMC Remedy API clients.
//
// BMC Remedy enforces per-user session limits. Concurrent requests with
// the same user account trigger Error 9093: "User is currently connected
// from another machine or incompatible session". This package ensures
// only one request executes at a time per client.
package queue

import (
	"context"
	"errors"
	"sync"
)

// ErrQueueClosed is returned when trying to acquire from a closed queue.
var ErrQueueClosed = errors.New("queue: closed")

// Queue ensures single active request per client using a semaphore pattern.
// It is safe for concurrent use.
type Queue struct {
	sem       chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
}

// New creates a new request serialization queue.
func New() *Queue {
	return &Queue{
		sem:    make(chan struct{}, 1),
		closed: make(chan struct{}),
	}
}

// Acquire waits for exclusive access to the queue.
// It respects context cancellation and returns an error if the context
// is cancelled or the queue is closed while waiting.
func (q *Queue) Acquire(ctx context.Context) error {
	// Check context first to avoid race condition in select
	if err := ctx.Err(); err != nil {
		return err
	}

	// Non-blocking check for closed queue before attempting to acquire.
	// This ensures immediate return if queue is already closed, avoiding
	// the race condition where select randomly chooses between cases.
	select {
	case <-q.closed:
		return ErrQueueClosed
	default:
	}

	select {
	case <-q.closed:
		return ErrQueueClosed
	case <-ctx.Done():
		return ctx.Err()
	case q.sem <- struct{}{}:
		return nil
	}
}

// Release releases the exclusive access acquired by Acquire.
// It must be called after Acquire returns successfully.
// Calling Release without a successful Acquire will panic.
func (q *Queue) Release() {
	select {
	case <-q.sem:
		// Successfully released
	default:
		panic("queue: Release called without Acquire")
	}
}

// Close closes the queue, causing all pending and future Acquire calls to fail.
// Close is idempotent and safe to call multiple times.
func (q *Queue) Close() {
	q.closeOnce.Do(func() {
		close(q.closed)
	})
}
