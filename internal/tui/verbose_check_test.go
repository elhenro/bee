package tui

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func TestVerbose_ShowsAllLines(t *testing.T) {
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, "line-")
	}
	content := strings.Join(lines, "\n")
	r := NewStreamRenderer(DefaultStyles(), 120)
	r.SetVerbose(true)
	out := r.renderToolResult(types.ToolResult{Content: content})
	if strings.Contains(out, "+59 more") || strings.Contains(out, "more") {
		t.Errorf("verbose unexpectedly truncated: contains 'more'\n%s", out)
	}
	if c := strings.Count(out, "line-"); c < 60 {
		t.Errorf("expected 60 lines, got %d", c)
	}
}
