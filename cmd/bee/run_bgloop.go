// Background-loop runner for `bee run --bg-loop`. Drives turns across
// inbox polls, writes status sidecars, exits on ctx cancel.
package main

import (
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
// sidecar and returns the stamped status.
func putStatus(s bgreg.Status, state bgreg.State, resp string) bgreg.Status {
	s.State, s.LastResponse, s.UpdatedAt = state, resp, time.Now().UTC()
	_ = bgreg.Write(s)
	return s
}

// runBgLoop persists the engine across turns: write status sidecar on every
// boundary, run a turn, write awaiting status with the assistant's final
// text, then poll the inbox for follow-up messages. Exits on ctx cancel.
func runBgLoop(ctx context.Context, eng *loop.Engine, sessID, firstMsg string) error {
	agentMode := os.Getenv("BEE_AGENT_WORKTREE") == "1"
	repoRoot := os.Getenv("BEE_AGENT_REPO_ROOT")

	// inherit prior status fields (worktree/branch/provider/etc.) written by
	// the spawner so we don't overwrite them on the first status update.
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
		// preserve fields set by Spawn that the bg-loop doesn't own
		if existing.Task != "" {
			base.Task = existing.Task
		}
		if !existing.StartedAt.IsZero() {
			base.StartedAt = existing.StartedAt
		}
		base.WorktreePath = existing.WorktreePath
		base.Branch = existing.Branch
		if existing.Provider != "" {
			base.Provider = existing.Provider
		}
		if existing.Model != "" {
			base.Model = existing.Model
		}
		base.MergeState = existing.MergeState
		// retry budgets and the last bounce reason survive a resume
		base.Hops = existing.Hops
		base.Resumes = existing.Resumes
		base.ConflictMsg = existing.ConflictMsg
		base.HandinSHA = existing.HandinSHA
	}
	if agentMode && base.Branch == "" {
		base.Branch = os.Getenv("BEE_AGENT_BRANCH")
	}
	if agentMode && base.MergeState == "" {
		base.MergeState = bgreg.MergeStateUnmerged
	}

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

		// token snapshot before turn so we can compute deltas
		var beforeIn, beforeOut int
		if eng.Costs != nil {
			t := eng.Costs.Total()
			beforeIn, beforeOut = t.Input, t.Output
		}

		res, err := eng.Run(ctx, msg)
		if err != nil {
			if ctx.Err() != nil {
				writeStopped(s)
				return nil
			}
			putStatus(s, bgreg.StateFailed, err.Error())
			return err
		}

		// accumulate token totals from cost tracker delta
		if eng.Costs != nil {
			t := eng.Costs.Total()
			base.InputTokens += t.Input - beforeIn
			base.OutputTokens += t.Output - beforeOut
		}

		// agents mode: auto-commit any dirty tree from this turn. Surface
		// failures loudly — a silent miss here corrupts merger state (agent
		// signals DONE on a branch with no commit).
		if agentMode && repoRoot != "" {
			clean, ierr := zzz.IsClean(eng.Cwd)
			if ierr != nil {
				return failAgentCommit(base, "git status: "+ierr.Error())
			}
			if !clean {
				if cerr := zzz.AddAll(eng.Cwd); cerr != nil {
					return failAgentCommit(base, "git add: "+cerr.Error())
				}
				commitMsg := zzz.CommitMessageFrom(res.FinalText, 1, base.InputTokens, base.OutputTokens)
				if _, cerr := zzz.Commit(eng.Cwd, commitMsg, false, false); cerr != nil {
					return failAgentCommit(base, "git commit: "+cerr.Error())
				}
				hadCommit = true
			}
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
				s = base
				// zero-change run has nothing to merge — skip the unmerged limbo
				// and tear down the worktree+branch right away.
				// a DONE is the agent's handin: proof of life resets the
				// resume budget, HEAD is what the merger will verify.
				s.Resumes = 0
				s.ConflictMsg = ""
				s.HandinSHA, _ = zzz.HeadSHA(eng.Cwd)
				// a resumed agent may only have rebased, so judge by commits
				// ahead of the base, not by whether this process committed.
				// when the count fails, keep the branch rather than tear
				// down a worktree that may hold commits.
				if n, aerr := agents.Ahead(eng.Cwd); aerr != nil || n > 0 {
					hadCommit = true
				}
				if hadCommit {
					s.MergeState = bgreg.MergeStateUnmerged
				} else {
					s.MergeState = bgreg.MergeStateMerged
					s.FinishedAt = time.Now().UTC()
					if repoRoot != "" && base.WorktreePath != "" {
						_ = zzz.WorktreeRemove(repoRoot, base.WorktreePath, true)
						s.WorktreePath = ""
					}
				}
				// exit loop — the merger goroutine in the TUI picks it up
				putStatus(s, bgreg.StateDone, final)
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
