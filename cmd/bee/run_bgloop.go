// Background-loop runner for `bee run --bg-loop`. Drives turns across
// inbox polls, writes status sidecars, exits on ctx cancel.
package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/agents"
	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/sentinel"
	"github.com/elhenro/bee/internal/sysload"
	"github.com/elhenro/bee/internal/zzz"
)

// putStatus stamps s with state, last response and the current time, writes the
// sidecar and returns the stamped status. Only an active write may create the
// file: a later write after `bee bg --kill` removed it would resurrect the
// husk for the merger.
func putStatus(s bgreg.Status, state bgreg.State, resp string) bgreg.Status {
	s.State, s.LastResponse, s.UpdatedAt = state, resp, time.Now().UTC()
	if _, err := bgreg.Read(s.SessionID); errors.Is(err, os.ErrNotExist) && state != bgreg.StateActive {
		return s
	}
	_ = bgreg.Write(s)
	return s
}

// inheritStatus builds the status this process writes, starting from whatever
// the spawner left on disk (worktree, branch, merge state, retry budgets, ...)
// so the first update does not overwrite it. Only what this process owns is
// reset: pid, cwd, state, token totals and the finish time.
func inheritStatus(eng *loop.Engine, sessID, firstMsg string, agentMode bool) bgreg.Status {
	base := bgreg.Status{
		SessionID: sessID,
		PID:       os.Getpid(),
		Task:      firstMsg,
		Model:     eng.Cfg.DefaultModel,
		Provider:  eng.Cfg.DefaultProvider,
		Cwd:       eng.Cwd,
		StartedAt: time.Now().UTC(),
	}
	if existing, err := bgreg.Read(sessID); err == nil {
		inherited := existing
		inherited.PID, inherited.Cwd = base.PID, base.Cwd
		inherited.State, inherited.LastResponse = "", ""
		inherited.InputTokens, inherited.OutputTokens = 0, 0
		inherited.FinishedAt = time.Time{}
		inherited.Task = cmp.Or(existing.Task, base.Task)
		inherited.StartedAt = cmp.Or(existing.StartedAt, base.StartedAt)
		inherited.Provider = cmp.Or(existing.Provider, base.Provider)
		inherited.Model = cmp.Or(existing.Model, base.Model)
		base = inherited
	}
	if agentMode && base.Branch == "" {
		base.Branch = os.Getenv("BEE_AGENT_BRANCH")
	}
	if agentMode && base.MergeState == "" {
		base.MergeState = bgreg.MergeStateUnmerged
	}
	return base
}

// tokenTotals reads the cost tracker's running input/output totals.
func tokenTotals(eng *loop.Engine) (in, out int) {
	if eng.Costs == nil {
		return 0, 0
	}
	t := eng.Costs.Total()
	return t.Input, t.Output
}

// runBgLoop persists the engine across turns: write status sidecar on every
// boundary, run a turn, write awaiting status with the assistant's final
// text, then poll the inbox for follow-up messages. Exits on ctx cancel.
// Each turn carries the transcript of the ones before it, and runs under the
// same auto-resume wrapper as `bee run`.
func runBgLoop(ctx context.Context, eng *loop.Engine, sessID, firstMsg string) error {
	agentMode := os.Getenv("BEE_AGENT_WORKTREE") == "1"
	repoRoot := os.Getenv("BEE_AGENT_REPO_ROOT")
	base := inheritStatus(eng, sessID, firstMsg, agentMode)

	if agentMode {
		holdForLoad(ctx, eng.Cfg.Swarm.MaxLoadRatio, base)
	}

	msg := firstMsg
	// a restarted agent starts with whatever was sent while nothing listened
	if pending, ok := pendingInbox(sessID); ok {
		msg += "\n\nMessages from the user:\n\n" + pending
	}
	hadCommit := false
	for {
		s := putStatus(base, bgreg.StateActive, "")
		beforeIn, beforeOut := tokenTotals(eng)

		res, err := runWithResume(ctx, eng, msg)
		carryHistory(eng, res, err)
		if err != nil {
			if ctx.Err() != nil {
				writeStopped(s)
				return nil
			}
			putStatus(s, bgreg.StateFailed, err.Error())
			return err
		}

		in, out := tokenTotals(eng)
		base.InputTokens += in - beforeIn
		base.OutputTokens += out - beforeOut

		// agents mode: auto-commit any dirty tree from this turn
		if agentMode && repoRoot != "" {
			committed, cerr := commitAgentTurn(eng, base, res.FinalText)
			if cerr != nil {
				return cerr
			}
			hadCommit = hadCommit || committed
		}

		// classify final text via shared sentinel package. Legacy bg
		// sessions (non-agent mode) ignore sentinels and just sit awaiting.
		final := res.FinalText
		if agentMode {
			kind := sentinel.Classify(final)
			// a reply queued while this turn ran must not die with a DONE or
			// BLOCKED exit: run it as the next turn instead of finalizing.
			if kind == sentinel.KindDone || kind == sentinel.KindBlocked {
				if next, ok := pendingInbox(sessID); ok {
					msg = next
					continue
				}
			}
			switch kind {
			case sentinel.KindDone:
				// exit loop: the merger goroutine in the TUI picks it up
				finishAgent(eng, base, repoRoot, hadCommit, final)
				return nil
			case sentinel.KindBlocked:
				putStatus(base, bgreg.StateFailed, final)
				return nil
			}
		}

		s = putStatus(base, bgreg.StateAwaiting, final)

		next, err := waitForInbox(ctx, sessID)
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			writeStopped(s)
			return nil
		}
		msg = next
	}
}

