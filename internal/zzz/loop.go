package zzz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/sentinel"
)

// Runner is the surface Drive needs from the engine. Real callers wrap
// *loop.Engine via NewLoopRunner; tests pass a stub.
type Runner interface {
	Run(ctx context.Context, prompt string) (loop.RunResult, error)
	CostTotal() cost.Summary
}

// NewLoopRunner adapts *loop.Engine to the Runner interface so Drive isn't
// bound to the concrete engine type.
func NewLoopRunner(eng *loop.Engine) Runner { return &loopRunner{eng: eng} }

type loopRunner struct{ eng *loop.Engine }

func (r *loopRunner) Run(ctx context.Context, prompt string) (loop.RunResult, error) {
	return r.eng.Run(ctx, prompt)
}
func (r *loopRunner) CostTotal() cost.Summary { return r.eng.Costs.Total() }

// preamble is prepended to every iteration prompt. The engine runs without a
// human at the keyboard so it must be told to make ONE small change and stop,
// never ask questions, never bail half-way.
const preamble = `You are running unattended inside an overnight loop.
Rules for this iteration:
- Make ONE small, focused change toward the objective.
- Do NOT ask the user any questions. There is no one to answer.
- If you cannot proceed, say exactly "BLOCKED: <reason>" and stop.
- If the objective is already complete, say exactly "DONE: <reason>" and stop.
- Otherwise: do the work, then briefly describe what you changed.

Objective:
`

const (
	defaultMaxConsecutiveFails = 3
	defaultHardErrorRetries    = 3
	defaultNotesTailIters      = 5
)

func cfgMaxConsecutiveFails(c Config) int {
	if c.MaxConsecutiveFails > 0 {
		return c.MaxConsecutiveFails
	}
	return defaultMaxConsecutiveFails
}

func cfgHardErrorRetries(c Config) int {
	if c.HardErrorRetries > 0 {
		return c.HardErrorRetries
	}
	return defaultHardErrorRetries
}

func cfgNotesTailIters(c Config) int {
	if c.NotesTailIters == 0 {
		return defaultNotesTailIters
	}
	return c.NotesTailIters
}

