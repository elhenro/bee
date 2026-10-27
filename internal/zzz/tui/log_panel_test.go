package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/elhenro/bee/internal/zzz"
)

// engine output lands in the log as raw lines; a long paragraph or stray
// control bytes must not break the alt-screen layout.
func TestLogPanel_ClipsAndSanitizesEngineLines(t *testing.T) {
	m := New(&zzz.Run{ID: "r1", Branch: "zzz/x", Status: "running"}, zzz.Config{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	long := strings.Repeat("a long paragraph in a single line, no newline ", 12)
	for _, txt := range []string{
		long,
		"progress 10%\r\x1b[2Kprogress 100%\tdone\x07",
		"日本語のテキスト" + long,
	} {
		m.Update(logMsg{level: "info", text: txt})
	}
	out := m.View()
	rows := 0
	for ln := range strings.SplitSeq(m.logPanel(78), "\n") {
		rows++
		if w := lipgloss.Width(ln); w > 78 {
			t.Fatalf("log row is %d cells: %q", w, ansi.Strip(ln))
		}
	}
	if rows != 4 { // header + 3 entries
		t.Fatalf("log panel has %d rows, want 4", rows)
	}
	for _, l := range m.log {
		if strings.ContainsAny(l.text, "\r\x1b\x07\t") {
			t.Fatalf("control bytes survived: %q", l.text)
		}
	}
	if !strings.Contains(ansi.Strip(out), "progress 10%progress 100% done") {
		t.Fatalf("sanitized line missing from log panel:\n%s", ansi.Strip(out))
	}
}
