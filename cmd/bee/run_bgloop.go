// Background-loop runner for `bee run --bg-loop`. Drives turns across
// inbox polls, writes status sidecars, exits on ctx cancel.
package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/sentinel"
	"github.com/elhenro/bee/internal/zzz"
)

// runBgLoop persists the engine across turns: write status sidecar on every
// boundary, run a turn, write awaiting status with the assistant's final
// text, then poll the inbox for follow-up messages. Exits on ctx cancel.
func runBgLoop(ctx context.Context, eng *loop.Engine, sessID, firstMsg string) error {
	agentMode := os.Getenv("BEE_AGENT_WORKTREE") == "1"
	branch := os.Getenv("BEE_AGENT_BRANCH")
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
	}
	if agentMode && base.Branch == "" {
		base.Branch = branch
	}
	if agentMode && base.MergeState == "" {
		base.MergeState = bgreg.MergeStateUnmerged
	}

	msg := firstMsg
	var cursor int64
	hadCommit := false
	for {
		s := base
		s.State = bgreg.StateActive
		s.UpdatedAt = time.Now().UTC()
		_ = bgreg.Write(s)

		// token snapshot before turn so we can compute deltas
		var beforeIn, beforeOut int
		if eng.Costs != nil {
			t := eng.Costs.Total()
			beforeIn, beforeOut = t.Input, t.Output
		}

		res, err := eng.Run(ctx, msg)
		if err != nil {
			if ctx.Err() != nil {
				s.State = bgreg.StateDone
				s.UpdatedAt = time.Now().UTC()
				_ = bgreg.Write(s)
				return nil
			}
			s.State = bgreg.StateFailed
			s.LastResponse = err.Error()
			s.UpdatedAt = time.Now().UTC()
			_ = bgreg.Write(s)
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
		var committedSHA string
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
				sha, cerr := zzz.Commit(eng.Cwd, commitMsg, false, false)
				if cerr != nil {
					return failAgentCommit(base, "git commit: "+cerr.Error())
				}
				committedSHA = sha
				hadCommit = true
			}
		}
		_ = committedSHA

		// classify final text via shared sentinel package. Legacy bg
		// sessions (non-agent mode) ignore sentinels and just sit awaiting.
		final := res.FinalText
		if agentMode {
			switch sentinel.Classify(final) {
			case sentinel.KindDone:
				s = base
				s.State = bgreg.StateDone
				// zero-change run has nothing to merge — skip the unmerged limbo
				// and tear down the worktree+branch right away.
				if hadCommit {
					s.MergeState = bgreg.MergeStateUnmerged
				} else {
					s.MergeState = bgreg.MergeStateMerged
					s.FinishedAt = time.Now().UTC()
					if agentMode && repoRoot != "" && base.WorktreePath != "" {
						_ = zzz.WorktreeRemove(repoRoot, base.WorktreePath, true)
						s.WorktreePath = ""
					}
				}
				s.LastResponse = final
				s.UpdatedAt = time.Now().UTC()
				_ = bgreg.Write(s)
				// exit loop — the merger goroutine in the TUI picks it up
				return nil
			case sentinel.KindBlocked:
				s = base
				s.State = bgreg.StateFailed
				s.LastResponse = final
				s.UpdatedAt = time.Now().UTC()
				_ = bgreg.Write(s)
				return nil
			}
		}

		s = base
		s.State = bgreg.StateAwaiting
		s.LastResponse = final
		s.UpdatedAt = time.Now().UTC()
		_ = bgreg.Write(s)

		next, newCursor, err := waitForInbox(ctx, sessID, cursor)
		if err != nil {
			return err
		}
		cursor = newCursor
		if ctx.Err() != nil {
			s.State = bgreg.StateDone
			s.UpdatedAt = time.Now().UTC()
			_ = bgreg.Write(s)
			return nil
		}
		msg = next
	}
}

// failAgentCommit marks the agent as failed when the auto-commit step can't
// finalize the worktree state. The merger keys off MergeStateUnmerged + DONE,
// so a silent commit miss would surface a corrupt branch downstream.
func failAgentCommit(base bgreg.Status, reason string) error {
	s := base
	s.State = bgreg.StateFailed
	s.LastResponse = "commit failed: " + reason
	s.UpdatedAt = time.Now().UTC()
	_ = bgreg.Write(s)
	return errors.New(s.LastResponse)
}

// waitForInbox polls the inbox until a message arrives or ctx is cancelled.
// Returns the concatenated text of all new messages and the advanced cursor.
func waitForInbox(ctx context.Context, sessID string, cursor int64) (string, int64, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", cursor, nil
		case <-ticker.C:
			msgs, nc, err := bgreg.InboxDrain(sessID, cursor)
			if err != nil {
				return "", cursor, err
			}
			if len(msgs) > 0 {
				var b strings.Builder
				for i, m := range msgs {
					if i > 0 {
						b.WriteString("\n\n")
					}
					b.WriteString(m.Text)
				}
				return b.String(), nc, nil
			}
		}
	}
}