// Drive is the main overnight loop. Returns nil on a clean exit (objective
// reached / max-iter / stop signal); error only on unrecoverable failure.
//
// ctx cancel = hard stop (mid-iteration). stopCh closed = graceful stop
// after current iteration finishes. If ui also satisfies Steerable, operator
// nudges are drained between iterations: notes get appended to the next
// prompt, stop closes the local graceful-stop path.
func Drive(ctx context.Context, stopCh <-chan struct{}, eng Runner, cfg Config, run *Run, ui UI) error {
	if err := preflightClean(run.RepoRoot); err != nil {
		run.Status = StatusAborted
		run.StopCause = "dirty git tree on startup"
		_ = SaveMeta(run)
		return err
	}

	var steer <-chan Steer
	if s, ok := ui.(Steerable); ok {
		steer = s.Steer()
	}
	var pendingNotes []string
	consecutiveFails := 0
	maxFails := cfgMaxConsecutiveFails(cfg)
	// priorTokens captures totals already on disk before this engine instance
	// started recording. eng.Costs is fresh per invocation, so resume needs
	// this baseline to keep accumulated tokens correct.
	priorTokens := run.Tokens
	for run.IterCount < cfg.MaxIterations {
		if ctx.Err() != nil {
			run.Status = StatusAborted
			run.StopCause = "context canceled"
			break
		}
		// drain pending steering nudges before this iteration
		stopRequested := false
		drained := true
		for drained {
			select {
			case s, ok := <-steer:
				if !ok {
					steer = nil
					drained = false
					continue
				}
				switch s.Kind {
				case SteerNote:
					t := strings.TrimSpace(s.Text)
					if t != "" {
						pendingNotes = append(pendingNotes, t)
						ui.Println("[zzz] noted: " + truncate(t, 80))
					}
				case SteerStop:
					stopRequested = true
				case SteerAbort:
					run.Status = StatusAborted
					run.StopCause = "operator abort"
					_ = SaveMeta(run)
					ui.Println("[zzz] abort honored.")
					return nil
				}
			default:
				drained = false
			}
		}
		select {
		case <-stopCh:
			run.Status = StatusAborted
			run.StopCause = "graceful stop requested"
			_ = SaveMeta(run)
			ui.Println("[zzz] graceful stop honored.")
			return nil
		default:
		}
		if stopRequested {
			run.Status = StatusAborted
			run.StopCause = "operator stop"
			_ = SaveMeta(run)
			ui.Println("[zzz] operator stop honored.")
			return nil
		}

		run.IterCount++
		iter := run.IterCount
		ui.SetIter(iter, cfg.MaxIterations)
		ui.SetPhase("prompting")

		res, err := runOneIteration(ctx, eng, cfg, run, iter, ui, pendingNotes, priorTokens)
		pendingNotes = nil
		if appErr := AppendEvent(run.ID, eventFromResult(iter, res)); appErr != nil {
			ui.Println("[zzz] events.jsonl write failed: " + appErr.Error())
		}

		// DONE wins regardless of iter shape — an agent that says "objective
		// already complete" without touching files must not be force-looped.
		if sentinel.IsDone(res.Subject) {
			if res.Status == IterCommitted {
				run.Commits = append(run.Commits, res.CommitSHA)
				_ = AppendNote(run.ID, iter, res.Subject, res.DiffStat)
				_ = SaveMeta(run)
				if cfg.Push {
					pushAfterCommit(run, res, ui, iter)
				}
			}
			run.Status = StatusCompleted
			run.StopCause = "agent emitted DONE"
			goto finish
		}

		switch res.Status {
		case IterCommitted:
			consecutiveFails = 0
			run.Commits = append(run.Commits, res.CommitSHA)
			_ = AppendNote(run.ID, iter, res.Subject, res.DiffStat)
			// persist SHA before any network attempt so push failures can't
			// hide a committed iteration from meta.json.
			_ = SaveMeta(run)
			if cfg.Push {
				pushAfterCommit(run, res, ui, iter)
			}
		case IterNoop:
			// noop is not a failure — agent may have surveyed without
			// writing this turn. reset the fail streak so plan-then-act
			// sequences aren't killed.
			consecutiveFails = 0
		case IterReset, IterFailed:
			consecutiveFails++
			if consecutiveFails >= maxFails {
				run.Status = StatusFailed
				run.StopCause = fmt.Sprintf("%d consecutive failures", consecutiveFails)
				goto finish
			}
		}
		if err != nil && errors.Is(err, context.Canceled) {
			run.Status = StatusAborted
			run.StopCause = "context canceled mid-iter"
			goto finish
		}

		if cfg.StopWhen != "" && strings.Contains(res.Subject, cfg.StopWhen) {
			run.Status = StatusCompleted
			run.StopCause = "stop-when matched"
			goto finish
		}
		if cfg.MaxTokens > 0 && run.Tokens.Input+run.Tokens.Output >= cfg.MaxTokens {
			run.Status = StatusCompleted
			run.StopCause = "max-tokens cap reached"
			goto finish
		}
		_ = SaveMeta(run)
	}

	if run.IterCount >= cfg.MaxIterations {
		run.Status = StatusCompleted
		run.StopCause = "max-iterations reached"
	}

finish:
	run.EndedAt = time.Now().UTC()
	_ = SaveMeta(run)
	ui.RenderSummary(run)
	return nil
}

// runOneIteration performs the inner clean-build-run-classify-commit cycle
// for one iteration. Hard errors are retried with exponential backoff.
// priorTokens is the on-disk total before this engine instance booted; used
// to keep run.Tokens consistent across resume.
func runOneIteration(ctx context.Context, eng Runner, cfg Config, run *Run, iter int, ui UI, notes []string, priorTokens TokenStat) (IterationResult, error) {
	start := time.Now()
	r := IterationResult{Iter: iter}

	prompt, err := buildPrompt(run.ID, cfg.Objective, iter, notes, cfgNotesTailIters(cfg))
	if err != nil {
		r.Status = IterFailed
		r.Err = err
		return r, err
	}

	beforeTotal := eng.CostTotal()

	ui.SetPhase("engine.run")
	res, runErr := runEngineWithRetry(ctx, eng, prompt, cfgHardErrorRetries(cfg))
	r.Duration = time.Since(start)
	r.Subject = strings.TrimSpace(res.FinalText)

	afterTotal := eng.CostTotal()
	r.Tokens = TokenStat{
		Input:  afterTotal.Input - beforeTotal.Input,
		Output: afterTotal.Output - beforeTotal.Output,
		USD:    afterTotal.USD - beforeTotal.USD,
	}
	run.Tokens.Input = priorTokens.Input + afterTotal.Input
	run.Tokens.Output = priorTokens.Output + afterTotal.Output
	run.Tokens.USD = priorTokens.USD + afterTotal.USD
	ui.SetTokens(run.Tokens)

	if runErr != nil {
		r.Status = IterFailed
		r.Err = runErr
		ui.SetPhase("hard-error")
		rollbackTree(run.RepoRoot)
		return r, runErr
	}

	if sentinel.IsBlocked(r.Subject) {
		r.Status = IterFailed
		ui.SetPhase("agent-blocked")
		// stash partial work before reset so operator can inspect what the
		// blocked iter produced. best-effort — patch failure must not mask
		// the BLOCKED signal.
		if perr := SaveBlockedPatch(run.ID, iter, run.RepoRoot); perr != nil {
			ui.Println("[zzz] blocked-patch save failed: " + perr.Error())
		}
		rollbackTree(run.RepoRoot)
		return r, nil
	}

	clean, err := IsClean(run.RepoRoot)
	if err != nil {
		r.Status = IterFailed
		r.Err = err
		return r, err
	}
	if clean {
		r.Status = IterNoop
		ui.SetPhase("noop")
		return r, nil
	}

	ui.SetPhase("commit")
	msg := CommitMessageFrom(r.Subject, iter, r.Tokens.Input, r.Tokens.Output)
	// single git invocation: stage + commit. Atomicity comes from git itself
	// once the index is set; preserves reset-on-failure on any error (a
	// pre-commit hook that touched staged content would otherwise bleed
	// into the next iter's diff).
	sha, err := CommitAll(run.RepoRoot, msg, cfg.Sign, cfg.NoVerify)
	if err != nil {
		r.Status = IterReset
		r.Err = err
		ui.SetPhase("commit-fail")
		rollbackTree(run.RepoRoot)
		return r, err
	}
	r.CommitSHA = sha
	r.Status = IterCommitted
	r.DiffStat, _ = DiffStat(run.RepoRoot, "")
	ui.IncCommits()
	return r, nil
}

