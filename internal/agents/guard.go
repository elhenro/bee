package agents

import (
	"os/exec"
	"strings"

	"github.com/elhenro/bee/internal/gate"
	"github.com/elhenro/bee/internal/sandbox"
)

// agentScope is the sandbox spawned agents run under (Spawn passes --safe).
// The gate runs the agent's code, so it gets the same confinement.
const agentScope = sandbox.WorkspaceWrite

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// touchesGate reports whether the branch changes the repo's gate file. Once
// merged, such a change would govern every later merge, so a human decides.
func touchesGate(dir string) bool {
	out, err := gitOut(dir, "diff", "--name-only", MergeBase+"...HEAD", "--", gate.File)
	return err == nil && out != ""
}

// branchAt reports whether refs/heads/branch in repoRoot still points at sha,
// so the fast-forward lands exactly the commit that was verified.
func branchAt(repoRoot, branch, sha string) bool {
	out, err := gitOut(repoRoot, "rev-parse", "refs/heads/"+branch)
	return err == nil && out == sha
}
