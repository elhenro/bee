//go:build !windows

package zzz

import (
	"os"
	"path/filepath"
	"testing"
)

// an agent can plant hooks and an fsmonitor command in the repo; the host's
// own commit and status calls afterwards must not run them.
func TestHostGitDoesNotRunPlantedHooks(t *testing.T) {
	repo := newRepo(t)
	marker := filepath.Join(t.TempDir(), "ran")
	script := "#!/bin/sh\ntouch " + marker + "\n"
	hook := filepath.Join(repo, ".git", "hooks", "post-commit")
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	fsm := filepath.Join(repo, "fsm.sh")
	if err := os.WriteFile(fsm, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := gitRun(repo, "config", "core.fsmonitor", fsm); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repo, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := IsClean(repo); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitAll(repo, "feat: x", false, false); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a planted hook or fsmonitor ran on the host git path")
	}
}
