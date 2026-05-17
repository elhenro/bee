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
