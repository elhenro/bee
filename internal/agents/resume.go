package agents

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/bgreg"
)

// Resume re-starts a stopped agent on its existing worktree and session with
// msg as the feedback. The new process does not reload the old transcript, so
// the prompt restates the original task. Each resume spends one unit of the
// budget; once Resumes reaches max the agent is blocked for a human instead
// and Resume returns false.
func Resume(repoRoot string, st bgreg.Status, msg string, max int) (bool, error) {
	if st.Resumes >= max {
		Block(st.SessionID, fmt.Sprintf("resume budget %d spent, last reason: %s", max, firstLine(msg)))
		return false, nil
	}
	err := bgreg.Update(st.SessionID, func(s *bgreg.Status) error {
		s.Resumes++
		s.State = bgreg.StateActive
		s.MergeState = bgreg.MergeStateUnmerged
		s.ConflictMsg = firstLine(msg)
		s.PID = 0
		s.UpdatedAt = time.Now().UTC()
		st = *s
		return nil
	})
	if err != nil {
		return false, err
	}
	pid, _, err := launch(repoRoot, st, agentPreamble+resumePrompt(st.Task, msg))
	if err != nil {
		Block(st.SessionID, "resume failed: "+err.Error())
		return false, err
	}
	_ = bgreg.Update(st.SessionID, func(s *bgreg.Status) error {
		s.PID = pid
		return nil
	})
	return true, nil
}

// Block parks an agent for a human: failed state, blocked merge state, reason
// in ConflictMsg. The merger never touches it again on its own.
func Block(id, reason string) {
	_ = bgreg.Update(id, func(s *bgreg.Status) error {
		s.State = bgreg.StateFailed
		s.MergeState = bgreg.MergeStateBlocked
		s.ConflictMsg = reason
		s.FinishedAt = time.Now().UTC()
		s.UpdatedAt = time.Now().UTC()
		return nil
	})
}

func resumePrompt(task, feedback string) string {
	return task + "\n\nYou worked on this task before in this worktree; your earlier commits are on the branch.\n" +
		"The coordinator sent it back:\n\n" + feedback
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}

// Ahead counts commits on dir's HEAD that MergeBase lacks. A DONE with work
// ahead is unmerged even when this process made no commit itself (a resumed
// agent may only have rebased).
func Ahead(dir string) (int, error) {
	cmd := exec.Command("git", "rev-list", "--count", MergeBase+"..HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// unmergedFiles lists paths left conflicted by an in-progress rebase.
func unmergedFiles(dir string) []string {
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			files = append(files, l)
		}
	}
	return files
}
