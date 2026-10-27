package tui

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
	"github.com/elhenro/bee/internal/worktree"
)

// lockedRunner serializes a worker that mutates the shared working tree: it
// holds mu for the whole Run so two shared-tree writers never race. Used as the
// fallback when worktree isolation is unavailable (not a git repo / disabled).
type lockedRunner struct {
	r  hive.Runner
	mu *sync.Mutex
}

func (l lockedRunner) Run(ctx context.Context, msg string) (loop.RunResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Run(ctx, msg)
}

// SetPromptTail forwards the pheromone card to the wrapped engine so a
// serialized worker still carries the hive state in its prompt.
func (l lockedRunner) SetPromptTail(f func() string) {
	if t, ok := l.r.(hive.Tailed); ok {
		t.SetPromptTail(f)
	}
}

// DeathReport forwards to the wrapped engine so a serialized worker that dies
// still leaves its report.
func (l lockedRunner) DeathReport(ctx context.Context, msgs []types.Message, cause error) (string, error) {
	if r, ok := l.r.(hive.Reporter); ok {
		return r.DeathReport(ctx, msgs, cause)
	}
	return "", errors.New("runner cannot report")
}

// stripWrites removes the structured write tools (plus extra) from eng. A
// review/verify engine keeps bash (needed for `git diff`) and the read/search
// tools, but an accidental edit/write/apply_patch call fails rather than
// mutating the tree during what should be observation only.
func stripWrites(eng *loop.Engine, extra ...string) {
	if eng.Tools != nil {
		eng.Tools = eng.Tools.Without(append([]string{"write", "edit", "apply_patch", "hashline_edit", "knowledge_write"}, extra...)...)
	}
}

// isolateWorker roots eng in its own git worktree. The returned done merges the
// tree back under treeMu, or drops it when the worker failed. ok is false when
// no worktree could be set up, so the caller falls back to serializing.
func isolateWorker(planner, eng *loop.Engine, idx int, treeMu *sync.Mutex, notify func(string)) (done func(error), ok bool) {
	wt, err := worktree.Create(planner.Cwd, fmt.Sprintf("queen-w%d", idx))
	if err != nil {
		return nil, false
	}
	reg, err := planner.ToolsForCwd(wt.Path)
	if err != nil {
		_ = wt.Cleanup()
		return nil, false
	}
	eng.Tools = reg
	eng.Cwd = wt.Path
	return func(runErr error) {
		defer func() { _ = wt.Cleanup() }()
		if runErr != nil {
			return // failed worker: drop its tree
		}
		treeMu.Lock()
		defer treeMu.Unlock()
		if merr := wt.MergeBack(); merr != nil {
			notify(fmt.Sprintf("hive: worker %d merge conflict — changes dropped", idx+1))
		}
	}, true
}
