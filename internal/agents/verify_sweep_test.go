//go:build !windows

package agents

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/beehome"
)

func deadPid(t *testing.T) int {
	t.Helper()
	c := exec.Command("true")
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	return c.Process.Pid
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestSweepVerifyRemovesAbandonedCheckouts(t *testing.T) {
	f := newFixture(t)
	sha := git(t, f.repo, "rev-parse", "HEAD")

	// a merger killed mid-verify: checkout and worktree entry stay behind
	stale, _, err := pristine(f.repo, sha)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale+ownerExt, []byte(strconv.Itoa(deadPid(t))), 0o600); err != nil {
		t.Fatal(err)
	}
	// a verify running right now in this process
	live, cleanup, err := pristine(f.repo, sha)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	base, _ := beehome.Join("verify")
	oldUnmarked := filepath.Join(base, "v-legacy-old")
	newUnmarked := filepath.Join(base, "v-legacy-new")
	for _, d := range []string{oldUnmarked, newUnmarked} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(oldUnmarked, old, old); err != nil {
		t.Fatal(err)
	}

	sweepVerify(f.repo)

	if exists(stale) || exists(stale+ownerExt) {
		t.Error("abandoned checkout survived the sweep")
	}
	if !exists(live) {
		t.Error("checkout of a live verify was swept")
	}
	if exists(oldUnmarked) {
		t.Error("old unmarked checkout survived")
	}
	if !exists(newUnmarked) {
		t.Error("fresh unmarked checkout was swept, it may belong to an older running build")
	}
	list := git(t, f.repo, "worktree", "list", "--porcelain")
	if strings.Contains(list, filepath.Base(stale)) {
		t.Errorf("worktree entry for %s not pruned:\n%s", stale, list)
	}
	if !strings.Contains(list, filepath.Base(live)) {
		t.Errorf("live worktree entry lost:\n%s", list)
	}
}

func TestPristineCleanupDropsMarker(t *testing.T) {
	f := newFixture(t)
	dir, cleanup, err := pristine(f.repo, git(t, f.repo, "rev-parse", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	if !exists(dir + ownerExt) {
		t.Fatal("pristine must record its owner")
	}
	cleanup()
	if exists(dir) || exists(dir+ownerExt) {
		t.Error("cleanup left the checkout or its marker")
	}
}
