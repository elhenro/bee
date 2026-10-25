package cron

import (
	"context"
	"sync"
)

// defaultResourceKey is used when a job does not declare one, so every job
// without an explicit key shares a single serialization slot.
const defaultResourceKey = "__default__"

// Queue serializes job execution per resource key so two jobs that share a
// key never run concurrently. A slot is released on completion or failure,
// including a panic, so a crashed job cannot wedge its key.
type Queue struct {
	mu    sync.Mutex
	locks map[string]chan struct{}
}

// NewQueue returns an empty queue.
func NewQueue() *Queue { return &Queue{locks: make(map[string]chan struct{})} }

// Acquire blocks until key is free or ctx is done, then returns a release
// func. The release func is idempotent and safe to defer.
func (q *Queue) Acquire(ctx context.Context, key string) (func(), error) {
	if key == "" {
		key = defaultResourceKey
	}
	q.mu.Lock()
	ch := q.locks[key]
	if ch == nil {
		ch = make(chan struct{}, 1)
		ch <- struct{}{}
		q.locks[key] = ch
	}
	q.mu.Unlock()

	select {
	case <-ch:
		var once sync.Once
		return func() { once.Do(func() { ch <- struct{}{} }) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
