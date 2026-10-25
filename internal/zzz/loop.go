package zzz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/goal"
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
func (r *loopRunner) MarkOutcome(ok bool)     { r.eng.Waggle.MarkOutcome(ok) }

// outcomeMarker is implemented by runners whose engine mines procedure memory.
// A kept iteration verifies the tool routes it recorded; a rolled-back one
// discards them, so a mutating route is only learned from work that survived.
type outcomeMarker interface{ MarkOutcome(ok bool) }

const (
	defaultMaxConsecutiveFails = 3
	defaultHardErrorRetries    = 3
	defaultNotesTailIters      = 5
	// defaultMaxConsecutiveNoops ends a run after this many iters in a row that
	// changed nothing. without it a weak model that only surveys (never writes)
	// resets the fail streak every turn and spins to MaxIterations.
	defaultMaxConsecutiveNoops = 5
	// maxRetryBackoff caps the per-iter exponential backoff so a long provider
	// outage can't park the loop in a 8-minute+ sleep between retries.
	maxRetryBackoff = 60 * time.Second
)

// orDefault returns v when positive, else def.
func orDefault[T ~int | ~int64](v, def T) T {
	if v > 0 {
		return v
	}
	return def
}

// driver holds the state one Drive call carries across iterations.
type driver struct {
	stopCh <-chan struct{}
	eng    Runner
	cfg    Config
	run    *Run
	ui     UI
	steer  <-chan Steer
	notes  []string // operator nudges for the next prompt
	// priorTokens captures totals already on disk before this engine instance
	// started recording. eng.Costs is fresh per invocation, so resume needs
	// this baseline to keep accumulated tokens correct.
	priorTokens TokenStat
	fails       int
	noops       int
	replies     goal.ReplyStreak
}

// Drive is the main overnight loop. Returns nil on a clean exit (objective
// reached / max-iter / stop signal); error only on unrecoverable failure.
//
// ctx cancel = hard stop (mid-iteration). stopCh closed = graceful stop
// after current iteration finishes. If ui also satisfies Steerable, operator
// nudges are drained between iterations: notes get appended to the next
// prompt, stop closes the local graceful-stop path.
func Drive(ctx context.Context, stopCh <-chan struct{}, eng Runner, cfg Config, run *Run, ui UI) (retErr error) {
	// a panic from the engine (malformed tool output, nil deref, OOM) must not
	// leave the run wedged at status=running with no EndedAt — and in TUI mode
	// must not kill the goroutine before model.Done fires. Recover, mark the run
	// failed terminally, persist, and return an error so callers unwind cleanly.
	defer func() {
		if r := recover(); r != nil {
			run.Status = StatusFailed
			run.StopCause = fmt.Sprintf("panic: %v", r)
			run.EndedAt = time.Now().UTC()
			_ = SaveMeta(run)
			ui.Println(fmt.Sprintf("[zzz] PANIC recovered — run marked failed: %v", r))
			ui.RenderSummary(run)
			retErr = fmt.Errorf("zzz: recovered panic: %v", r)
		}
	}()
	if err := preflightClean(run.RepoRoot); err != nil {
		run.Status = StatusAborted
		run.StopCause = "dirty git tree on startup"
		_ = SaveMeta(run)
		return err
	}

	d := &driver{stopCh: stopCh, eng: eng, cfg: cfg, run: run, ui: ui, priorTokens: run.Tokens}
	if s, ok := ui.(Steerable); ok {
		d.steer = s.Steer()
	}
	run.Status, run.StopCause = d.loop(ctx)
	run.EndedAt = time.Now().UTC()
	_ = SaveMeta(run)
	ui.RenderSummary(run)
	return nil
}

// loop runs iterations until one ends the run, returning the final status and
// its cause.
func (d *driver) loop(ctx context.Context) (status, cause string) {
	for d.run.IterCount < d.cfg.MaxIterations {
		if ctx.Err() != nil {
			return StatusAborted, "context canceled"
		}
		if cause := d.halt(); cause != "" {
			return StatusAborted, cause
		}

		d.run.IterCount++
		iter := d.run.IterCount
		d.ui.SetIter(iter, d.cfg.MaxIterations)
		d.ui.SetPhase("prompting")

		res, err := runOneIteration(ctx, d.eng, d.cfg, d.run, iter, d.ui, d.notes, d.priorTokens)
		d.notes = nil
		if appErr := AppendEvent(d.run.ID, eventFromResult(iter, res)); appErr != nil {
			d.ui.Println("[zzz] events.jsonl write failed: " + appErr.Error())
		}
		if status, cause := d.settle(res, err); status != "" {
			return status, cause
		}
		_ = SaveMeta(d.run)
	}
	return StatusCompleted, "max-iterations reached"
}

