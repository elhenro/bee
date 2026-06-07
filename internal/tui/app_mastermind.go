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
// spawns a sub-agent hive (planner + workers + reviewer + verifier) off the
// main engine and runs decompose → dispatch → review → verify → synthesize.
//
// The review gate is the extra time queen mode spends after workers finish
// changing code: three reviewers (correctness, persistence, integration) read
// the real diff and a verifier re-checks every finding before it counts — the
// last quality pass before the user commits.
//
// Each phase streams into scrollback as it lands (plan, then each worker result,
// then the review-gate summary, then the synthesized answer) via the engine's
// LiveMsgCh — the same path normal tool/assistant cards use — so a multi-minute
// run shows continuous progress instead of an opaque spinner. The final
// turnDoneMsg carries the full transcript; live messages carry stable IDs so the
// final replace de-dupes against what already rendered.
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
	task := hiveTaskFromContent(content)

	workerCount := planner.Cfg.MastermindWorkers
	if workerCount < 1 {
		workerCount = 3
	}

	triageOn := planner != nil && planner.Cfg.MastermindTriage

	return func() tea.Msg {
		defer close(done)
		if prevDone != nil {
			<-prevDone // wait for a prior (possibly esc'd) run to fully return
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

		// adaptive routing: small, clear tasks skip the hive and run as a normal
		// streaming turn on the main engine — full tool/thought streaming, no
		// planner/worker/review overhead. The hive is reserved for work that
		// needs it. Ambiguity (and any classifier error) falls through to the
		// hive, so a risky change is never under-reviewed.
		if triageOn && hive.TriageSimple(ctx, planner.Provider, planner.Cfg.DefaultModel, task) {
			notify("queen: small task — single pass, no hive")
			planner.InitialMessages = history // safe: prior run already released the engine
			res, err := planner.RunWithContent(ctx, content)
			return turnDoneMsg{gen: gen, result: res, err: err}
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

		// spawnObserver clones a review/verify engine with the structured write
		// tools stripped — it still has bash (needed for `git diff`) and the
		// read/search tools, but an accidental edit/write/apply_patch call fails
		// rather than mutating the tree during what should be observation only.
		spawnObserver := func(label string) (*loop.Engine, *session.Rollout, error) {
			eng, sess, err := spawn(label)
			if err == nil && eng.Tools != nil {
				eng.Tools = eng.Tools.Without("write", "edit", "apply_patch", "hashline_edit", "knowledge_write")
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

		// review gate: the queen scores three dimensions (correctness,
		// persistence, integration) against the real working-tree changes, then
		// a verifier re-checks every finding before it's kept. Runs after workers
		// finish mutating code — the last quality pass before the user commits.
		//
		// The dimensions run sequentially and Engine.Run is stateless across
		// calls (re-seeds from InitialMessages, never writes back), so one shared
		// reviewer engine serves all three via the queen's round-robin
		// (q.Reviewers[i%len]). Spawn MastermindReviewers engines (default 1) to
		// keep the per-turn engine count down without changing review behavior.
		reviewerCount := planner.Cfg.MastermindReviewers
		if reviewerCount < 1 {
			reviewerCount = 1
		}
		reviewers := make([]hive.Runner, 0, reviewerCount)
		for i := 0; i < reviewerCount; i++ {
			rev, revSess, rerr := spawnObserver(fmt.Sprintf("reviewer-%d", i))
			if rerr != nil {
				return turnDoneMsg{gen: gen, err: fmt.Errorf("mastermind: reviewer %d: %w", i, rerr)}
			}
			sessions = append(sessions, revSess)
			reviewers = append(reviewers, rev)
		}
		verifier, verSess, err := spawnObserver("verifier")
		if err != nil {
			return turnDoneMsg{gen: gen, err: fmt.Errorf("mastermind: verifier: %w", err)}
		}
		sessions = append(sessions, verSess)

		// review-gate cards: each dimension emits one permanent card showing its
		// findings with verdicts, so the user sees every check the queen ran (not
		// just transient one-liners). Hooks fire sequentially within this one
		// goroutine (MaxParallel=1, gate is serial), so no locking is needed.
		// A dimension's card flushes when the next dimension starts; the last one
		// flushes after Run returns.
		var curDim string
		curFindings := []hive.Finding{}
		dimSeen := false
		flushReview := func() {
			if !dimSeen {
				return
			}
			emit(formatReview(curDim, curFindings))
			dimSeen = false
			curFindings = curFindings[:0]
		}

		q := hive.NewQueen(plannerEng, workers)
		q.Reviewers = reviewers
		q.Verifier = verifier
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
			OnReview: func(dim string, claims []string) {
				flushReview() // close out the previous dimension's card
				curDim, dimSeen = dim, true
				notify(fmt.Sprintf("hive: review %s — %d finding(s), verifying…", dim, len(claims)))
			},
			OnVerify: func(f hive.Finding) {
				curFindings = append(curFindings, f)
				verdict := "confirmed"
				if !f.Confirmed {
					verdict = "refuted"
				}
				notify(fmt.Sprintf("hive: verify %s — %s", f.Dimension, verdict))
			},
			OnSynthesize: func() { notify("hive: synthesizing") },
		}

		notify("hive: decomposing task")
		res, runErr := q.Run(ctx, task)
		flushReview() // emit the final dimension's card

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

// hiveTaskFromContent flattens the user's content blocks into the string task
// the hive Runner consumes. All non-empty text blocks are joined so nothing is
// dropped. The Runner interface is string-only, so images can't reach workers;
// rather than drop them silently, append a note recording how many were
// attached so sub-agents (and the user) know they exist but aren't visible.
func hiveTaskFromContent(blocks []types.ContentBlock) string {
	var texts []string
	images := 0
	for _, b := range blocks {
		switch b.Type {
		case types.BlockText:
			if t := strings.TrimSpace(b.Text); t != "" {
				texts = append(texts, t)
			}
		case types.BlockImage:
			images++
		}
	}
	task := strings.Join(texts, "\n\n")
	if images > 0 {
		task += fmt.Sprintf("\n\n[note: %d image(s) were attached but are not visible to sub-agents]", images)
	}
	return strings.TrimSpace(task)
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

// formatReview renders one dimension's review card: each finding with its
// re-verification verdict (✓ confirmed / ✗ refuted-and-dropped), or a clean
// note when the dimension turned up nothing. Shows the user every check ran.
func formatReview(dim string, findings []hive.Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### ⬢ review — %s\n\n", dim)
	if len(findings) == 0 {
		b.WriteString("_clean — no findings_")
		return b.String()
	}
	for _, f := range findings {
		mark, tail := "✓", ""
		if !f.Confirmed {
			mark, tail = "✗", " — _dropped_"
		}
		fmt.Fprintf(&b, "- %s %s", mark, strings.TrimSpace(f.Claim))
		if v := strings.TrimSpace(f.Verdict); v != "" {
			fmt.Fprintf(&b, " (%s)", v)
		}
		b.WriteString(tail)
		b.WriteByte('\n')
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
