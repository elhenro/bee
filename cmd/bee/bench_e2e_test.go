package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/bench"
)

// TestBenchScriptedSuite drives the full bench runTask path — setup, spawn real
// binary through the /goal loop, read the session transcript, run checks, score
// — against the scripted provider so it stays deterministic and offline.
func TestBenchScriptedSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e under -short")
	}
	bin := buildBee(t)
	home, _ := tmpHomeSessions(t)
	fixture := absFixture(t, "bench_write_turn.json")

	// scripted env minus BEE_SESSIONS_DIR — the bench runner owns the sessions
	// dir per task so it can read back the transcript it just produced.
	extraEnv := []string{
		"BEE_TEST_PROVIDER=scripted",
		"BEE_TEST_SCRIPT=" + fixture,
		"HOME=" + home,
		"BEE_HOME=" + home,
		"BEE_SKILLS_DIR=" + filepath.Join(home, "skills"),
		"BEE_BIN_DIR=" + filepath.Join(home, "bin"),
	}

	task := bench.Task{
		ID:     "write-marker",
		Prompt: "write verbose-marker into out.txt",
		Checks: []bench.Check{
			{Kind: "grep", File: "$SANDBOX/out.txt", Pattern: "verbose-marker"},
		},
		Budget: bench.Budget{MaxTurns: 8},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := bench.RunSuite(ctx, []bench.Task{task}, bench.Options{
		BeeBin:   bin,
		Label:    "scripted",
		Timeout:  30 * time.Second,
		ExtraEnv: extraEnv,
	})
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	if len(res.Tasks) != 1 {
		t.Fatalf("want 1 task result, got %d", len(res.Tasks))
	}
	tr := res.Tasks[0]
	if tr.Err != "" {
		t.Fatalf("task error: %s", tr.Err)
	}
	if !tr.Succeeded {
		t.Errorf("task should succeed (grep out.txt), reason=%q metrics=%+v", tr.Reason, tr.Metrics)
	}
	if tr.Metrics.ToolCalls < 1 {
		t.Errorf("expected ≥1 tool call, got %d", tr.Metrics.ToolCalls)
	}
	if tr.Dims.Success != 1 {
		t.Errorf("success dim = %v, want 1", tr.Dims.Success)
	}
}
