package shell

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHappyPath(t *testing.T) {
	res, err := New().Run(context.Background(), map[string]any{"command": "echo hello"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "hello") {
		t.Fatalf("missing output: %q", res.Content)
	}
}

func TestNonZeroExit(t *testing.T) {
	res, err := New().Run(context.Background(), map[string]any{
		"command": "echo oops >&2 ; exit 7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for non-zero exit")
	}
	if !strings.Contains(res.Content, "exit 7") {
		t.Fatalf("missing exit code in output: %s", res.Content)
	}
	if !strings.Contains(res.Content, "oops") {
		t.Fatalf("missing stderr in output: %s", res.Content)
	}
}

func TestTimeoutFires(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash + sleep + ctx-kill not reliable on Windows runners")
	}
	start := time.Now()
	res, err := New().Run(context.Background(), map[string]any{
		"command":         "sleep 5",
		"timeout_seconds": 1,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected IsError on timeout")
	}
	if !strings.Contains(res.Content, "timeout") {
		t.Fatalf("missing timeout marker: %s", res.Content)
	}
	if elapsed > 4*time.Second {
		t.Fatalf("timeout did not fire fast: %v", elapsed)
	}
}

func TestLargeOutputTruncated(t *testing.T) {
	// emit ~30 KB
	res, err := New().Run(context.Background(), map[string]any{
		"command": "head -c 30000 /dev/zero | tr '\\0' x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "[…truncated]") {
		t.Fatalf("missing truncate marker in %d-byte output", len(res.Content))
	}
	// must not exceed cap + marker
	if len(res.Content) > maxOutputBytes+len(truncMarker)+1 {
		t.Fatalf("output exceeds cap: %d", len(res.Content))
	}
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := New().Run(ctx, map[string]any{"command": "echo hi"})
	if err == nil && !res.IsError {
		t.Fatal("expected error from canceled ctx")
	}
}

func TestMissingCommand(t *testing.T) {
	res, _ := New().Run(context.Background(), map[string]any{"command": ""})
	if !res.IsError {
		t.Fatal("empty command should error")
	}
}

func TestCwdHonored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git-bash pwd returns POSIX path on Windows, not the Windows dir")
	}
	dir := t.TempDir()
	res, err := New().Run(context.Background(), map[string]any{
		"command": "pwd",
		"cwd":     dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	// macOS resolves /var -> /private/var; check suffix instead
	if !strings.Contains(res.Content, dir) && !strings.Contains(res.Content, strings.TrimPrefix(dir, "/private")) {
		t.Fatalf("cwd not honored: got %q, want substring %q", res.Content, dir)
	}
}

func TestSpec(t *testing.T) {
	s := New().Spec()
	if s.Name != "bash" {
		t.Fatalf("wrong name: %s", s.Name)
	}
	if s.Schema == nil {
		t.Fatal("nil schema")
	}
}

// TestShell_KillsBackgroundedChildren verifies that timeout kills the entire
// process group, not just the bash leader. Without the fix, a backgrounded
// `sleep` becomes an orphan after bash dies and continues running until its
// natural exit. We grep for a unique sentinel arg so the check is robust to
// other sleeps on the host.
func TestShell_KillsBackgroundedChildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process groups + pgrep not portable to Windows")
	}
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep not on PATH")
	}
	// write the background child's pid to a sentinel file we can check
	// directly. pgrep on macOS won't find a stray sleep by argv if extra
	// args aren't valid for sleep, so a pid file is the robust signal.
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	cmdLine := "sleep 30 & echo $! > " + pidFile + "; wait"
	done := make(chan struct{}, 1)
	go func() {
		_, _ = New().Run(context.Background(), map[string]any{
			"command":         cmdLine,
			"timeout_seconds": 1,
		})
		done <- struct{}{}
	}()
	// the bug: cmd.Run() returns only when stdout pipe closes, which is
	// when sleep finally exits at 30s. With the fix, kill happens at 1s
	// + WaitDelay, well under our 5s probe window below.
	deadline := time.After(5 * time.Second)
	select {
	case <-done:
	case <-deadline:
	}
	// probe the recorded child pid directly. signal 0 = existence check.
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("child pid file missing: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("bad pid in sentinel: %q", raw)
	}
	alive := exec.Command("kill", "-0", strconv.Itoa(pid)).Run() == nil
	if alive {
		_ = exec.Command("kill", "-9", strconv.Itoa(pid)).Run()
		t.Fatalf("backgrounded sleep (pid=%d) survived timeout", pid)
	}
}

// TestUserRCAliasZsh proves an alias declared in a fake .zshrc is expanded
// when the tool runs with UseUserRC=true. Skipped if zsh is absent.
func TestUserRCAliasZsh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("zsh not standard on Windows runners")
	}
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not on PATH")
	}
	rc := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(rc, []byte("alias greet='echo aliased-hi'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := NewWithOptions(nil, Options{UseUserRC: true, Shell: "zsh", RCFile: rc})
	res, err := tool.Run(context.Background(), map[string]any{"command": "greet"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	if !strings.Contains(res.Content, "aliased-hi") {
		t.Fatalf("alias not expanded: %q", res.Content)
	}
}

// TestUserRCAliasBash mirrors the zsh test for bash. bash needs the
// shopt expand_aliases prelude that buildInvocation injects.
func TestUserRCAliasBash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("aliases under git-bash on Windows are flaky")
	}
	rc := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(rc, []byte("alias greet='echo aliased-hi'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := NewWithOptions(nil, Options{UseUserRC: true, Shell: "bash", RCFile: rc})
	res, err := tool.Run(context.Background(), map[string]any{"command": "greet"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	if !strings.Contains(res.Content, "aliased-hi") {
		t.Fatalf("alias not expanded: %q", res.Content)
	}
}

// TestMissingRCFileSoftFails ensures pointing at a non-existent rc file does
// not break command execution — the prelude guards on [ -f rc ].
func TestMissingRCFileSoftFails(t *testing.T) {
	tool := NewWithOptions(nil, Options{
		UseUserRC: true,
		Shell:     "bash",
		RCFile:    "/nonexistent/path/to/.bashrc",
	})
	res, err := tool.Run(context.Background(), map[string]any{"command": "echo ok"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	if !strings.Contains(res.Content, "ok") {
		t.Fatalf("missing output: %q", res.Content)
	}
}