// commitAgentTurn commits a dirty worktree after a turn and reports whether it
// did. Failures are surfaced loudly: a silent miss here corrupts merger state
// (agent signals DONE on a branch with no commit).
func commitAgentTurn(eng *loop.Engine, base bgreg.Status, final string) (bool, error) {
	clean, err := zzz.IsClean(eng.Cwd)
	if err != nil {
		return false, failAgentCommit(base, "git status: "+err.Error())
	}
	if clean {
		return false, nil
	}
	if err := zzz.AddAll(eng.Cwd); err != nil {
		return false, failAgentCommit(base, "git add: "+err.Error())
	}
	msg := zzz.CommitMessageFrom(final, 1, base.InputTokens, base.OutputTokens)
	if _, err := zzz.Commit(eng.Cwd, msg, false, false); err != nil {
		return false, failAgentCommit(base, "git commit: "+err.Error())
	}
	return true, nil
}

// finishAgent records a DONE hand-in. A DONE is proof of life, so it resets
// the resume budget; HEAD is what the merger will verify.
func finishAgent(eng *loop.Engine, base bgreg.Status, repoRoot string, hadCommit bool, final string) {
	s := base
	s.Resumes = 0
	s.ConflictMsg = ""
	s.HandinSHA, _ = zzz.HeadSHA(eng.Cwd)
	// a resumed agent may only have rebased, so judge by commits ahead of the
	// base, not by whether this process committed. when the count fails, keep
	// the branch rather than tear down a worktree that may hold commits.
	if n, err := agents.Ahead(eng.Cwd); err != nil || n > 0 {
		hadCommit = true
	}
	if hadCommit {
		s.MergeState = bgreg.MergeStateUnmerged
	} else {
		// zero-change run has nothing to merge: skip the unmerged limbo and
		// tear down the worktree+branch right away.
		s.MergeState = bgreg.MergeStateMerged
		s.FinishedAt = time.Now().UTC()
		if repoRoot != "" && base.WorktreePath != "" {
			_ = zzz.WorktreeRemove(repoRoot, base.WorktreePath, true)
			s.WorktreePath = ""
		}
	}
	putStatus(s, bgreg.StateDone, final)
}

// writeStopped records a cancelled run as done, unless `bee bg --kill` already
// removed the sidecar: rewriting it would resurrect a killed agent for the
// merger.
func writeStopped(s bgreg.Status) {
	if _, err := bgreg.Read(s.SessionID); errors.Is(err, os.ErrNotExist) {
		return
	}
	putStatus(s, bgreg.StateDone, s.LastResponse)
}

// maxLoadHold caps how long an agent waits on load. Agents are separate
// processes and cannot see their siblings, so a time cap is what keeps a busy
// machine from starving them.
const maxLoadHold = 10 * time.Minute

// holdForLoad keeps a fresh agent from starting its first turn while the
// machine is oversubscribed, and says so on the overview row.
func holdForLoad(ctx context.Context, ratio float64, base bgreg.Status) {
	deadline := time.Now().Add(maxLoadHold)
	_ = sysload.Wait(ctx, ratio, func(perCore float64) {
		putStatus(base, bgreg.StateActive, fmt.Sprintf("waiting: load %.1f per core is above %.1f", perCore, ratio))
	}, func() bool { return time.Now().After(deadline) })
}

// failAgentCommit marks the agent as failed when the auto-commit step can't
// finalize the worktree state. The merger keys off MergeStateUnmerged + DONE,
// so a silent commit miss would surface a corrupt branch downstream.
func failAgentCommit(base bgreg.Status, reason string) error {
	return errors.New(putStatus(base, bgreg.StateFailed, "commit failed: "+reason).LastResponse)
}

// pendingInbox takes whatever is in the inbox right now without waiting.
func pendingInbox(sessID string) (string, bool) {
	msgs, err := bgreg.InboxTake(sessID)
	if err != nil || len(msgs) == 0 {
		return "", false
	}
	return joinInbox(msgs), true
}

func joinInbox(msgs []bgreg.Inbox) string {
	texts := make([]string, len(msgs))
	for i, m := range msgs {
		texts[i] = m.Text
	}
	return strings.Join(texts, "\n\n")
}

// waitForInbox polls the inbox until a message arrives or ctx is cancelled.
// Returns the concatenated text of all new messages.
func waitForInbox(ctx context.Context, sessID string) (string, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", nil
		case <-ticker.C:
			msgs, err := bgreg.InboxTake(sessID)
			if err != nil {
				return "", err
			}
			if len(msgs) > 0 {
				return joinInbox(msgs), nil
			}
		}
	}
}
