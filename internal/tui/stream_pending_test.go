package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func TestRenderPendingTools_OneRowPerCall(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	uses := []types.ToolUse{
		{ID: "u1", Name: "bash", Input: map[string]any{"command": "go version"}},
		{ID: "u2", Name: "read", Input: map[string]any{"path": "go.mod"}},
	}
	out := stripANSI(r.RenderPendingTools(uses, 3))
	for _, want := range []string{"Bash", "go version", "Read", "go.mod"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in pending render: %q", want, out)
		}
	}
	if got := strings.Count(strings.TrimSpace(out), "\n"); got != 1 {
		t.Fatalf("expected 2 rows (one per call), got %d: %q", got+1, out)
	}
}

func TestRenderPendingTools_SkipsEscalateAndEmpty(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	if out := r.RenderPendingTools(nil, 0); out != "" {
		t.Fatalf("nil uses should render empty, got %q", out)
	}
	out := r.RenderPendingTools([]types.ToolUse{{ID: "e", Name: "escalate"}}, 0)
	if out != "" {
		t.Fatalf("escalate-only should render empty, got %q", out)
	}
}

func TestRenderPendingTools_CapsLargeBatch(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	uses := make([]types.ToolUse, 20)
	for i := range uses {
		uses[i] = types.ToolUse{Name: "read", Input: map[string]any{"path": "f"}}
	}
	out := stripANSI(r.RenderPendingTools(uses, 1))
	if !strings.Contains(out, "more running") {
		t.Fatalf("expected overflow marker for large batch: %q", out)
	}
	if rows := strings.Count(strings.TrimSpace(out), "\n") + 1; rows > 8 {
		t.Fatalf("expected <=8 rows, got %d", rows)
	}
}

func TestRenderPendingTools_HangsTailUnderRunningCall(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	uses := []types.ToolUse{
		{ID: "u1", Name: "bash", Input: map[string]any{"command": "make"}},
		{ID: "u2", Name: "bash", Input: map[string]any{"command": "make test"}},
	}
	r.SetLiveTail("u1", "one\n\x1b[31mtwo\x1b[0m\nthree\nfour\nfive")
	rows := strings.Split(strings.TrimRight(stripANSI(r.RenderPendingTools(uses, 0)), "\n"), "\n")
	if len(rows) != 6 {
		t.Fatalf("want head + 4 tail rows + next call, got %d: %q", len(rows), rows)
	}
	if !strings.Contains(rows[1], glyphResult) || !strings.Contains(rows[1], "two") || strings.Contains(rows[1], "\x1b") {
		t.Fatalf("first tail row should open on the connector, ansi stripped: %q", rows[1])
	}
	if strings.Contains(rows[2], glyphResult) || !strings.Contains(rows[2], "three") {
		t.Fatalf("later tail rows hang under the first: %q", rows[2])
	}
	if !strings.Contains(rows[5], "make test") {
		t.Fatalf("second call stays below the tail: %q", rows[5])
	}
}

func TestRenderPendingTools_TailForFinishedCallIsIgnored(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	r.SetLiveTail("gone", "stale output")
	out := stripANSI(r.RenderPendingTools([]types.ToolUse{{ID: "u1", Name: "bash", Input: map[string]any{"command": "ls"}}}, 0))
	if strings.Contains(out, "stale") {
		t.Fatalf("tail of another call leaked: %q", out)
	}
}

func TestRenderPendingTools_TailFitsNarrowTerminal(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), minRenderWidth)
	r.SetLiveTail("u1", strings.Repeat("x", 200))
	out := r.RenderPendingTools([]types.ToolUse{{ID: "u1", Name: "bash", Input: map[string]any{"command": "ls"}}}, 0)
	for _, ln := range strings.Split(out, "\n") {
		if w := lipglossWidth(ln); w > minRenderWidth {
			t.Fatalf("row wider than terminal (%d): %q", w, ln)
		}
	}
}

func TestRenderPendingTools_EditHeadIsBuiltOnce(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	u := types.ToolUse{ID: "e1", Name: "edit", Input: map[string]any{"path": "a.go", "old": "x := 1\n", "new": "x := 2\ny := 3\n"}}
	first := stripANSI(r.RenderPendingTools([]types.ToolUse{u}, 0))
	for _, want := range []string{"Update", "a.go", "+2", "−1"} {
		if !strings.Contains(first, want) {
			t.Fatalf("missing %q in pending edit head: %q", want, first)
		}
	}
	if strings.Contains(first, "x := 2") {
		t.Fatalf("a running call shows its header, not the diff: %q", first)
	}
	// a planted head comes back verbatim: the diff is not rebuilt per frame
	r.live.editHeads["e1"] = "PLANTED"
	if got := stripANSI(r.RenderPendingTools([]types.ToolUse{u}, 1)); !strings.Contains(got, "PLANTED") {
		t.Fatalf("second frame rebuilt the head: %q", got)
	}
	// and it matches the head the finished card opens with
	r = NewStreamRenderer(DefaultStyles(), 100)
	ph, _ := r.callParts(u, outcomePending, 0)
	dh, _ := r.callParts(u, outcomeDone, 0)
	if stripANSI(ph) != stripANSI(dh) {
		t.Fatalf("pending head %q differs from settled head %q", stripANSI(ph), stripANSI(dh))
	}
}

func TestPendingEditHead_MemoStaysBounded(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	for i := 0; i < 3*maxEditHeads; i++ {
		u := types.ToolUse{ID: fmt.Sprintf("w%d", i), Name: "write", Input: map[string]any{"path": "a.txt", "content": "hi\n"}}
		if _, ok := r.pendingEditHead(u); !ok {
			t.Fatal("write is a file mutation")
		}
	}
	if n := len(r.live.editHeads); n > maxEditHeads {
		t.Fatalf("memo grew to %d entries", n)
	}
	if _, ok := r.pendingEditHead(types.ToolUse{ID: "r", Name: "read"}); ok {
		t.Fatal("only file mutations carry a diff header")
	}
}
