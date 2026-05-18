package agents

import (
	"os"

	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/zzz"
)

// ClearResult lists ids removed and any errors hit per id.
type ClearResult struct {
	Removed []string
	Errors  map[string]error
}

// ClearMerged removes every session whose merge state is "merged": status
// sidecar, inbox, worktree (if any), log. RepoRoot is needed to drop worktrees
// via `git worktree remove`. Sessions with WorktreePath still on disk get a
// best-effort plain dir removal as a fallback when git refuses (already-gone
// branch, etc.).
func ClearMerged(repoRoot string) ClearResult {
	res := ClearResult{Errors: map[string]error{}}
	all, err := bgreg.List()
	if err != nil {
		res.Errors["list"] = err
		return res
	}
	for _, s := range all {
		if s.MergeState != bgreg.MergeStateMerged {
			continue
		}
		if err := removeSessionArtifacts(repoRoot, s); err != nil {
			res.Errors[s.SessionID] = err
			continue
		}
		res.Removed = append(res.Removed, s.SessionID)
	}
	return res
}

func removeSessionArtifacts(repoRoot string, s bgreg.Status) error {
	if s.WorktreePath != "" && repoRoot != "" {
		if err := zzz.WorktreeRemove(repoRoot, s.WorktreePath, true); err != nil {
			// fall back to a raw dir removal; the git ref may already be gone.
			_ = os.RemoveAll(s.WorktreePath)
		}
	}
	_ = bgreg.InboxRemove(s.SessionID)
	if logPath, err := logFilePath(s.SessionID); err == nil {
		_ = os.Remove(logPath)
	}
	return bgreg.Remove(s.SessionID)
}
