package zzz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/sentinel"
)

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

// runOneIteration performs the inner clean-build-run-classify-commit cycle
// for one iteration. Hard errors are retried with exponential backoff.
// priorTokens is the on-disk total before this engine instance booted; used
// to keep run.Tokens consistent across resume.
func runOneIteration(ctx context.Context, eng Runner, cfg Config, run *Run, iter int, ui UI, notes []string, priorTokens TokenStat) (IterationResult, error) {
	r := IterationResult{Iter: iter}
	fail := func(status string, err error) (IterationResult, error) {
		r.Status, r.Err = status, err
		return r, err
	}

	tail := cfg.NotesTailIters
	if tail == 0 {
		tail = defaultNotesTailIters
	}
	prompt, err := buildPrompt(run.ID, cfg.Objective, iter, notes, tail)
	if err != nil {
		return fail(IterFailed, err)
	}

	beforeTotal := eng.CostTotal()

	ui.SetPhase("engine.run")
	res, runErr := runEngineWithRetry(ctx, eng, prompt, orDefault(cfg.HardErrorRetries, defaultHardErrorRetries))
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
		ui.SetPhase("hard-error")
		rollbackTree(run.RepoRoot)
		return fail(IterFailed, runErr)
	}

	// classified once so the rollback below and Drive's DONE check agree: a
	// reply holding both markers is DONE, never reset as blocked then completed.
	r.Sentinel = sentinel.Classify(r.Subject)
	if r.Sentinel == sentinel.KindBlocked {
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
		return fail(IterFailed, err)
	}
	if clean {
		r.Status = IterNoop
		run.Hops = 0 // a reverted change takes its red verifies with it
		ui.SetPhase("noop")
		return r, nil
	}
	if !gateTree(ctx, cfg, run, &r, ui) {
		return r, r.Err
	}

	ui.SetPhase("commit")
	msg := CommitMessageFrom(r.Subject, iter, r.Tokens.Input, r.Tokens.Output)
	// single git invocation: stage + commit. Atomicity comes from git itself
	// once the index is set; preserves reset-on-failure on any error (a
	// pre-commit hook that touched staged content would otherwise bleed
	// into the next iter's diff).
	sha, err := CommitAll(run.RepoRoot, msg, cfg.Sign, cfg.NoVerify)
	if err != nil {
		ui.SetPhase("commit-fail")
		rollbackTree(run.RepoRoot)
		return fail(IterReset, err)
	}
	r.CommitSHA = sha
	r.Status = IterCommitted
	r.DiffStat, _ = DiffStat(run.RepoRoot)
	ui.IncCommits()
	return r, nil
}

// rollbackTree wipes both tracked and untracked changes. Best-effort; failures
// surface on the next iter's preflight.
func rollbackTree(dir string) {
	_ = ResetHard(dir)
	_ = CleanFD(dir)
}

// pushAfterCommit pushes the run's branch, recording per-iter outcome on the
// run so meta.json reflects what's actually on the remote.
func pushAfterCommit(run *Run, res IterationResult, ui UI) {
	if perr := Push(run.RepoRoot, run.Branch); perr != nil {
		ui.Println("[zzz] push failed: " + perr.Error())
		run.PushFailedIters = append(run.PushFailedIters, res.Iter)
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
		if delay > maxRetryBackoff {
			delay = maxRetryBackoff
		}
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
