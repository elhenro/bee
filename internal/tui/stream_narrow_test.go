package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/types"
)

const narrowMarkdown = "## A heading that is long enough to wrap\n\n" +
	"- a bullet with a fairly long line of prose in it\n- `inline code` and **bold** words\n\n" +
	"1. a numbered item that also runs long\n\n" +
	"```go\nfunc veryLongFunctionName(argumentOne, argumentTwo string) error {\n\treturn nil\n}\n```\n\n" +
	"| name | value |\n|---|---|\n| alpha | a long cell value that needs wrapping |\n| beta | short |\n\n" +
	"> quoted text that goes on for a good while\n\n" +
	"See https://example.com/a/very/long/url/that/cannot/break/anywhere/at/all for details."

func narrowTranscript() []types.Message {
	patch := "--- a/s.go\n+++ b/s.go\n@@ -1,3 +1,4 @@\n a\n+inserted line with some length to it\n b\n c\n"
	text := func(role types.Role, s string) types.Message {
		return types.Message{Role: role, Content: []types.ContentBlock{{Type: types.BlockText, Text: s}}}
	}
	return []types.Message{
		text(types.RoleUser, "please look at the server handler and add sensible timeouts everywhere"),
		{Role: types.RoleAssistant, Content: []types.ContentBlock{{Type: types.BlockThinking, Text: "thinking about the handler and which timeouts it still lacks"}}},
		text(types.RoleAssistant, narrowMarkdown),
		text(types.RoleAssistant, "a plain reply with no markdown at all that still has to wrap to the terminal"),
		useMsg("r1", "read", map[string]any{"path": "internal/server/handler_with_a_long_name.go"}),
		resultMsg("r1", numbered(20, "code"), false),
		useMsg("b1", "bash", map[string]any{"command": "gofmt -l . && go vet ./... && go build ./... && go test ./..."}),
		resultMsg("b1", "internal/server/handler.go\n"+strings.Repeat("a long output row ", 6)+"\nok", false),
		useMsg("e1", "edit", map[string]any{"path": "internal/server/handler.go",
			"old": "\t\tReadTimeout: 5 * time.Second,\n", "new": "\t\tReadTimeout:  5 * time.Second,\n\t\tWriteTimeout: 10 * time.Second,\n"}),
		resultMsg("e1", "edited internal/server/handler.go", false),
		useMsg("p1", "apply_patch", map[string]any{"patch": patch}),
		resultMsg("p1", "patched s.go", false),
		useMsg("f1", "read", map[string]any{"path": "missing.go"}),
		resultMsg("f1", "open missing.go: no such file or directory", true),
		{Role: types.RoleAssistant, Ephemeral: true, Content: []types.ContentBlock{{Type: types.BlockText, Text: "(/new done)"}}},
		{Role: types.RoleAssistant, Ephemeral: true, Content: []types.ContentBlock{{Type: types.BlockText, Text: "/settings  toggle verbosity and agent-thought visibility\n/stop      cancel the running turn (same as esc)"}}},
	}
}

// every row the transcript prints fits the terminal: a row wider than the
// terminal is soft-wrapped by it and tears the layout
func TestTranscript_FitsNarrowTerminals(t *testing.T) {
	for _, w := range []int{20, 24, 30, 40, 80} {
		for _, compact := range []bool{false, true} {
			r := NewStreamRenderer(DefaultStyles(), w)
			r.SetCompact(compact)
			for i, m := range narrowTranscript() {
				for _, ln := range strings.Split(stripANSI(r.RenderMessage(m)), "\n") {
					if got := lipgloss.Width(ln); got > w {
						t.Errorf("width %d compact=%v message %d: row is %d cells: %q", w, compact, i, got, ln)
					}
				}
			}
		}
	}
}

func TestSpinnerRow_ShedsFiguresWholeWhenNarrow(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 30)
	r.SetLoaderStats(LoaderStats{InTokens: 3000, OutTokens: 340, ShowIn: true, ShowOut: true})
	row := stripANSI(r.RenderSpinner(1, 12*1e9))
	if w := lipgloss.Width(row); w > 30 {
		t.Fatalf("row is %d cells: %q", w, row)
	}
	if strings.HasSuffix(strings.TrimSpace(row), glyphSep) || strings.Contains(row, glyphSep+" "+glyphMore) {
		t.Fatalf("figures must drop whole, not be cut: %q", row)
	}
	if !strings.Contains(row, "12s · ↑ 3.0k") || strings.Contains(row, "↓") {
		t.Fatalf("keep the clock and the figures that fit, shed the rest: %q", row)
	}
	wide := NewStreamRenderer(DefaultStyles(), 100)
	wide.SetLoaderStats(LoaderStats{InTokens: 3000, OutTokens: 340, ShowIn: true, ShowOut: true})
	full := stripANSI(wide.RenderSpinner(1, 12*1e9))
	for _, want := range []string{"12s", "↑ 3.0k", "↓ 340", interruptHint} {
		if !strings.Contains(full, want) {
			t.Fatalf("wide row lost %q: %q", want, full)
		}
	}
}

func TestStatusLine_NarrowKeepsModelAndDropsChips(t *testing.T) {
	m := statusTestModel()
	m.model = "anthropic/claude-sonnet-4.5"
	m.thinking = "high"
	m.width = 30
	out := stripANSI(m.renderTopBar())
	if w := lipgloss.Width(out); w > 30 {
		t.Fatalf("footer is %d cells: %q", w, out)
	}
	if !strings.Contains(out, "claude-sonnet-4.5") || strings.Contains(out, glyphMore) {
		t.Fatalf("model name should survive whole, got %q", out)
	}
	if strings.Contains(out, "t:high") {
		t.Fatalf("effort chip should shed before the model truncates: %q", out)
	}
	m.yolo = true
	if out := stripANSI(m.renderTopBar()); !strings.Contains(out, "YOLO") {
		t.Fatalf("the safety badge never sheds: %q", out)
	}
	m.width = 120
	m.yolo = false
	if wide := stripANSI(m.renderTopBar()); !strings.Contains(wide, "anthropic/claude-sonnet-4.5") || !strings.Contains(wide, "t:high") {
		t.Fatalf("a wide footer keeps the full model and chips: %q", wide)
	}
}

func TestErrorBlock_WrapsToNarrowTerminal(t *testing.T) {
	m := statusTestModel()
	m.width = 24
	out := stripANSI(m.renderErrorBlock("provider returned an error that runs on for quite a while"))
	for _, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w > 24 {
			t.Fatalf("row is %d cells: %q", w, ln)
		}
	}
	if !strings.Contains(fmt.Sprint(out), "quite a while") {
		t.Fatalf("nothing is clipped from the message: %q", out)
	}
}
