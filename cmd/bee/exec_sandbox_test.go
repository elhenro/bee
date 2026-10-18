package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/tools/usertool"
)

func TestExecSandboxUnconfinedIsEmpty(t *testing.T) {
	if got := execSandbox(config.Defaults(), t.TempDir()); len(got) != 0 {
		t.Fatalf("default scope must not wrap, got %d options", len(got))
	}
}

// a read-only scope must stop an exec skill from writing, where it used to run
// as bare bash.
func TestExecSandboxReadOnlyBlocksWrite(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("needs sandbox-exec")
	}
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec missing")
	}
	cfg := config.Defaults()
	cfg.Sandbox.Scope = "read-only"
	target := filepath.Join(t.TempDir(), "probe")
	tool, err := usertool.New("probe", `echo x > "$1"`, "", execSandbox(cfg, t.TempDir())...)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tool.Run(context.Background(), map[string]any{"args": target})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("write under read-only should fail, got %q", res.Content)
	}
	if _, serr := os.Stat(target); serr == nil {
		t.Fatal("probe file was written despite read-only scope")
	}
}
