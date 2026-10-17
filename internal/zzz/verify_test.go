package zzz

import (
	"context"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func gateTest(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("verify gate runs through sh")
	}
}

// a red verify keeps the change for the next iteration, which fixes it and
// commits both edits through a green gate.
func TestDrive_VerifyRedRepairsThenCommits(t *testing.T) {
	gateTest(t)
	run, repo := setupDriveTest(t)
	runner := &stubRunner{dir: repo, turns: []stubTurn{
		{text: "feat: half done", write: map[string]string{"a.txt": "1"}},
		{text: "fix: make verify pass", write: map[string]string{"fixed.txt": "ok"}},
	}}
	ui := &stubUI{}
	cfg := Config{MaxIterations: 2, Verify: []string{"test -f fixed.txt"}}
	if err := Drive(context.Background(), nil, runner, cfg, run, ui); err != nil {
		t.Fatal(err)
	}
	if len(run.Commits) != 1 || run.Hops != 0 {
		t.Fatalf("commits=%d hops=%d, want one commit and hops reset", len(run.Commits), run.Hops)
	}
	out, err := gitRun(repo, "show", "--name-only", "--format=", "HEAD")
	if err != nil || !strings.Contains(out, "a.txt") || !strings.Contains(out, "fixed.txt") {
		t.Fatalf("commit must carry the repaired change: %q err=%v", out, err)
	}
	if !slices.Contains(ui.phases, "verify-red") {
		t.Errorf("phases %v missing verify-red", ui.phases)
	}
	// the repair left the span open, the verified commit closed it as kept
	if want := []bool{true}; !equalBools(runner.marks, want) {
		t.Errorf("marks %v want %v", runner.marks, want)
	}
	notes, _ := ReadNotes(run.ID)
	if !strings.Contains(notes, "verify failed (hop 1/3)") {
		t.Errorf("next prompt must see the red verify, notes:\n%s", notes)
	}
}

func TestDrive_VerifyHopCapDropsChange(t *testing.T) {
	gateTest(t)
	run, repo := setupDriveTest(t)
	runner := &stubRunner{dir: repo, turns: []stubTurn{
		{text: "feat: try", write: map[string]string{"a.txt": "1"}},
		{text: "fix: try again", write: map[string]string{"a.txt": "2"}},
	}}
	cfg := Config{MaxIterations: 2, MaxHops: 1, MaxConsecutiveFails: 5, Verify: []string{"false"}}
	if err := Drive(context.Background(), nil, runner, cfg, run, &stubUI{}); err != nil {
		t.Fatal(err)
	}
	if clean, _ := IsClean(repo); !clean {
		t.Fatal("change past the hop cap must be rolled back")
	}
	if len(run.Commits) != 0 || run.Hops != 0 {
		t.Fatalf("commits=%d hops=%d", len(run.Commits), run.Hops)
	}
	if want := []bool{false}; !equalBools(runner.marks, want) {
		t.Errorf("marks %v want %v", runner.marks, want)
	}
}

func TestDrive_DONEWaitsForGreenVerify(t *testing.T) {
	gateTest(t)
	run, repo := setupDriveTest(t)
	runner := &stubRunner{dir: repo, turns: []stubTurn{
		{text: "DONE: shipped", write: map[string]string{"a.txt": "1"}},
		{text: "DONE: fixed", write: map[string]string{"fixed.txt": "ok"}},
	}}
	cfg := Config{MaxIterations: 5, Verify: []string{"test -f fixed.txt"}}
	if err := Drive(context.Background(), nil, runner, cfg, run, &stubUI{}); err != nil {
		t.Fatal(err)
	}
	if run.IterCount != 2 || len(run.Commits) != 1 || run.Status != StatusCompleted {
		t.Fatalf("iters=%d commits=%d status=%s cause=%q", run.IterCount, len(run.Commits), run.Status, run.StopCause)
	}
}