// halt drains queued operator input and reports why the loop must stop before
// the next iteration, or "" to go on. Notes are kept for the next prompt.
func (d *driver) halt() string {
	stop := false
	for drained := false; !drained; {
		select {
		case s, ok := <-d.steer:
			if !ok {
				d.steer = nil
				drained = true
				continue
			}
			switch s.Kind {
			case SteerNote:
				if t := strings.TrimSpace(s.Text); t != "" {
					d.notes = append(d.notes, t)
					d.ui.Println("[zzz] noted: " + truncate(t, 80))
				}
			case SteerStop:
				stop = true
			case SteerAbort:
				d.ui.Println("[zzz] abort honored.")
				return "operator abort"
			}
		default:
			drained = true
		}
	}
	select {
	case <-d.stopCh:
		d.ui.Println("[zzz] graceful stop honored.")
		return "graceful stop requested"
	default:
	}
	if stop {
		d.ui.Println("[zzz] operator stop honored.")
		return "operator stop"
	}
	return ""
}

// settle books one finished iteration and returns the run's final status and
// cause when it ends here, or "" to go on.
func (d *driver) settle(res IterationResult, err error) (status, cause string) {
	markIter(d.eng, res)
	// DONE wins regardless of iter shape — an agent that says "objective
	// already complete" without touching files must not be force-looped.
	// a red tree is not done: the repair iteration runs first. a change the
	// gate dropped or a failed iteration is not done either: nothing landed.
	if res.Sentinel == sentinel.KindDone && (res.Status == IterCommitted || res.Status == IterNoop) {
		if res.Status == IterCommitted {
			d.recordCommit(res)
		}
		return StatusCompleted, "agent emitted DONE"
	}

	switch res.Status {
	case IterCommitted:
		d.fails, d.noops = 0, 0
		d.recordCommit(res)
	case IterNoop:
		// noop is not a failure — agent may have surveyed without writing
		// this turn. reset the fail streak so plan-then-act sequences aren't
		// killed, but leave a note (so the next prompt shows the survey
		// already happened) and track the streak so a model that only ever
		// surveys can't spin to MaxIterations committing nothing.
		// a clean tree does not prove the shell calls were read-only, so
		// markIter drops the span rather than verifying it.
		d.fails = 0
		d.noops++
		_ = AppendNote(d.run.ID, res.Iter, "noop — no file changes this iteration", res.Subject)
		if d.noops >= orDefault(d.cfg.MaxConsecutiveNoops, defaultMaxConsecutiveNoops) {
			return StatusFailed, fmt.Sprintf("%d consecutive no-op iterations (model made no changes)", d.noops)
		}
	case IterRepair:
		// bounded by the hop budget, not the fail streak
		d.noops = 0
	case IterReset, IterFailed:
		d.fails++
		if d.fails >= orDefault(d.cfg.MaxConsecutiveFails, defaultMaxConsecutiveFails) {
			return StatusFailed, fmt.Sprintf("%d consecutive failures", d.fails)
		}
	}
	if d.replies.Observe(res.Subject) {
		return StatusFailed, goal.StalledReason(d.replies.Count())
	}
	if errors.Is(err, context.Canceled) {
		return StatusAborted, "context canceled mid-iter"
	}
	if d.cfg.StopWhen != "" && strings.Contains(res.Subject, d.cfg.StopWhen) {
		return StatusCompleted, "stop-when matched"
	}
	if d.cfg.MaxTokens > 0 && d.run.Tokens.Input+d.run.Tokens.Output >= d.cfg.MaxTokens {
		return StatusCompleted, "max-tokens cap reached"
	}
	return "", ""
}

// recordCommit books a committed iteration on the run and pushes it.
func (d *driver) recordCommit(res IterationResult) {
	d.run.Commits = append(d.run.Commits, res.CommitSHA)
	_ = AppendNote(d.run.ID, res.Iter, res.Subject, res.DiffStat)
	// persist SHA before any network attempt so push failures can't
	// hide a committed iteration from meta.json.
	_ = SaveMeta(d.run)
	if d.cfg.Push {
		pushAfterCommit(d.run, res, d.ui)
	}
}
