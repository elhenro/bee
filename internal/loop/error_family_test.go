package loop

import (
	"testing"

	"github.com/elhenro/bee/internal/types"
)

// familyFixture is one real error shape: two messages that differ only in
// volatile detail (temp path, line number, pid, timing) and must therefore
// normalize to the same family key.
type familyFixture struct {
	name string
	tool string
	msgA string
	msgB string
}

// familyFixtures covers the six shapes the ticket names plus a pid/hex case.
var familyFixtures = []familyFixture{
	{"go build undefined", "bash",
		"# example.com/app\n./main.go:12:5: undefined: foo",
		"# example.com/app\n./main.go:99:3: undefined: foo"},
	{"go test failure", "bash",
		"--- FAIL: TestAdd (0.01s)\n    main_test.go:42: got 3, want 4",
		"--- FAIL: TestAdd (0.02s)\n    main_test.go:108: got 3, want 4"},
	{"permission denied temp path", "bash",
		"open /tmp/bee-a1b2/main.go: permission denied",
		"open /tmp/bee-z9y8/main.go: permission denied"},
	{"command not found", "bash",
		"bash: line 1: frobnicate: command not found",
		"bash: line 7: frobnicate: command not found"},
	{"no such file", "read",
		"open /tmp/abc123.txt: no such file or directory",
		"open /tmp/zzz999.txt: no such file or directory"},
	{"json parse", "bash",
		"invalid character 'x' looking for beginning of value",
		"invalid character 'y' looking for beginning of value"},
	{"pid and hex addr", "bash",
		"connect: pid 4242 at 0x7ffee3a2 failed",
		"connect: pid 9090 at 0x7ffee9b1 failed"},
}

// AC1: messages differing only by temp path / line number / pid / timing
// collapse onto one family key.
func TestErrorFamily_VolatileDetailCollides(t *testing.T) {
	for _, f := range familyFixtures {
		t.Run(f.name, func(t *testing.T) {
			a, b := errorFamilyKey(f.tool, f.msgA), errorFamilyKey(f.tool, f.msgB)
			if a != b {
				t.Fatalf("expected collision\n A=%q -> %s\n B=%q -> %s", f.msgA, a, f.msgB, b)
			}
		})
	}
}

// AC2: genuinely different error kinds get different keys — compile vs
// permission-denied, and every fixture against every other on the same tool.
func TestErrorFamily_DistinctShapesDiffer(t *testing.T) {
	compile := errorFamilyKey("bash", "# example.com/app\n./main.go:1:1: undefined: foo")
	perm := errorFamilyKey("bash", "open /tmp/x: permission denied")
	if compile == perm {
		t.Fatalf("compile and permission errors must not collide: %s", compile)
	}

	seen := map[string]string{}
	for _, f := range familyFixtures {
		k := errorFamilyKey(f.tool, f.msgA)
		if prev, ok := seen[k]; ok {
			t.Fatalf("%q collides with %q (key %s)", f.name, prev, k)
		}
		seen[k] = f.name
	}
}

// AC3: a success of the tool clears the family streak.
func TestErrorFamily_StreakResetsOnSuccess(t *testing.T) {
	tr := newRepeatTracker()
	u := types.ToolUse{ID: "1", Name: "bash", Input: map[string]any{"command": "ls /tmp/x"}}

	if obs := tr.ObserveWithResult(u, true, "open /tmp/a: permission denied"); obs.ConsecutiveSameFamilyFailures != 1 {
		t.Fatalf("want family streak 1, got %d", obs.ConsecutiveSameFamilyFailures)
	}
	// different args, same normalized family → the whole point of clustering.
	if obs := tr.ObserveWithResult(u, true, "open /tmp/b: permission denied"); obs.ConsecutiveSameFamilyFailures != 2 {
		t.Fatalf("want family streak 2, got %d", obs.ConsecutiveSameFamilyFailures)
	}
	if obs := tr.ObserveWithResult(u, false, "ok"); obs.ConsecutiveSameFamilyFailures != 0 {
		t.Fatalf("success must clear streak, got %d", obs.ConsecutiveSameFamilyFailures)
	}
	if obs := tr.ObserveWithResult(u, true, "open /tmp/c: permission denied"); obs.ConsecutiveSameFamilyFailures != 1 {
		t.Fatalf("want streak restarted at 1, got %d", obs.ConsecutiveSameFamilyFailures)
	}
}

// A failure in a different family restarts the streak at 1.
func TestErrorFamily_DifferentFamilyRestarts(t *testing.T) {
	tr := newRepeatTracker()
	u := types.ToolUse{ID: "1", Name: "bash", Input: map[string]any{"command": "x"}}
	tr.ObserveWithResult(u, true, "open /tmp/a: permission denied")
	if obs := tr.ObserveWithResult(u, true, "bash: frobnicate: command not found"); obs.ConsecutiveSameFamilyFailures != 1 {
		t.Fatalf("family change must restart streak at 1, got %d", obs.ConsecutiveSameFamilyFailures)
	}
}

// AC4: normalization is deterministic across runs — same input, same output,
// and the key folds in the tool name.
func TestErrorFamily_Deterministic(t *testing.T) {
	for _, f := range familyFixtures {
		first := normalizeErrorShape(f.msgA)
		for i := 0; i < 100; i++ {
			if got := normalizeErrorShape(f.msgA); got != first {
				t.Fatalf("%s: non-deterministic normalization: %q vs %q", f.name, got, first)
			}
		}
		if errorFamilyKey(f.tool, f.msgA) != errorFamilyKey(f.tool, f.msgA) {
			t.Fatalf("%s: non-deterministic family key", f.name)
		}
	}
	if errorFamilyKey("bash", familyFixtures[0].msgA) == errorFamilyKey("read", familyFixtures[0].msgA) {
		t.Fatalf("family key must include the tool name")
	}
}
