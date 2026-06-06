package tui

import (
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
	for _, want := range []string{"bash", "go version", "read", "go.mod"} {
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
