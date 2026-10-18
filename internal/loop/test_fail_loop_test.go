package loop

import (
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func goTest(cmd string) types.ToolUse {
	return types.ToolUse{Name: "bash", Input: map[string]any{"command": cmd}}
}

const failOut = "--- FAIL: TestDance (0.00s)\n    a_test.go:203: dance must not reflow the row\nFAIL\nFAIL\tgithub.com/x/y\t0.397s\n"

// same failure through different -run flags and timings counts as one.
func TestObserveTestFail_SameFailureAcrossCommands(t *testing.T) {
	tr := newRepeatTracker()
	outs := []string{failOut, "--- FAIL: TestDance (0.01s)\n    a_test.go:203: dance must not reflow the row\nFAIL\nFAIL\tgithub.com/x/y\t0.364s\n", failOut}
	cmds := []string{"go test ./a -run X | tail -4", "go test ./a -run 'X|Y' -count=1 | tail", "go test ./a"}
	var n int
	for i := range outs {
		n = tr.observeTestFail(goTest(cmds[i]), outs[i])
	}
	if n != testFailNudgeAt {
		t.Fatalf("count = %d, want %d", n, testFailNudgeAt)
	}
}

func TestObserveTestFail_IgnoresNonTestsAndGreen(t *testing.T) {
	tr := newRepeatTracker()
	if tr.observeTestFail(goTest("ls"), failOut) != 0 {
		t.Fatal("non-test command counted")
	}
	if tr.observeTestFail(goTest("go test ./..."), "ok  \tgithub.com/x/y\t0.3s\n") != 0 {
		t.Fatal("green run counted")
	}
	if tr.observeTestFail(types.ToolUse{Name: "read"}, failOut) != 0 {
		t.Fatal("non-bash counted")
	}
}

func TestObserveTestFail_DistinctFailuresSeparate(t *testing.T) {
	tr := newRepeatTracker()
	tr.observeTestFail(goTest("go test ./a"), failOut)
	if n := tr.observeTestFail(goTest("go test ./a"), "--- FAIL: TestOther (0.00s)\n    b_test.go:9: nope\n"); n != 1 {
		t.Fatalf("distinct failure count = %d, want 1", n)
	}
}
