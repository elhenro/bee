package main

import "testing"

// stalledStep must increment on identical not-met reasons and reset on a
// changed reason, so the loop only bails on genuine no-progress spinning.
func TestStalledStep(t *testing.T) {
	streak, prev := 0, ""

	streak, prev = stalledStep(streak, prev, "missing file")
	if streak != 1 {
		t.Fatalf("first not-met: streak = %d, want 1", streak)
	}
	streak, prev = stalledStep(streak, prev, "missing file")
	if streak != 2 {
		t.Fatalf("same reason: streak = %d, want 2", streak)
	}
	streak, prev = stalledStep(streak, prev, "build fails")
	if streak != 1 || prev != "build fails" {
		t.Fatalf("changed reason: streak = %d prev = %q, want 1 and updated", streak, prev)
	}
	streak, _ = stalledStep(streak, prev, "build fails")
	if streak != 2 {
		t.Fatalf("same reason again: streak = %d, want 2", streak)
	}
}
