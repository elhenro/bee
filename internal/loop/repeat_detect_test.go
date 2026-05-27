package loop

import (
	"testing"

	"github.com/elhenro/bee/internal/types"
)

// signatureFor must collapse semantically-equal Inputs even when keys are
// declared in different order (Go map iteration is randomized).
func TestSignatureFor_KeyOrderInsensitive(t *testing.T) {
	a := types.ToolUse{Name: "read", Input: map[string]any{"path": "/etc/hosts", "lines": "1-10"}}
	b := types.ToolUse{Name: "read", Input: map[string]any{"lines": "1-10", "path": "/etc/hosts"}}
	if signatureFor(a) != signatureFor(b) {
		t.Fatalf("sigs differ for key-reordered inputs: %v vs %v", signatureFor(a), signatureFor(b))
	}
}

// nil vs empty Input collapse to the same sentinel.
func TestSignatureFor_EmptyInput(t *testing.T) {
	a := types.ToolUse{Name: "ls"}
	b := types.ToolUse{Name: "ls", Input: map[string]any{}}
	if signatureFor(a) != signatureFor(b) {
		t.Fatalf("nil/empty sigs differ: %v vs %v", signatureFor(a), signatureFor(b))
	}
}

// Different args, same tool → distinct sigs.
func TestSignatureFor_DifferentArgs(t *testing.T) {
	a := types.ToolUse{Name: "read", Input: map[string]any{"path": "a"}}
	b := types.ToolUse{Name: "read", Input: map[string]any{"path": "b"}}
	if signatureFor(a) == signatureFor(b) {
		t.Fatalf("expected distinct sigs for distinct args")
	}
}

// Two-strike fires only when the same sig errors twice in a row.
func TestTracker_TwoStrike(t *testing.T) {
	tr := newRepeatTracker()
	u := types.ToolUse{ID: "1", Name: "read", Input: map[string]any{"path": "/nope"}}
	if tr.Observe(u, true).IsTwoStrike {
		t.Fatalf("first error must not trigger two-strike")
	}
	if !tr.Observe(u, true).IsTwoStrike {
		t.Fatalf("second consecutive error on same sig must trigger two-strike")
	}
}

// Two-strike does NOT fire if anything intervenes — success or different sig.
func TestTracker_TwoStrikeBrokenByOther(t *testing.T) {
	tr := newRepeatTracker()
	u := types.ToolUse{ID: "1", Name: "read", Input: map[string]any{"path": "/nope"}}
	other := types.ToolUse{ID: "2", Name: "bash", Input: map[string]any{"command": "ls"}}
	tr.Observe(u, true)
	tr.Observe(other, false)
	if tr.Observe(u, true).IsTwoStrike {
		t.Fatalf("non-consecutive errors must not trigger two-strike")
	}
}

// Per-tool failure streak counts errors on the same tool name regardless of args.
func TestTracker_PerToolFailureStreak(t *testing.T) {
	tr := newRepeatTracker()
	args1 := types.ToolUse{ID: "1", Name: "bash", Input: map[string]any{"command": "a"}}
	args2 := types.ToolUse{ID: "2", Name: "bash", Input: map[string]any{"command": "b"}}
	tr.Observe(args1, true)
	obs := tr.Observe(args2, true)
	if obs.ConsecutiveSameToolFailures != 2 {
		t.Fatalf("expected streak=2, got %d", obs.ConsecutiveSameToolFailures)
	}
	// success resets the streak.
	tr.Observe(args1, false)
	obs = tr.Observe(args2, true)
	if obs.ConsecutiveSameToolFailures != 1 {
		t.Fatalf("expected streak reset to 1 after success, got %d", obs.ConsecutiveSameToolFailures)
	}
}

// ConsecutiveSameSigFailures generalizes two-strike to N: count of identical
// failing calls in an unbroken streak. Reset by success or different sig.
func TestTracker_ConsecutiveSameSigFailures(t *testing.T) {
	tr := newRepeatTracker()
	u := types.ToolUse{ID: "1", Name: "write", Input: map[string]any{"path": "/tmp/x"}}
	for i := 1; i <= 5; i++ {
		obs := tr.Observe(u, true)
		if obs.ConsecutiveSameSigFailures != i {
			t.Fatalf("iter %d: want streak=%d, got %d", i, i, obs.ConsecutiveSameSigFailures)
		}
	}
	// success in the middle resets the streak.
	tr.Observe(u, false)
	obs := tr.Observe(u, true)
	if obs.ConsecutiveSameSigFailures != 1 {
		t.Fatalf("after success: want streak reset to 1, got %d", obs.ConsecutiveSameSigFailures)
	}
}

// different-sig failure also breaks the same-sig streak.
func TestTracker_SameSigStreakBrokenByDifferentSig(t *testing.T) {
	tr := newRepeatTracker()
	u := types.ToolUse{ID: "1", Name: "write", Input: map[string]any{"path": "/tmp/x"}}
	other := types.ToolUse{ID: "2", Name: "write", Input: map[string]any{"path": "/tmp/y"}}
	tr.Observe(u, true)
	tr.Observe(other, true)
	obs := tr.Observe(u, true)
	if obs.ConsecutiveSameSigFailures != 1 {
		t.Fatalf("after intervening sig: want streak=1, got %d", obs.ConsecutiveSameSigFailures)
	}
}

// RepeatCount tracks the same sig across the whole Run (no sliding window).
func TestTracker_RepeatCount(t *testing.T) {
	tr := newRepeatTracker()
	u := types.ToolUse{ID: "1", Name: "read", Input: map[string]any{"path": "x"}}
	for i := 1; i <= 5; i++ {
		obs := tr.Observe(u, false)
		if obs.RepeatCount != i {
			t.Fatalf("iter %d: want repeat=%d, got %d", i, i, obs.RepeatCount)
		}
	}
}
