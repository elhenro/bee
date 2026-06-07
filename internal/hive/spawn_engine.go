package hive

import (
	"io"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
)

// SpawnWorker clones a worker engine from parent, reusing its Provider, Tools,
// Skills, Cfg, and Cwd but giving it a fresh session rollout so the worker's
// transcript stays isolated from the parent's. Returns the engine and its
// rollout; the caller owns closing the rollout.
//
// Memory and the live channels (StreamCh/ThinkCh/...) are intentionally left
// nil: a hive worker runs silently and its answer is read back from
// RunResult.FinalText, mirroring `bee swarm`'s cheap fan-out workers. label is
// debug-only — the session id is the canonical handle.
func SpawnWorker(parent *loop.Engine, label string) (*loop.Engine, *session.Rollout, error) {
	if parent == nil {
		return nil, nil, errNilEngine
	}
	roll, err := session.Open(uuid.NewString())
	if err != nil {
		return nil, nil, err
	}
	_ = label
	eng := &loop.Engine{
		Provider: parent.Provider,
		Tools:    parent.Tools,
		Skills:   parent.Skills,
		Cfg:      parent.Cfg,
		Cwd:      parent.Cwd,
		Stdout:   io.Discard,
		Sessions: roll,
	}
	return eng, roll, nil
}
