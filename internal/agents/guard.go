package agents

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// touchesGate reports whether the branch changes what the main checkout's
// gate reads. Once merged, such a change would govern every later merge, so a
// human decides. Fails closed: a git error counts as touched.
func touchesGate(repoRoot, dir string) bool {
	args := append([]string{"diff", "--name-only", MergeBase + "...HEAD", "--"}, gatePaths(repoRoot)...)
	out, err := gitOut(dir, args...)
	return err != nil || out != ""
}

// gatePaths are the pathspecs a branch may not touch: the whole .bee dir in
// any case (a case-insensitive filesystem maps .BEE onto .bee, and a .bee
// symlink would redirect the gate), plus the in-repo file the gate resolves
// to when it is a symlink.
func gatePaths(repoRoot string) []string {
	specs := []string{":(icase)" + filepath.Dir(gate.File)}
	root, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return specs
	}
	real, err := filepath.EvalSymlinks(filepath.Join(root, gate.File))
	if err != nil {
		return specs
	}
	rel, err := filepath.Rel(root, real)
	if err == nil && rel != gate.File && rel != ".." && !strings.HasPrefix(rel, "../") {
		specs = append(specs, ":(literal)"+filepath.ToSlash(rel))
	}
	return specs
}

// pristine checks sha out detached into a temp dir, so verify sees exactly
// the commit being merged: no untracked or ignored leftovers from the agent.
// The gate must therefore bring its own dependencies.
func pristine(repoRoot, sha string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "bee-verify-")
	if err != nil {
		return "", nil, err
	}
	cmd := exec.Command("git", "worktree", "add", "--detach", "-q", dir, sha)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("verify checkout: %v: %s", err, strings.TrimSpace(string(out)))
	}
	cleanup := func() {
		_, _ = gitOut(repoRoot, "worktree", "remove", "--force", dir)
		_ = os.RemoveAll(dir)
	}
	return dir, cleanup, nil
}

// branchAt reports whether refs/heads/branch in repoRoot still points at sha,
// so the fast-forward lands exactly the commit that was verified.
func branchAt(repoRoot, branch, sha string) bool {
	out, err := gitOut(repoRoot, "rev-parse", "refs/heads/"+branch)
	return err == nil && out == sha
}
