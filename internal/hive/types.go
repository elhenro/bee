// Package hive implements multi-bee orchestration: fan-out (Pool), queen-and-
// workers (Queen), and the result fan-in primitives both share.
//
// A Worker wraps a single loop.Engine running in its own goroutine. The TUI
// visual concept lives in internal/tui/hive.go and is intentionally separate.
package hive

import (
	"context"
	"time"

	"github.com/elhenro/bee/internal/loop"
)

// State of a Worker bee.
type State string

const (
	StatePending  State = "pending"
	StateRunning  State = "running"
	StateDone     State = "done"
	StateFailed   State = "failed"
	StateCanceled State = "canceled"
)

// Worker is one bee executing one task against its own Engine.
//
// Engine is required; the rest is filled in by Pool/Queen and observed via
// the Result channel.
type Worker struct {
	ID      string
	Name    string
	Task    string
	Engine  *loop.Engine
	State   State
	Started time.Time
	Ended   time.Time
}

// Result is what a Worker emits on completion. Final is the final assistant
// text; Err is non-nil if the run failed.
type Result struct {
	WorkerID string
	Name     string
	Task     string
	Final    string
	Err      error
	Started  time.Time
	Ended    time.Time
}

// Runner is the contract Pool and Queen both rely on. It exists so callers can
// inject a stub Runner in tests instead of a real loop.Engine.
type Runner interface {
	Run(ctx context.Context, userMsg string) (loop.RunResult, error)
}

// SubTask pairs a single planner-emitted sub-task with the role that should
// execute it. Queen produces these during decompose and uses them downstream
// in dispatch + synthesize.
type SubTask struct {
	Role Role
	Task string
}
