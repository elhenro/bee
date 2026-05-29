package main

import (
	"context"
	"encoding/json"
	"os"
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

// TestBenchRepeatRuns drives --runs N: the task runs twice, the result folds to
// a mean with per-run samples recorded so a tuner can gauge noise.
func TestBenchRepeatRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e under -short")
	}
	bin := buildBee(t)
	home, _ := tmpHomeSessions(t)
	fixture := absFixture(t, "bench_write_turn.json")

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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	res, err := bench.RunSuite(ctx, []bench.Task{task}, bench.Options{
		BeeBin:   bin,
		Label:    "repeat",
		Timeout:  30 * time.Second,
		Runs:     2,
		ExtraEnv: extraEnv,
	})
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	if res.Runs != 2 {
		t.Errorf("SuiteResult.Runs = %d, want 2", res.Runs)
	}
	tr := res.Tasks[0]
	if len(tr.Samples) != 2 {
		t.Fatalf("want 2 samples, got %d (%v)", len(tr.Samples), tr.Samples)
	}
	if tr.Spread < 0 {
		t.Errorf("spread must be ≥0, got %v", tr.Spread)
	}
}

// TestBenchHoldoutSegregation runs a held-out slice through the same scripted
// provider and confirms AttachHoldout reports it under the Holdout* fields,
// separate from the main suite, never folded into the main aggregate.
func TestBenchHoldoutSegregation(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e under -short")
	}
	bin := buildBee(t)
	home, _ := tmpHomeSessions(t)
	fixture := absFixture(t, "bench_write_turn.json")

	extraEnv := []string{
		"BEE_TEST_PROVIDER=scripted",
		"BEE_TEST_SCRIPT=" + fixture,
		"HOME=" + home,
		"BEE_HOME=" + home,
		"BEE_SKILLS_DIR=" + filepath.Join(home, "skills"),
		"BEE_BIN_DIR=" + filepath.Join(home, "bin"),
	}
	opt := bench.Options{BeeBin: bin, Label: "scripted", Timeout: 30 * time.Second, ExtraEnv: extraEnv}

	main := bench.Task{
		ID:     "main-marker",
		Prompt: "write verbose-marker into out.txt",
		Checks: []bench.Check{{Kind: "grep", File: "$SANDBOX/out.txt", Pattern: "verbose-marker"}},
		Budget: bench.Budget{MaxTurns: 8},
	}

	holdoutDir := writeHoldoutDir(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	res, err := bench.RunSuite(ctx, []bench.Task{main}, opt)
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	if err := bench.AttachHoldout(ctx, &res, holdoutDir, opt); err != nil {
		t.Fatalf("AttachHoldout: %v", err)
	}

	if len(res.Tasks) != 1 || res.Tasks[0].ID != "main-marker" {
		t.Fatalf("main suite should hold only main-marker, got %+v", res.Tasks)
	}
	if len(res.HoldoutTasks) != 1 || res.HoldoutTasks[0].ID != "held-marker" {
		t.Fatalf("held-out slice should hold only held-marker, got %+v", res.HoldoutTasks)
	}
	if res.HoldoutAggregate == 0 {
		t.Errorf("held-out aggregate should be populated, got 0")
	}
}

// writeHoldoutDir drops one held-out task spec into a temp dir for AttachHoldout
// to load. The scripted provider writes out.txt, so the grep passes.
func writeHoldoutDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	spec := bench.Task{
		ID:     "held-marker",
		Prompt: "write verbose-marker into out.txt",
		Checks: []bench.Check{{Kind: "grep", File: "$SANDBOX/out.txt", Pattern: "verbose-marker"}},
		Budget: bench.Budget{MaxTurns: 8},
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "held-marker.json"), raw, 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return dir
}
