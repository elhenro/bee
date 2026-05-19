package zzz

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/loop"
)

// stubRunner replays a queued list of (text, error, files) per iteration so
// Drive can be exercised without booting a real provider/engine. files are
// written to the run's RepoRoot before Run returns to simulate the model
// editing the tree.
type stubRunner struct {
	mu     sync.Mutex
	turns  []stubTurn
	idx    int
	dir    string
	total  cost.Summary
	tokens int
}

type stubTurn struct {
	text  string
	err   error
	write map[string]string // path → content
	in    int
	out   int
}

func (s *stubRunner) Run(_ context.Context, _ string) (loop.RunResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx >= len(s.turns) {
		return loop.RunResult{FinalText: "BLOCKED: stub exhausted"}, nil
	}
	t := s.turns[s.idx]
	s.idx++
	for p, c := range t.write {
		full := filepath.Join(s.dir, p)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, []byte(c), 0o644)
	}
	s.total.Input += t.in
	s.total.Output += t.out
	return loop.RunResult{FinalText: t.text}, t.err
}

func (s *stubRunner) CostTotal() cost.Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.total
}

// stubUI captures phase/iter transitions and println lines.
type stubUI struct {
	mu      sync.Mutex
	phases  []string
	iters   []int
	logs    []string
	tokens  TokenStat
	commits int
	summary *Run
	steer   chan Steer
}

func (u *stubUI) SetIter(n, _ int)       { u.mu.Lock(); u.iters = append(u.iters, n); u.mu.Unlock() }
func (u *stubUI) SetPhase(p string)      { u.mu.Lock(); u.phases = append(u.phases, p); u.mu.Unlock() }
func (u *stubUI) SetTokens(t TokenStat)  { u.mu.Lock(); u.tokens = t; u.mu.Unlock() }
func (u *stubUI) IncCommits()            { u.mu.Lock(); u.commits++; u.mu.Unlock() }
func (u *stubUI) Println(s string)       { u.mu.Lock(); u.logs = append(u.logs, s); u.mu.Unlock() }
func (u *stubUI) RenderSummary(r *Run)   { u.mu.Lock(); u.summary = r; u.mu.Unlock() }
func (u *stubUI) Steer() <-chan Steer    { return u.steer }

// setupDriveTest builds a fresh git repo + HOME tempdir and returns the
// run skeleton plus repo path.
func setupDriveTest(t *testing.T) (*Run, string) {
	t.Helper()
	repo := newRepo(t)
	t.Setenv("BEE_HOME", t.TempDir())
	id := NewID()
	r := &Run{
		ID:        id,
		Objective: "stub objective",
		Branch:    "zzz/" + id,
		Mode:      ModeBranch,
		RepoRoot:  repo,
		StartedAt: time.Now().UTC(),
		Status:    StatusRunning,
	}
	if err := SaveMeta(r); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	return r, repo
}

// TestDrive_CommitFailResetsTree confirms that a Commit() failure leaves the
// next iteration's preflight clean — the bug being that the old code left
// the tree dirty when a pre-commit hook rejected the commit, causing iter
// N+1 to commit iter N's stale changes under iter N+1's subject.
func TestDrive_CommitFailResetsTree(t *testing.T) {
	run, repo := setupDriveTest(t)
	// install a pre-commit hook that always fails so Commit() errors.
	hookDir := filepath.Join(repo, ".git", "hooks")
	hookPath := filepath.Join(hookDir, "pre-commit")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("hook: %v", err)
	}
	runner := &stubRunner{
		dir: repo,
		turns: []stubTurn{
			{text: "feat: first change", write: map[string]string{"a.txt": "1"}, in: 100, out: 50},
		},
	}
	ui := &stubUI{}
	cfg := Config{MaxIterations: 1, MaxConsecutiveFails: 1}
	if err := Drive(context.Background(), nil, runner, cfg, run, ui); err != nil {
		t.Fatalf("Drive: %v", err)
	}
	clean, _ := IsClean(repo)
	if !clean {
		t.Errorf("tree should be clean after commit-fail rollback")
	}
	if run.Status != StatusFailed {
		t.Errorf("status want %s got %s", StatusFailed, run.Status)
	}
}

// TestDrive_DONEHonoredOnNoop confirms an agent saying "DONE: …" without
// writing anything ends the run immediately — previously a noop swallowed
// the DONE sentinel.
func TestDrive_DONEHonoredOnNoop(t *testing.T) {
	run, repo := setupDriveTest(t)
	runner := &stubRunner{
		dir: repo,
		turns: []stubTurn{
			{text: "DONE: objective already complete", in: 50, out: 20},
		},
	}
	ui := &stubUI{}
	cfg := Config{MaxIterations: 10}
	if err := Drive(context.Background(), nil, runner, cfg, run, ui); err != nil {
		t.Fatal(err)
	}
	if run.Status != StatusCompleted {
		t.Errorf("status want %s got %s (cause=%q)", StatusCompleted, run.Status, run.StopCause)
	}
	if run.IterCount != 1 {
		t.Errorf("expected one iter, got %d", run.IterCount)
	}
}

