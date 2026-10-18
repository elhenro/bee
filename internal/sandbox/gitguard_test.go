package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUnknownScopeFailsClosed(t *testing.T) {
	cmd := []string{"bash", "-c", "echo pwned"}
	wrapped, err := Wrap(Policy{Scope: "workspace-wrte", Cwd: t.TempDir()}, cmd)
	if err == nil {
		t.Fatal("unknown scope must report an error")
	}
	if len(wrapped) <= 3 {
		t.Fatalf("callers treat <=3 args as unwrapped; got %v", wrapped)
	}
	out, _ := exec.Command(wrapped[0], wrapped[1:]...).CombinedOutput()
	if strings.Contains(string(out), "pwned") || !strings.Contains(string(out), "unknown scope") {
		t.Fatalf("refusing command misbehaved: %q", out)
	}
}

func TestGitProtected(t *testing.T) {
	dir := t.TempDir()
	if got := gitProtected(dir); len(got) != 0 {
		t.Fatalf("no .git, nothing to protect, got %v", got)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(gitProtected(dir), "\n")
	if !strings.Contains(got, filepath.Join(".git", "hooks")) || !strings.Contains(got, filepath.Join(".git", "config")) {
		t.Fatalf("hooks/config missing: %q", got)
	}
	if strings.Contains(got, "objects") || strings.Contains(got, "refs") || strings.Contains(got, "index") {
		t.Fatalf("objects/refs/index must stay writable: %q", got)
	}
}

// real sandbox-exec run: hooks and config are read-only, commits still work.
func TestSeatbeltProtectsGitHooksAndConfig(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("seatbelt only")
	}
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("no sandbox-exec")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git binary")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run := func(script string) (string, error) {
		w, err := Wrap(Policy{Scope: WorkspaceWrite, Cwd: dir}, []string{"bash", "-c", script})
		if err != nil {
			t.Fatalf("wrap: %v", err)
		}
		c := exec.Command(w[0], w[1:]...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		return string(out), err
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	if out, err := run("echo '#!/bin/sh' > .git/hooks/pre-commit"); err == nil {
		t.Errorf("hook write allowed: %s", out)
	}
	if out, err := run("echo '[core]' >> .git/config"); err == nil {
		t.Errorf("config write allowed: %s", out)
	}
	script := "echo x > f.txt && git add f.txt && git -c user.name=t -c user.email=t@t commit -qm init"
	if out, err := run(script); err != nil {
		t.Errorf("commit inside sandbox failed: %v %s", err, out)
	}
}
