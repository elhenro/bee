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

const (
	maxConsecutiveFails = 3
	hardErrorRetries    = 3
)

// Drive is the main overnight loop. Returns nil on a clean exit (objective
// reached / max-iter / stop signal); error only on unrecoverable failure.
//
// ctx cancel = hard stop (mid-iteration). stopCh closed = graceful stop
// after current iteration finishes. If ui also satisfies Steerable, operator
// nudges are drained between iterations: notes get appended to the next
// prompt, stop closes the local graceful-stop path.
func Drive(ctx context.Context, stopCh <-chan struct{}, eng *loop.Engine, cfg Config, run *Run, ui UI) error {
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

		res, err := runOneIteration(ctx, eng, cfg, run, iter, ui, pendingNotes)
		pendingNotes = nil
		_ = AppendEvent(run.ID, eventFromResult(iter, res))

		switch res.Status {
		case IterCommitted:
			consecutiveFails = 0
			run.Commits = append(run.Commits, res.CommitSHA)
			_ = AppendNote(run.ID, iter, res.Subject, res.DiffStat)
			if cfg.Push {
				if perr := Push(run.RepoRoot, run.Branch); perr != nil {
					ui.Println("[zzz] push failed: " + perr.Error())
				}
			}
			if sentinel.IsDone(res.Subject) {
				run.Status = StatusCompleted
				run.StopCause = "agent emitted DONE"
				goto finish
			}
		case IterReset, IterNoop, IterFailed:
			consecutiveFails++
			if consecutiveFails >= maxConsecutiveFails {
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
func runOneIteration(ctx context.Context, eng *loop.Engine, cfg Config, run *Run, iter int, ui UI, notes []string) (IterationResult, error) {
	start := time.Now()
	r := IterationResult{Iter: iter}

	prompt, err := buildPrompt(run.ID, cfg.Objective, iter, notes)
	if err != nil {
		r.Status = IterFailed
		r.Err = err
		return r, err
	}

	beforeIn := eng.Costs.Total().Input
	beforeOut := eng.Costs.Total().Output
	beforeUSD := eng.Costs.Total().USD

	ui.SetPhase("engine.run")
	res, runErr := runEngineWithRetry(ctx, eng, prompt)
	r.Duration = time.Since(start)
	r.Subject = strings.TrimSpace(res.FinalText)

	afterIn := eng.Costs.Total().Input
	afterOut := eng.Costs.Total().Output
	afterUSD := eng.Costs.Total().USD
	r.Tokens = TokenStat{Input: afterIn - beforeIn, Output: afterOut - beforeOut, USD: afterUSD - beforeUSD}
	run.Tokens.Input = afterIn
	run.Tokens.Output = afterOut
	run.Tokens.USD = afterUSD
	ui.SetTokens(run.Tokens)

	if runErr != nil {
		r.Status = IterFailed
		r.Err = runErr
		ui.SetPhase("hard-error")
		_ = ResetHard(run.RepoRoot, "")
		_ = CleanFD(run.RepoRoot)
		return r, runErr
	}

	if sentinel.IsBlocked(r.Subject) {
		r.Status = IterFailed
		ui.SetPhase("agent-blocked")
		_ = ResetHard(run.RepoRoot, "")
		_ = CleanFD(run.RepoRoot)
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
	if err := AddAll(run.RepoRoot); err != nil {
		r.Status = IterReset
		r.Err = err
		_ = ResetHard(run.RepoRoot, "")
		_ = CleanFD(run.RepoRoot)
		return r, err
	}
	msg := CommitMessageFrom(r.Subject, iter, r.Tokens.Input, r.Tokens.Output)
	sha, err := Commit(run.RepoRoot, msg, cfg.Sign, cfg.NoVerify)
	if err != nil {
		r.Status = IterFailed
		r.Err = err
		ui.SetPhase("commit-fail")
		return r, err
	}
	r.CommitSHA = sha
	r.Status = IterCommitted
	r.DiffStat, _ = DiffStat(run.RepoRoot, "")
	ui.IncCommits()
	return r, nil
}

// runEngineWithRetry calls eng.Run with bounded exponential backoff on hard
// errors (network blips, transient provider 5xx). Agent-level "blocked"
// responses are NOT retried here — they're classified upstream.
func runEngineWithRetry(ctx context.Context, eng *loop.Engine, prompt string) (loop.RunResult, error) {
	var lastErr error
	delay := time.Second
	for attempt := 0; attempt < hardErrorRetries; attempt++ {
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

// buildPrompt assembles preamble + notes.md (prior iterations) + operator
// nudges from this turn + objective.
func buildPrompt(runID, objective string, iter int, operatorNotes []string) (string, error) {
	notes, err := ReadNotes(runID)
	if err != nil {
		return "", err
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
		return errors.New("preflight: working tree is dirty — commit, stash, or discard before starting bee zzz")
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