// TestDrive_NoopDoesNotCountAsFail confirms noop iters don't trip the
// consecutive-fail breaker — prior code killed plan-then-act sequences.
func TestDrive_NoopDoesNotCountAsFail(t *testing.T) {
	run, repo := setupDriveTest(t)
	runner := &stubRunner{
		dir: repo,
		turns: []stubTurn{
			{text: "thinking…", in: 10, out: 5},                                                            // noop
			{text: "still thinking", in: 10, out: 5},                                                       // noop
			{text: "feat: act now", write: map[string]string{"x.txt": "ok"}, in: 100, out: 50},             // commit
		},
	}
	ui := &stubUI{}
	cfg := Config{MaxIterations: 3, MaxConsecutiveFails: 2}
	if err := Drive(context.Background(), nil, runner, cfg, run, ui); err != nil {
		t.Fatal(err)
	}
	if run.Status != StatusCompleted {
		t.Errorf("want completed (max-iter), got %s cause=%s", run.Status, run.StopCause)
	}
	if len(run.Commits) != 1 {
		t.Errorf("want 1 commit, got %d", len(run.Commits))
	}
}

// TestDrive_BLOCKEDStashesPatch confirms a BLOCKED iter writes blocked-N.patch
// before resetting so partial work is recoverable.
func TestDrive_BLOCKEDStashesPatch(t *testing.T) {
	run, repo := setupDriveTest(t)
	runner := &stubRunner{
		dir: repo,
		turns: []stubTurn{
			{text: "BLOCKED: missing dependency", write: map[string]string{"partial.txt": "wip"}, in: 30, out: 10},
		},
	}
	ui := &stubUI{}
	cfg := Config{MaxIterations: 1, MaxConsecutiveFails: 1}
	if err := Drive(context.Background(), nil, runner, cfg, run, ui); err != nil {
		t.Fatal(err)
	}
	home, _ := HomeDir()
	patch, err := os.ReadFile(filepath.Join(home, "runs", run.ID, "blocked-1.patch"))
	if err != nil {
		t.Fatalf("blocked patch missing: %v", err)
	}
	if !strings.Contains(string(patch), "partial.txt") {
		t.Errorf("patch should mention partial.txt: %s", patch)
	}
	clean, _ := IsClean(repo)
	if !clean {
		t.Errorf("tree should be clean after BLOCKED reset")
	}
}

// TestDrive_ResumePreservesPriorTokens confirms a resumed run keeps its
// already-accumulated token totals rather than overwriting them with the
// fresh engine's costs.
func TestDrive_ResumePreservesPriorTokens(t *testing.T) {
	run, repo := setupDriveTest(t)
	// pretend a prior session already booked some tokens.
	run.Tokens = TokenStat{Input: 1000, Output: 500, USD: 0.05}
	if err := SaveMeta(run); err != nil {
		t.Fatal(err)
	}
	runner := &stubRunner{
		dir: repo,
		turns: []stubTurn{
			{text: "feat: more work", write: map[string]string{"y.txt": "ok"}, in: 200, out: 100},
		},
	}
	ui := &stubUI{}
	cfg := Config{MaxIterations: 1}
	if err := Drive(context.Background(), nil, runner, cfg, run, ui); err != nil {
		t.Fatal(err)
	}
	if run.Tokens.Input != 1200 {
		t.Errorf("input tokens want 1200 (1000 prior + 200 iter), got %d", run.Tokens.Input)
	}
	if run.Tokens.Output != 600 {
		t.Errorf("output tokens want 600, got %d", run.Tokens.Output)
	}
}

// TestDrive_GracefulStop confirms a closed stopCh ends the run as aborted
// before any iteration runs. Pre-closing avoids any goroutine timing race.
func TestDrive_GracefulStop(t *testing.T) {
	run, repo := setupDriveTest(t)
	runner := &stubRunner{dir: repo, turns: []stubTurn{{text: "should not run", in: 10, out: 5}}}
	stopCh := make(chan struct{})
	close(stopCh)
	ui := &stubUI{}
	cfg := Config{MaxIterations: 5}
	if err := Drive(context.Background(), stopCh, runner, cfg, run, ui); err != nil {
		t.Fatal(err)
	}
	if run.Status != StatusAborted {
		t.Errorf("want aborted, got %s", run.Status)
	}
	if run.IterCount != 0 {
		t.Errorf("no iter should have run, got IterCount=%d", run.IterCount)
	}
}

// TestDrive_HardErrorRetries confirms the retry budget is respected per iter.
func TestDrive_HardErrorRetries(t *testing.T) {
	run, repo := setupDriveTest(t)
	hardErr := errors.New("network blip")
	runner := &stubRunner{
		dir: repo,
		turns: []stubTurn{
			{err: hardErr},
			{err: hardErr},
		},
	}
	ui := &stubUI{}
	cfg := Config{MaxIterations: 1, HardErrorRetries: 2, MaxConsecutiveFails: 1}
	if err := Drive(context.Background(), nil, runner, cfg, run, ui); err != nil {
		t.Fatal(err)
	}
	if runner.idx != 2 {
		t.Errorf("retries=2 should consume 2 turns, used %d", runner.idx)
	}
	if run.Status != StatusFailed {
		t.Errorf("want failed, got %s", run.Status)
	}
}
