package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/types"
)

// runMastermind drives one mastermind turn: instead of a single engine Run, it
// spawns a sub-agent hive (planner + workers + critic) off the main engine and
// runs the decompose → dispatch → review → synthesize pipeline.
//
// Each phase streams into scrollback as it lands (plan, then each worker result,
// then the critic note, then the synthesized answer) via the engine's LiveMsgCh
// — the same path normal tool/assistant cards use — so a multi-minute run shows
// continuous progress instead of an opaque spinner. The final turnDoneMsg
// carries the full transcript; live messages carry stable IDs so the final
// replace de-dupes against what already rendered.
//
// Workers run sequentially (Queen.MaxParallel = 1): they share the parent's one
// tool registry rooted at cwd, so parallel writers would race. The quality win
// here is the decomposition + critic verify, not wall-clock — which is also what
// lifts small/local models, where parallelism wouldn't help anyway. Parallel
// workers on isolated worktrees (the `swarm --isolated` path) is a follow-up.
func (m Model) runMastermind(ctx context.Context, gen int, prevDone, done chan struct{}, content []types.ContentBlock, history, prior []types.Message) tea.Cmd {
	planner := m.eng
	warn := m.warnCh
	live := m.liveMsgCh
	task := firstContentText(content)

	workerCount := planner.Cfg.MastermindWorkers
	if workerCount < 1 {
		workerCount = 3
	}

	return func() tea.Msg {
		defer close(done)
		if prevDone != nil {
			<-prevDone // wait for a prior (possibly esc'd) run to fully return
		}
		var mu sync.Mutex
		var streamed []types.Message
		var idN int

		// emit appends one assistant message to the running transcript and
		// pushes it to the live channel so it renders the instant a phase
		// finishes. Stable IDs let the final turnDoneMsg replace de-dupe.
		emit := func(text string) {
			text = strings.TrimSpace(text)
			if text == "" {
				return
			}
			mu.Lock()
			idN++
			msg := types.Message{
				ID:      fmt.Sprintf("mm-%d", idN),
				Role:    types.RoleAssistant,
				Content: []types.ContentBlock{{Type: types.BlockText, Text: text}},
			}
			streamed = append(streamed, msg)
			mu.Unlock()
			if live != nil {
				select {
				case live <- msg:
				default: // dropped sends reappear via the final turnDoneMsg replace
				}
			}
		}
		notify := func(s string) {
			if warn == nil {
				return
			}
			select {
			case warn <- s:
			default:
			}
		}

		// spawnHive clone: reuse the parent's provider/tools/skills, fresh
		// session, and share the session cost tracker so the top-bar token meter
		// keeps moving while the hive churns.
		spawn := func(label string) (*loop.Engine, *session.Rollout, error) {
			eng, sess, err := hive.SpawnWorker(planner, label)
			if err == nil {
				eng.Costs = planner.Costs
			}
			return eng, sess, err
		}

		var sessions []*session.Rollout
		defer func() {
			for _, s := range sessions {
				_ = s.Close()
			}
		}()

		// planner clone carries the conversation so decompose + synthesize have
		// context; workers stay focused on their single sub-task (no history).
		plannerEng, plannerSess, err := spawn("planner")
		if err != nil {
			return turnDoneMsg{gen: gen, err: fmt.Errorf("mastermind: planner: %w", err)}
		}
		sessions = append(sessions, plannerSess)
		plannerEng.InitialMessages = history

		workers := make([]hive.Runner, 0, workerCount)
		for i := 0; i < workerCount; i++ {
			w, sess, werr := spawn(fmt.Sprintf("worker-%d", i))
			if werr != nil {
				return turnDoneMsg{gen: gen, err: fmt.Errorf("mastermind: worker %d: %w", i, werr)}
			}
			sessions = append(sessions, sess)
			workers = append(workers, w)
		}

		critic, criticSess, err := spawn("critic")
		if err != nil {
			return turnDoneMsg{gen: gen, err: fmt.Errorf("mastermind: critic: %w", err)}
		}
		sessions = append(sessions, criticSess)

		q := hive.NewQueen(plannerEng, workers)
		q.Critic = critic
		q.MaxParallel = 1 // sequential — shared cwd tool registry, see doc above
		q.Hooks = hive.Hooks{
			OnPlan: func(p []hive.SubTask) {
				notify(fmt.Sprintf("hive: planned %d sub-tasks", len(p)))
				emit(formatPlan(p))
			},
			OnWorkerStart: func(i int, st hive.SubTask) {
				notify(fmt.Sprintf("hive: worker %d/%d — %s", i+1, len(workers), st.Role))
			},
			OnWorkerDone: func(i int, r hive.Result) { emit(formatWorker(i, r)) },
			OnCritique:   func(c string) { emit("### ⬢ critic\n\n" + c) },
			OnSynthesize: func() { notify("hive: synthesizing") },
		}

		notify("hive: decomposing task")
		res, runErr := q.Run(ctx, task)

		final := strings.TrimSpace(res.Final)
		if runErr == nil {
			emit(final)
		}

		mu.Lock()
		out := append(append([]types.Message(nil), prior...), streamed...)
		mu.Unlock()
		return turnDoneMsg{gen: gen, result: loop.RunResult{Messages: out, FinalText: final}, err: runErr}
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

// formatPlan renders the queen's decomposition as a numbered, role-tagged list.
func formatPlan(plan []hive.SubTask) string {
	var b strings.Builder
	b.WriteString("## ⬢ mastermind — plan\n\n")
	for i, st := range plan {
		role := st.Role
		if role == "" {
			role = "builder"
		}
		fmt.Fprintf(&b, "%d. **%s** — %s\n", i+1, role, strings.TrimSpace(st.Task))
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatWorker renders one finished worker's result (or its error).
func formatWorker(i int, r hive.Result) string {
	head := fmt.Sprintf("### ⬢ worker %d", i+1)
	if t := strings.TrimSpace(r.Task); t != "" {
		head += " — " + t
	}
	if r.Err != nil {
		return head + "\n\n_failed: " + r.Err.Error() + "_"
	}
	body := strings.TrimSpace(r.Final)
	if body == "" {
		body = "_(no output)_"
	}
	return head + "\n\n" + body
}
