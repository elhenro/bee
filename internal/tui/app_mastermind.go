package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/types"
)

// runMastermind drives one mastermind turn: instead of a single engine Run, it
// spawns a sub-agent hive (planner + workers + critic) off the main engine and
// runs the decompose → dispatch → review → synthesize pipeline. Progress streams
// to the warning line; the finished transcript + answer come back via
// turnDoneMsg, exactly like a normal turn.
//
// Workers run sequentially (Queen.MaxParallel = 1): they share the parent's one
// tool registry rooted at cwd, so parallel writers would race. The quality win
// here is the decomposition + critic verify, not wall-clock — which is also what
// lifts small/local models, where parallelism wouldn't help anyway. Parallel
// workers on isolated worktrees (the `swarm --isolated` path) is a follow-up.
func (m Model) runMastermind(ctx context.Context, content []types.ContentBlock, history, prior []types.Message) tea.Cmd {
	planner := m.eng
	warn := m.warnCh
	task := firstContentText(content)

	workerCount := planner.Cfg.MastermindWorkers
	if workerCount < 1 {
		workerCount = 3
	}

	return func() tea.Msg {
		notify := func(s string) {
			if warn == nil {
				return
			}
			select {
			case warn <- s:
			default:
			}
		}

		// planner clone carries the conversation so decompose + synthesize have
		// context; workers stay focused on their single sub-task (no history).
		plannerEng, plannerSess, err := hive.SpawnWorker(planner, "planner")
		if err != nil {
			return turnDoneMsg{err: fmt.Errorf("mastermind: planner: %w", err)}
		}
		defer plannerSess.Close()
		plannerEng.InitialMessages = history

		var sessions []*session.Rollout
		defer func() {
			for _, s := range sessions {
				_ = s.Close()
			}
		}()

		workers := make([]hive.Runner, 0, workerCount)
		for i := 0; i < workerCount; i++ {
			w, sess, werr := hive.SpawnWorker(planner, fmt.Sprintf("worker-%d", i))
			if werr != nil {
				return turnDoneMsg{err: fmt.Errorf("mastermind: worker %d: %w", i, werr)}
			}
			sessions = append(sessions, sess)
			workers = append(workers, w)
		}

		critic, criticSess, err := hive.SpawnWorker(planner, "critic")
		if err != nil {
			return turnDoneMsg{err: fmt.Errorf("mastermind: critic: %w", err)}
		}
		sessions = append(sessions, criticSess)

		q := hive.NewQueen(plannerEng, workers)
		q.Critic = critic
		q.MaxParallel = 1 // sequential — shared cwd tool registry, see doc above
		q.Hooks = hive.Hooks{
			OnPlan: func(p []hive.SubTask) { notify(fmt.Sprintf("hive: planned %d sub-tasks", len(p))) },
			OnWorkerStart: func(i int, st hive.SubTask) {
				notify(fmt.Sprintf("hive: worker %d/%d — %s", i+1, len(workers), st.Role))
			},
			OnCritique:   func(string) { notify("hive: critic reviewing") },
			OnSynthesize: func() { notify("hive: synthesizing") },
		}

		notify("hive: decomposing task")
		res, err := q.Run(ctx, task)
		if err != nil {
			return turnDoneMsg{result: loop.RunResult{Messages: prior}, err: err}
		}

		msgs := append([]types.Message(nil), prior...)
		msgs = append(msgs, types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: formatHiveLog(res)}},
		})
		final := strings.TrimSpace(res.Final)
		if final != "" {
			msgs = append(msgs, types.Message{
				Role:    types.RoleAssistant,
				Content: []types.ContentBlock{{Type: types.BlockText, Text: final}},
			})
		}
		return turnDoneMsg{result: loop.RunResult{Messages: msgs, FinalText: final}}
	}
}

// firstContentText returns the first non-empty text block — the user's prompt.
func firstContentText(blocks []types.ContentBlock) string {
	for _, b := range blocks {
		if b.Type == types.BlockText && strings.TrimSpace(b.Text) != "" {
			return b.Text
		}
	}
	return ""
}

// formatHiveLog renders a compact record of what the hive did: the plan, a
// one-line status per worker, and the critic's note. Worker bodies are omitted
// — the synthesis below already folds them in; this is the "show your work" log.
func formatHiveLog(res hive.QueenResult) string {
	var b strings.Builder
	b.WriteString("## ⬢ mastermind hive\n\n")
	for i, st := range res.Plan {
		role := st.Role
		if role == "" {
			role = "builder"
		}
		fmt.Fprintf(&b, "%d. **%s** — %s\n", i+1, role, strings.TrimSpace(st.Task))
	}
	if len(res.Plan) > 0 {
		b.WriteString("\n")
	}
	for i, wr := range res.WorkerResults {
		status := "done"
		if wr.Err != nil {
			status = "failed: " + wr.Err.Error()
		}
		dur := wr.Ended.Sub(wr.Started).Round(time.Millisecond)
		fmt.Fprintf(&b, "- worker %d — %s (%s)\n", i+1, status, dur)
	}
	if strings.TrimSpace(res.Critique) != "" {
		b.WriteString("\n**critic:** ")
		b.WriteString(strings.TrimSpace(res.Critique))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
