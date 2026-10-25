package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/elhenro/bee/internal/beehome"
	"github.com/elhenro/bee/internal/gate"
	"github.com/elhenro/bee/internal/gitsafe"
	"github.com/elhenro/bee/internal/sandbox"
)

// agentScope is the sandbox spawned agents run under (Spawn passes --safe).
// The gate runs the agent's code, so it gets the same confinement.
const agentScope = sandbox.WorkspaceWrite

func gitOut(dir string, args ...string) (string, error) {
	cmd := gitsafe.Command(args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// touchesGate reports whether the branch changes anything under .bee, in any
// case (a case-insensitive filesystem maps .BEE onto .bee). Once merged, such
// a change would govern every later merge, so a human decides. Fails closed:
// a git error counts as touched.
func touchesGate(dir string) bool {
	out, err := gitOut(dir, "diff", "--name-only", MergeBase+"...HEAD", "--", ":(icase)"+filepath.Dir(gate.File))
	return err != nil || out != ""
}

// gateLinked reports whether the main checkout reads its gate through a
// symlink. That is unsupported: a link could point at a file any branch may
// edit, and following link chains safely is not worth the complexity. A
// missing gate is fine, since a branch adding one trips touchesGate.
func gateLinked(repoRoot string) bool {
	for _, p := range []string{filepath.Dir(gate.File), gate.File} {
		fi, err := os.Lstat(filepath.Join(repoRoot, p))
		if err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

// pristine checks sha out detached, so verify sees exactly the commit being
// merged: no untracked or ignored leftovers from the agent. The gate must
// therefore bring its own dependencies. The checkout lives in a private dir
// under bee home, outside every agent's sandbox write roots (tmp is shared
// with them), so no other agent can edit it while verify runs.
func pristine(repoRoot, sha string) (string, func(), error) {
	base, err := beehome.Join("verify")
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp(base, "v-")
	if err != nil {
		return "", nil, err
	}
	cmd := gitsafe.Command("worktree", "add", "--detach", "-q", dir, sha)
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

// sameRepo reports whether the worktree at wt belongs to the repo at
// repoRoot. Sessions of every repo share one registry, and the merger must
// only touch its own.
func sameRepo(repoRoot, wt string) bool {
	a, errA := commonDir(repoRoot)
	b, errB := commonDir(wt)
	return errA == nil && errB == nil && a == b
}

func commonDir(dir string) (string, error) {
	out, err := gitOut(dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(dir, out)
	}
	return filepath.EvalSymlinks(out)
}

// rebasing reports whether dir has a rebase in progress that someone else
// started: aborting it would throw away a half-resolved conflict.
func rebasing(dir string) bool {
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		p, err := gitOut(dir, "rev-parse", "--git-path", name)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// branchAt reports whether refs/heads/branch in repoRoot still points at sha,
// so the fast-forward lands exactly the commit that was verified.
func branchAt(repoRoot, branch, sha string) bool {
	out, err := gitOut(repoRoot, "rev-parse", "refs/heads/"+branch)
	return err == nil && out == sha
}
