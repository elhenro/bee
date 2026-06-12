package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
)

// fakeSwarmProv is a minimal llm.Provider for maybeTriageShortCircuit unit
// tests. It records how many times Stream was called and emits a canned
// text+done event sequence (or an error on Stream itself).
type fakeSwarmProv struct {
	calls atomic.Int32
	reply string
	err   error
}

func (f *fakeSwarmProv) Name() string { return "fake-swarm" }
func (f *fakeSwarmProv) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	out := make(chan llm.Event, 2)
	out <- llm.Event{Type: llm.EventTextDelta, Delta: f.reply}
	out <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
	close(out)
	return out, nil
}

// TestMaybeTriageShortCircuit_Disabled bypasses triage entirely. Provider
// must never be touched and planner must never run.
func TestMaybeTriageShortCircuit_Disabled(t *testing.T) {
	prov := &fakeSwarmProv{reply: "simple"}
	called := false
	planner := func(ctx context.Context, task string) (loop.RunResult, error) {
		called = true
		return loop.RunResult{}, nil
	}
	short, err := maybeTriageShortCircuit(context.Background(), prov, "m", false, planner, "fix typo", io.Discard)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if short {
		t.Fatal("short-circuit should be false when disabled")
	}
	if called {
		t.Fatal("planner ran with triage disabled")
	}
	if prov.calls.Load() != 0 {
		t.Fatalf("provider called %d times, want 0 (triage disabled)", prov.calls.Load())
	}
}

// TestMaybeTriageShortCircuit_ProviderErrorsFallsThrough ensures a Stream
// error from the classifier defaults to the full hive, not a one-shot turn.
// Under-reviewing a real task is the failure mode we're guarding against.
func TestMaybeTriageShortCircuit_ProviderErrorsFallsThrough(t *testing.T) {
	prov := &fakeSwarmProv{err: errors.New("network down")}
	called := false
	planner := func(ctx context.Context, task string) (loop.RunResult, error) {
		called = true
		return loop.RunResult{}, nil
	}
	short, err := maybeTriageShortCircuit(context.Background(), prov, "m", true, planner, "refactor auth", io.Discard)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if short {
		t.Fatal("provider error must not short-circuit; hive should run")
	}
	if called {
		t.Fatal("planner ran despite triage classifier error")
	}
}

// TestMaybeTriageShortCircuit_ComplexFallsThrough covers the common path:
// classifier says "complex", planner must not run, hive must run.
func TestMaybeTriageShortCircuit_ComplexFallsThrough(t *testing.T) {
	prov := &fakeSwarmProv{reply: "complex"}
	called := false
	planner := func(ctx context.Context, task string) (loop.RunResult, error) {
		called = true
		return loop.RunResult{}, nil
	}
	short, err := maybeTriageShortCircuit(context.Background(), prov, "m", true, planner, "refactor auth", io.Discard)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if short {
		t.Fatal("complex task must not short-circuit")
	}
	if called {
		t.Fatal("planner ran despite triage=complex")
	}
	if prov.calls.Load() != 1 {
		t.Fatalf("expected exactly 1 classifier call, got %d", prov.calls.Load())
	}
}

// TestMaybeTriageShortCircuit_SimpleRunsPlannerAndEmits covers the
// short-circuit path end-to-end: classifier says "simple", planner runs once
// with the original task, and the printed output uses the canonical header.
func TestMaybeTriageShortCircuit_SimpleRunsPlannerAndEmits(t *testing.T) {
	prov := &fakeSwarmProv{reply: "simple"}
	var gotTask string
	called := 0
	planner := func(ctx context.Context, task string) (loop.RunResult, error) {
		called++
		gotTask = task
		return loop.RunResult{FinalText: "done in one turn"}, nil
	}
	var buf bytes.Buffer
	short, err := maybeTriageShortCircuit(context.Background(), prov, "m", true, planner, "rename foo to bar", &buf)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !short {
		t.Fatal("simple task must short-circuit")
	}
	if called != 1 {
		t.Fatalf("planner ran %d times, want 1", called)
	}
	if gotTask != "rename foo to bar" {
		t.Fatalf("planner saw task=%q, want %q", gotTask, "rename foo to bar")
	}
	if !strings.Contains(buf.String(), "## Single turn (triage=simple)") {
		t.Errorf("missing short-circuit header; got:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "done in one turn") {
		t.Errorf("missing planner output; got:\n%s", buf.String())
	}
}

// TestMaybeTriageShortCircuit_SimplePlannerErrorSurfaces checks that an
// error from the single-turn planner propagates through (true, err) so the
// caller exits with a non-zero status — silently masking planner failures
// would defeat the point of routing to it in the first place.
func TestMaybeTriageShortCircuit_SimplePlannerErrorSurfaces(t *testing.T) {
	prov := &fakeSwarmProv{reply: "simple"}
	want := errors.New("planner blew up")
	planner := func(ctx context.Context, task string) (loop.RunResult, error) {
		return loop.RunResult{}, want
	}
	var buf bytes.Buffer
	short, err := maybeTriageShortCircuit(context.Background(), prov, "m", true, planner, "x", &buf)
	if !short {
		t.Fatal("short-circuit should still be true even when planner errors")
	}
	if !errors.Is(err, want) {
		t.Fatalf("err=%v, want %v", err, want)
	}
	if !strings.Contains(buf.String(), "error: planner blew up") {
		t.Errorf("expected error in output; got:\n%s", buf.String())
	}
}

// TestSwarmSmoke builds bee and runs `bee swarm "do a thing" --workers 2`
// with BEE_TEST_PROVIDER=stub. Asserts exit 0 and a synthesis section in stdout.
func TestSwarmSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skip swarm smoke under -short")
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "bee"+exeSuffix())
	{
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("go build: %v", err)
		}
	}

	home := filepath.Join(tmp, "home")
	sessDir := filepath.Join(tmp, "sessions")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "swarm", "--workers", "2", "do a thing")
	cmd.Env = append(os.Environ(),
		"BEE_TEST_PROVIDER=stub",
		"HOME="+home,
		"BEE_HOME="+home,
		"BEE_SESSIONS_DIR="+sessDir,
		"BEE_SKILLS_DIR="+filepath.Join(home, "skills"),
		"BEE_BIN_DIR="+filepath.Join(home, "bin"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bee swarm failed: %v\noutput:\n%s", err, out)
	}
	s := string(out)
	if !strings.Contains(s, "Synthesis") {
		t.Errorf("expected '## Synthesis' header in stdout, got:\n%s", s)
	}
	if !strings.Contains(s, "stub:") {
		t.Errorf("expected stub provider response in output:\n%s", s)
	}
}
