package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/types"
)

func narrowModel(t *testing.T, w int) Model {
	t.Helper()
	m := NewModel(nil, "/tmp/proj/with/a/fairly/deep/path", "anthropic/claude-sonnet-4.5", "workspace-write", caveman.Full)
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 30})
	return next.(Model)
}

// every row of the live frame fits the terminal; the inline renderer wraps
// nothing for us, so a wider row tears the redraw
func assertFits(t *testing.T, name string, w int, view string) {
	t.Helper()
	for _, ln := range strings.Split(stripANSI(view), "\n") {
		if got := lipgloss.Width(ln); got > w {
			t.Errorf("%s at width %d: row is %d cells: %q", name, w, got, ln)
		}
	}
}

func TestView_FitsNarrowTerminals(t *testing.T) {
	for _, w := range []int{minRenderWidth, 24, 30, 40} {
		m := narrowModel(t, w)
		assertFits(t, "idle", w, m.View())

		typed := m
		typed.input.SetValue("a fairly long prompt that has to wrap across several rows of the box")
		assertFits(t, "typed", w, typed.View())

		busy := m
		busy.state = StateStreaming
		busy.turnStartedAt = time.Now().Add(-75 * time.Second)
		busy.showLoaderIn, busy.showLoaderOut, busy.showLoaderRate = true, true, true
		busy.partial = "Checking the handler first, then I will wire up the write timeout.\n\nsecond paragraph"
		busy.pendingTools = []types.ToolUse{{ID: "u1", Name: "bash", Input: map[string]any{"command": "go build ./... && go test ./... && golangci-lint run"}}}
		assertFits(t, "streaming", w, busy.View())

		errored := m
		errored.lastErr = "provider returned an error that runs on for quite a while"
		errored.state = StateError
		assertFits(t, "error", w, errored.View())

		slash := m
		slash.input.SetValue("/")
		next, _ := slash.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
		assertFits(t, "slash picker", w, next.(Model).View())

		ask := m
		ask.approval.Show(ApprovalRequest{ToolName: "bash", Action: "rm -rf /tmp/some/long/path/here", Reason: "deletes files outside the project"})
		ask.approval.SetWidth(w)
		assertFits(t, "approval", w, ask.View())
	}
}

// a terminal narrower than anything we lay out for still draws without
// panicking (a negative repeat count is the usual culprit)
func TestView_AnyWidthDoesNotPanic(t *testing.T) {
	for w := 1; w <= 60; w++ {
		m := narrowModel(t, w)
		m.View()
		m.input.SetValue("hello there")
		m.View()
		m.state = StateStreaming
		m.turnStartedAt = time.Now().Add(-3 * time.Second)
		m.partial = "streaming words that are long enough to wrap"
		m.pendingTools = []types.ToolUse{{ID: "u1", Name: "read", Input: map[string]any{"path": "a/b/c.go"}}}
		m.View()
		m.state = StateError
		m.lastErr = "boom"
		m.View()
		r := NewStreamRenderer(DefaultStyles(), w)
		for _, msg := range narrowTranscript() {
			r.RenderMessage(msg)
		}
	}
}
