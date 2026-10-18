package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/types"
)

func labeledBash() map[string]any {
	return map[string]any{"command": "go test ./internal/game/... -run Snake -count=1", "description": "running the snake tests"}
}

func renderCall(r *StreamRenderer, in map[string]any, out string, isErr bool) string {
	r.RenderMessage(useMsg("u1", "bash", in))
	return stripANSI(r.RenderMessage(resultMsg("u1", out, isErr)))
}

func TestCallLabel_ShowsLabelNotCommandByDefault(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	got := renderCall(r, labeledBash(), "ok", false)
	if !strings.Contains(got, "Bash  running the snake tests") {
		t.Fatalf("head should carry the label: %q", got)
	}
	if strings.Contains(got, "-run Snake") {
		t.Fatalf("command should stay hidden until the setting is on: %q", got)
	}
}

func TestCallLabel_DetailSettingShowsWholeCommand(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	r.SetShowCallDetail(true)
	got := renderCall(r, labeledBash(), "ok", false)
	if !strings.Contains(got, glyphDetail+"  go test ./internal/game/... -run Snake -count=1") {
		t.Fatalf("detail row missing: %q", got)
	}
	if strings.Index(got, "running the snake tests") > strings.Index(got, "-run Snake") {
		t.Fatalf("detail must sit under the label: %q", got)
	}
}

func TestCallLabel_FailedCallRevealsCommand(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	got := renderCall(r, labeledBash(), "exit 1", true)
	if !strings.Contains(got, "-run Snake") {
		t.Fatalf("a failed call has to say what failed: %q", got)
	}
}

func TestCallLabel_UnlabeledCallUnchanged(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	got := renderCall(r, map[string]any{"command": "make build"}, "ok", false)
	if !strings.Contains(got, "Bash  make build") {
		t.Fatalf("no label should keep the command inline: %q", got)
	}
}

func TestCallLabel_LongCommandWrapsAndFolds(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 40)
	r.SetShowCallDetail(true)
	var cmd strings.Builder
	for i := 0; i < 30; i++ {
		cmd.WriteString("echo line number " + strings.Repeat("x", i) + "\n")
	}
	got := renderCall(r, map[string]any{"command": cmd.String(), "description": "echoing lots"}, "ok", false)
	if !strings.Contains(got, "lines") || !strings.Contains(got, "+") {
		t.Fatalf("long detail should fold with a count: %q", got)
	}
	for _, ln := range strings.Split(got, "\n") {
		if w := lipgloss.Width(ln); w > 40 {
			t.Fatalf("row %d cols exceeds 40: %q", w, ln)
		}
	}
}

func TestCallLabel_PendingRowUsesLabel(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	r.SetShowCallDetail(true)
	use := useMsg("u1", "bash", labeledBash()).Content[0].Use
	got := stripANSI(r.RenderPendingTools([]types.ToolUse{*use}, 0))
	if !strings.Contains(got, "running the snake tests") || strings.Contains(got, "-run Snake") {
		t.Fatalf("in-flight row should show just the label: %q", got)
	}
}

// a label is model output: escape sequences must not reach the terminal
func TestCallLabel_StripsTerminalEscapes(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	r.SetShowCallDetail(true)
	in := map[string]any{
		"command":     "echo hi\x1b]0;pwned\x07\x1b[2J\u009b31m",
		"description": "running\x1b[2J\x1b]52;c;aGk=\x07 tests\r\u009b",
	}
	r.RenderMessage(useMsg("u1", "bash", in))
	raw := r.RenderMessage(resultMsg("u1", "ok", false))
	for _, bad := range []string{"\x1b]0;", "\x1b]52", "\x1b[2J", "\x07", "\r", "\u009b"} {
		if strings.Contains(raw, bad) {
			t.Fatalf("control sequence %q reached the terminal: %q", bad, raw)
		}
	}
	if !strings.Contains(stripANSI(raw), "running") || !strings.Contains(stripANSI(raw), "tests") {
		t.Fatalf("label text should survive the clean: %q", stripANSI(raw))
	}
}
