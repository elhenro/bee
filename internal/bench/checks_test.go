package bench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunChecks_CmdAndGrep(t *testing.T) {
	sandbox := t.TempDir()
	if err := os.WriteFile(filepath.Join(sandbox, "f.txt"), []byte("hello verbose world"), 0o644); err != nil {
		t.Fatal(err)
	}
	checks := []Check{
		{Kind: "cmd", Run: "test -f $SANDBOX/f.txt", ExpectExit: 0},
		{Kind: "grep", File: "$SANDBOX/f.txt", Pattern: "verbose"},
	}
	results, ok := RunChecks(checks, sandbox, 30*time.Second)
	if !ok {
		t.Fatalf("expected all pass, got %+v", results)
	}
}

func TestRunChecks_CmdWrongExit(t *testing.T) {
	sandbox := t.TempDir()
	checks := []Check{{Kind: "cmd", Run: "false", ExpectExit: 0}}
	_, ok := RunChecks(checks, sandbox, 30*time.Second)
	if ok {
		t.Error("expected failure on exit 1 vs want 0")
	}
}

func TestRunChecks_GrepMiss(t *testing.T) {
	sandbox := t.TempDir()
	os.WriteFile(filepath.Join(sandbox, "f.txt"), []byte("nothing here"), 0o644)
	checks := []Check{{Kind: "grep", File: "$SANDBOX/f.txt", Pattern: "verbose"}}
	_, ok := RunChecks(checks, sandbox, 30*time.Second)
	if ok {
		t.Error("expected grep miss to fail")
	}
}

func TestRunChecks_UnknownKind(t *testing.T) {
	_, ok := RunChecks([]Check{{Kind: "bogus"}}, t.TempDir(), 30*time.Second)
	if ok {
		t.Error("unknown kind must not pass")
	}
}

func TestRunChecks_CmdTimesOut(t *testing.T) {
	checks := []Check{{Kind: "cmd", Run: "sleep 5", ExpectExit: 0}}
	results, ok := RunChecks(checks, t.TempDir(), 100*time.Millisecond)
	if ok {
		t.Error("a hung cmd check must fail, not hang")
	}
	if len(results) != 1 || !strings.Contains(results[0].Detail, "timed out") {
		t.Errorf("want timeout detail, got %+v", results)
	}
}
