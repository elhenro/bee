package bgreg

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteReadStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BEE_HOME", dir)

	want := Status{
		SessionID:    "abc-123",
		PID:          4242,
		State:        StateAwaiting,
		Task:         "refactor auth",
		LastResponse: "I propose splitting flow.go into…",
		UpdatedAt:    time.Now().UTC().Truncate(time.Second),
		StartedAt:    time.Now().UTC().Truncate(time.Second),
		Model:        "deepseek/deepseek-chat",
		Cwd:          "/home/h/proj",
	}
	if err := Write(want); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := Read("abc-123")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.SessionID != want.SessionID || got.State != want.State || got.LastResponse != want.LastResponse {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got, want)
	}

	if _, err := os.Stat(filepath.Join(dir, "sessions", "bg", "abc-123.status.json")); err != nil {
		t.Fatalf("status file missing: %v", err)
	}
}

func TestListAll(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BEE_HOME", dir)

	for _, id := range []string{"a", "b", "c"} {
		if err := Write(Status{SessionID: id, State: StateActive, UpdatedAt: time.Now()}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	got, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d", len(got))
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BEE_HOME", dir)
	_ = Write(Status{SessionID: "rm-me", State: StateDone, UpdatedAt: time.Now()})
	if err := Remove("rm-me"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := Read("rm-me"); err == nil {
		t.Fatalf("expected error after Remove, got nil")
	}
}

// TestWriteBumpsVersion verifies that each successful Write monotonically
// advances the on-disk Version, so concurrent readers can detect change.
func TestWriteBumpsVersion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BEE_HOME", dir)
	for i := 0; i < 3; i++ {
		if err := Write(Status{SessionID: "v", State: StateActive}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	got, err := Read("v")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Version != 3 {
		t.Fatalf("Version=%d want 3", got.Version)
	}
}

// TestUpdateAtomic exercises the read-modify-write helper. Two sequential
// Updates should produce Version=2 with both mutations applied.
func TestUpdateAtomic(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BEE_HOME", dir)
	if err := Write(Status{SessionID: "u", State: StateActive}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := Update("u", func(s *Status) error {
		s.State = StateAwaiting
		s.LastResponse = "first"
		return nil
	}); err != nil {
		t.Fatalf("update1: %v", err)
	}
	if err := Update("u", func(s *Status) error {
		s.LastResponse = s.LastResponse + ":second"
		return nil
	}); err != nil {
		t.Fatalf("update2: %v", err)
	}
	got, err := Read("u")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.State != StateAwaiting {
		t.Errorf("State=%s want awaiting", got.State)
	}
	if got.LastResponse != "first:second" {
		t.Errorf("LastResponse=%q want first:second", got.LastResponse)
	}
	if got.Version < 2 {
		t.Errorf("Version=%d want ≥2", got.Version)
	}
}