// rollbackTree wipes both tracked and untracked changes. Best-effort; failures
// surface on the next iter's preflight.
func rollbackTree(dir string) {
	_ = ResetHard(dir, "")
	_ = CleanFD(dir)
}

// pushAfterCommit pushes the run's branch, recording per-iter outcome on the
// run so meta.json reflects what's actually on the remote.
func pushAfterCommit(run *Run, res IterationResult, ui UI, iter int) {
	if perr := Push(run.RepoRoot, run.Branch); perr != nil {
		ui.Println("[zzz] push failed: " + perr.Error())
		run.PushFailedIters = append(run.PushFailedIters, iter)
		return
	}
	run.PushedCommits = append(run.PushedCommits, res.CommitSHA)
}

// runEngineWithRetry calls eng.Run with bounded exponential backoff on hard
// errors (network blips, transient provider 5xx). Agent-level "blocked"
// responses are NOT retried here — they're classified upstream.
func runEngineWithRetry(ctx context.Context, eng Runner, prompt string, retries int) (loop.RunResult, error) {
	if retries < 1 {
		retries = 1
	}
	var lastErr error
	delay := time.Second
	for attempt := 0; attempt < retries; attempt++ {
		if ctx.Err() != nil {
			return loop.RunResult{}, ctx.Err()
		}
		res, err := eng.Run(ctx, prompt)
		if err == nil {
			return res, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return res, err
		}
		lastErr = err
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return loop.RunResult{}, ctx.Err()
		}
		delay *= 2
	}
	return loop.RunResult{}, lastErr
}

// buildPrompt assembles preamble + tail of notes.md + operator nudges +
// objective. tailIters<0 echoes the full notes file (legacy behavior);
// tailIters>0 keeps only the last N "## iter X" sections so prompt size
// stays bounded across long runs.
func buildPrompt(runID, objective string, iter int, operatorNotes []string, tailIters int) (string, error) {
	notes, err := ReadNotes(runID)
	if err != nil {
		return "", err
	}
	if tailIters > 0 {
		notes = TailNoteSections(notes, tailIters)
	}
	var b strings.Builder
	b.WriteString(preamble)
	b.WriteString(objective)
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "This is iteration %d.\n", iter)
	if len(operatorNotes) > 0 {
		b.WriteString("\nOperator nudges for THIS iteration (live input):\n")
		for _, n := range operatorNotes {
			b.WriteString("- ")
			b.WriteString(n)
			b.WriteString("\n")
		}
	}
	if strings.TrimSpace(notes) != "" {
		b.WriteString("\nPrior iterations (notes.md):\n")
		b.WriteString(notes)
	}
	return b.String(), nil
}

// truncate trims s to at most n runes, appending "…" if it had to cut.
func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

// preflightClean refuses to start on a dirty tree. Mid-run dirtiness is
// always a bug (we reset after every failure) so it isn't tolerated either.
func preflightClean(dir string) error {
	clean, err := IsClean(dir)
	if err != nil {
		return fmt.Errorf("preflight: %w", err)
	}
	if !clean {
		return errors.New("preflight: working tree is dirty — commit, stash, or discard before continuing bee zzz")
	}
	return nil
}

// eventFromResult flattens an IterationResult into a row for events.jsonl.
func eventFromResult(iter int, r IterationResult) Event {
	ev := Event{Iter: iter, Phase: r.Status, Tokens: r.Tokens, Commit: r.CommitSHA, DiffStat: r.DiffStat}
	if r.Err != nil {
		ev.Err = r.Err.Error()
	}
	return ev
}
