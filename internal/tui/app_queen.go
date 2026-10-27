package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/types"
)

// runQueen drives one queen turn: instead of a single engine Run, it
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
// Workers run in parallel (up to MastermindWorkers at once) via Queen.WorkerFor:
// read-only roles run in the shared tree with mutating tools stripped, mutating
// roles each get their own git worktree and merge back as they finish, so
// concurrent writers never race. Without a git repo (or with MastermindParallel
// off) mutating workers serialize on the shared tree instead. Each worker streams
// its activity into scrollback live, so the run shows what every bee is doing.
func (m Model) runQueen(ctx context.Context, gen int, prevDone, done chan struct{}, content []types.ContentBlock, history, prior []types.Message) tea.Cmd {
	planner := m.eng
	// pump channels: the TUI's stream/think/progress/live pumps are armed at
	// Init() against the parent engine's channels. In queen mode the parent
	// engine never runs — work happens on sub-engines spawned below. Snapshot
	// the TUI's channels here and re-bind them on every spawned engine so the
	// planner's first call (decompose) and every worker/reviewer/synthesize
	// turn stream reasoning + text deltas live into the same pump, instead of
	// dropping them because the sub-engine's channels are nil.
	warn := m.warnCh
	live := m.liveMsgCh
	stream := m.streamCh
	think := m.thinkCh
	progress := m.progressCh
	task := hiveTaskWithContext(content, history)
	toggles := m.cfgSnapshot()

	workerCount := planner.Cfg.MastermindWorkers
	if workerCount < 1 {
		workerCount = 3
	}

	triageOn := planner.Cfg.MastermindTriage

	return func() tea.Msg {
		defer close(done)
		if prevDone != nil {
			<-prevDone // wait for a prior (possibly esc'd) run to fully return
		}
		toggles.apply(planner)
		// non-blocking: a send on a nil channel falls through to default
		notify := func(s string) {
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
			msg := assistantText(text)
			msg.ID = fmt.Sprintf("mm-%d", idN)
			streamed = append(streamed, msg)
			mu.Unlock()
			select {
			case live <- msg:
			default: // dropped sends reappear via the final turnDoneMsg replace
			}
		}

		// spawnHive clone: reuse the parent's provider/tools/skills, fresh
		// session, and share the session cost tracker so the top-bar token meter
		// keeps moving while the hive churns. Each sub-engine streams its
		// assistant/tool messages into the shared live channel so the whole run
		// — planner, every worker, reviewers — renders live in scrollback
		// instead of an opaque spinner. The curated phase cards (plan, per-worker
		// summary, review) still land on top via emit.
		spawnTier := hiveSpawner(planner, notify)
		spawn := func(label string, tier config.ModelTier) (*loop.Engine, *session.Rollout, error) {
			eng, sess, err := spawnTier(label, tier)
			if err == nil {
				eng.Costs = planner.Costs
				wireHivePumps(eng, live, stream, think, progress)
			}
			return eng, sess, err
		}

		// spawnObserver clones a review/verify engine with the structured write
		// tools stripped, see stripWrites.
		spawnObserver := func(label string) (*loop.Engine, *session.Rollout, error) {
			eng, sess, err := spawn(label, planner.Cfg.Swarm.Reviewer)
			if err == nil {
				stripWrites(eng)
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
		plannerEng, plannerSess, err := spawn("planner", config.ModelTier{})
		if err != nil {
			return turnDoneMsg{gen: gen, err: fmt.Errorf("queen: planner: %w", err)}
		}
		sessions = append(sessions, plannerSess)
		plannerEng.InitialMessages = history

		// isolate: run mutating workers concurrently, each in its own git
		// worktree, merging changes back as they finish. Needs the tools-for-cwd
		// builder (to root a worker at its worktree) and a git repo (worktree
		// create). When unavailable, mutating workers serialize on the shared
		// tree instead (treeMu) so they never race.
		isolate := planner.Cfg.MastermindParallel && planner.ToolsForCwd != nil
		var treeMu sync.Mutex

		// workerFor spawns a dedicated engine per sub-task. Read-only roles run
		// in the shared tree (mutating tools stripped) in parallel; mutating
		// roles get an isolated worktree (or the serialized shared tree). Every
		// worker streams its activity live into scrollback via the shared live
		// channel so the user sees what each one does, not just a spinner.
		workerFor := func(idx int, st hive.SubTask) (hive.Runner, func(error), error) {
			eng, sess, err := spawn(fmt.Sprintf("worker-%d", idx), planner.Cfg.Swarm.Worker)
			if err != nil {
				return nil, nil, err
			}
			mu.Lock()
			sessions = append(sessions, sess)
			mu.Unlock()

			if st.Role.ReadOnly() {
				stripWrites(eng, "bash")
				return eng, nil, nil
			}

			if isolate {
				if done, ok := isolateWorker(planner, eng, idx, &treeMu, notify); ok {
					return eng, done, nil
				}
				notify(fmt.Sprintf("hive: worker %d — worktree unavailable, serializing", idx+1))
			}

			// no isolation: serialize mutating workers on the shared tree so they
			// never race (still parallel with read-only scouts).
			return lockedRunner{r: eng, mu: &treeMu}, nil, nil
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
		reviewerCount := max(planner.Cfg.MastermindReviewers, 1)
		reviewers := make([]hive.Runner, 0, reviewerCount)
		for i := range reviewerCount {
			rev, revSess, rerr := spawnObserver(fmt.Sprintf("reviewer-%d", i))
			if rerr != nil {
				return turnDoneMsg{gen: gen, err: fmt.Errorf("queen: reviewer %d: %w", i, rerr)}
			}
			sessions = append(sessions, revSess)
			reviewers = append(reviewers, rev)
		}
		verifier, verSess, err := spawnObserver("verifier")
		if err != nil {
			return turnDoneMsg{gen: gen, err: fmt.Errorf("queen: verifier: %w", err)}
		}
		sessions = append(sessions, verSess)

		// review-gate cards: each dimension emits one permanent card showing its
		// findings with verdicts, so the user sees every check the queen ran (not
		// just transient one-liners). Hooks fire sequentially within this one
		// goroutine (MaxParallel=1, gate is serial), so no locking is needed.
		// A dimension's card flushes when the next dimension starts; the last one
		// flushes after Run returns.
		cards := reviewCards{emit: emit}

		q := hive.NewQueen(plannerEng, nil)
		q.Reviewers = reviewers
		q.Verifier = verifier
		q.WorkerFor = workerFor
		// pheromone card: mirrored next to the sessions so the hive can be
		// watched from another terminal while it runs.
		if path, perr := hive.PheromonePath(plannerSess.ID()); perr == nil {
			q.Pheromone = hive.NewPheromone(path)
			notify("hive: pheromone card at " + path)
		}
		q.MaxParallel = workerCount // cap concurrent workers; isolation keeps them safe
		q.Throttle = hiveThrottle(planner.Cfg, notify)
		if n := tierNotice(planner.Cfg); n != "" {
			notify(n)
		}
		// sub-task count drives the worker N/M readout: set from the plan in
		// OnPlan so the denominator is the real sub-task count, not the parallel
		// cap — else a 5-task plan with a cap of 3 prints "worker 5/3".
		planCount := workerCount
		q.Hooks = hive.Hooks{
			OnPlan: func(p []hive.SubTask) {
				planCount = len(p)
				notify(fmt.Sprintf("hive: planned %d sub-tasks", len(p)))
				emit(formatPlan(p))
			},
			OnWorkerStart: func(i int, st hive.SubTask) {
				notify(fmt.Sprintf("hive: worker %d/%d — %s", i+1, planCount, st.Role))
			},
			OnWorkerDone: func(i int, r hive.Result) { emit(formatWorker(i, r)) },
			OnReview: func(dim string, claims []string) {
				cards.flush() // close out the previous dimension's card
				cards.start(dim)
				notify(fmt.Sprintf("hive: review %s — %d finding(s), verifying…", dim, len(claims)))
			},
			OnVerify: func(f hive.Finding) {
				cards.add(f)
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
		cards.flush() // emit the final dimension's card

		final := strings.TrimSpace(res.Final)
		if runErr == nil {
			emit(final)
		}

		mu.Lock()
		out := slices.Concat(prior, streamed)
		mu.Unlock()
		return turnDoneMsg{gen: gen, result: loop.RunResult{Messages: out, FinalText: final}, err: runErr}
	}
}
