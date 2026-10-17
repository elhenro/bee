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
// symlink would redirect the gate), plus every in-repo link and target the
// main checkout's gate resolves through.
func gatePaths(repoRoot string) []string {
	specs := []string{":(icase)" + filepath.Dir(gate.File)}
	root, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return specs
	}
	for _, p := range linkChain(root, gate.File) {
		if p != gate.File {
			specs = append(specs, ":(literal)"+p)
		}
	}
	return specs
}

// pristine checks sha out detached, so verify sees exactly the commit being
// merged: no untracked or ignored leftovers from the agent. The gate must
// therefore bring its own dependencies. The checkout lives in a private dir
// under bee home, outside every agent's sandbox write roots (tmp is shared
// with them), so no other agent can edit it while verify runs.
func pristine(repoRoot, sha string) (string, func(), error) {
	home, err := beeHome()
	if err != nil {
		return "", nil, err
	}
	base := filepath.Join(home, "verify")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp(base, "v-")
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
