package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// makeFakeBin writes a small shell script that echoes its argv (one per
// line) to stdout and exits 0. On windows, tests are skipped — fine since
// bee is primarily run on unix devboxes.
func makeFakeBin(t *testing.T, dir, name, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary not supported on windows")
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAvailable_MissingStore(t *testing.T) {
	dir := t.TempDir()
	if _, ok := Available(dir); ok {
		t.Fatalf("Available should be false when .codegraph/codegraph.db absent")
	}
}

func TestAvailable_HasStoreAndBin(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".codegraph", "codegraph.db"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	makeFakeBin(t, binDir, "codegraph", `echo ok`)
	t.Setenv("PATH", binDir)
	bin, ok := Available(cwd)
	if !ok {
		t.Fatalf("Available should be true with store+bin")
	}
	if !strings.HasSuffix(bin, "/codegraph") {
		t.Fatalf("bin %q should end with /codegraph", bin)
	}
}

func TestRun_PassesArgvToBin(t *testing.T) {
	cwd := t.TempDir()
	binDir := t.TempDir()
	bin := makeFakeBin(t, binDir, "codegraph", `for a in "$@"; do echo "$a"; done`)
	tool := New(cwd, bin)

	res, err := tool.Run(context.Background(), map[string]any{
		"op":     "search",
		"target": "Foo",
		"args":   []any{"--limit", "5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	want := "search\nFoo\n--limit\n5\n"
	if res.Content != want {
		t.Fatalf("argv mismatch\n got: %q\nwant: %q", res.Content, want)
	}
}

func TestRun_RejectsUnknownOp(t *testing.T) {
	tool := New(t.TempDir(), "/bin/false")
	res, _ := tool.Run(context.Background(), map[string]any{"op": "install"})
	if !res.IsError {
		t.Fatalf("install should be rejected")
	}
	if !strings.Contains(res.Content, "not allowed") {
		t.Fatalf("want 'not allowed' message, got: %s", res.Content)
	}
}

func TestRun_MissingOp(t *testing.T) {
	tool := New(t.TempDir(), "/bin/false")
	res, _ := tool.Run(context.Background(), map[string]any{})
	if !res.IsError || !strings.Contains(res.Content, "missing op") {
		t.Fatalf("want missing-op error, got: %+v", res)
	}
}

func TestRun_StatusAllowsEmptyTarget(t *testing.T) {
	cwd := t.TempDir()
	binDir := t.TempDir()
	bin := makeFakeBin(t, binDir, "codegraph", `echo "indexed: 42"`)
	tool := New(cwd, bin)
	res, err := tool.Run(context.Background(), map[string]any{"op": "status"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("status should succeed with no target: %s", res.Content)
	}
	if !strings.Contains(res.Content, "indexed: 42") {
		t.Fatalf("want stdout passthrough, got: %s", res.Content)
	}
}

func TestRun_BinExitsNonZero(t *testing.T) {
	cwd := t.TempDir()
	binDir := t.TempDir()
	bin := makeFakeBin(t, binDir, "codegraph", `echo "boom" 1>&2; exit 3`)
	tool := New(cwd, bin)
	res, _ := tool.Run(context.Background(), map[string]any{"op": "search", "target": "x"})
	if !res.IsError {
		t.Fatalf("non-zero exit should be IsError")
	}
	if !strings.Contains(res.Content, "boom") {
		t.Fatalf("want stderr in content, got: %s", res.Content)
	}
}
