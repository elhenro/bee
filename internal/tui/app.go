package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rivo/uniseg"

	"github.com/elhenro/bee/internal/commands"
)

// side returns a fresh commands.Side bound to the current model. We rebuild
// per call because bubbletea passes Model by value — caching a *Model
// pointer would observe a stale copy after the next Update.
func (m *Model) side() commands.Side { return &tuiSide{m: m} }

// inputHeightCap is how tall the textarea can grow before further newlines
// just scroll inside it — keeps the chrome from devouring the transcript.
const inputHeightCap = 6

// inputGrowForMutation bumps the textarea height to inputHeightCap before
// a keystroke or SetValue can soft-wrap the content. Without this, content
// that wraps past the current height makes the textarea's internal
// viewport scroll YOffset down to keep the cursor visible — and once
// YOffset > 0, no path in the textarea API resets it back. Pre-growing
// gives repositionView room and keeps YOffset at 0; syncInputHeight at the
// end of Update then shrinks back to the actual row count for layout.
//
// textarea.Model holds its viewport as a *viewport.Model pointer, so a
// SetHeight inside View's value-copy mutates the same viewport as the
// persistent Model — but the textarea's own height value field stays at
// whatever Update last assigned. That desync is the root cause of the bug:
// a guard like `if Height() < cap` reads the stale value field, returns
// false, and the viewport.Height never grows for the next keystroke. We
// force SetHeight unconditionally so both fields land in lockstep.
func (m *Model) inputGrowForMutation() {
	m.input.SetHeight(inputHeightCap)
}

// syncInputHeight matches the textarea's render height to its content,
// clamped to [1, inputHeightCap]. Call after any value/cursor mutation.
//
// textarea.LineCount() counts logical lines (split on \n) — it does NOT
// account for soft-wrapped rows when a single long line exceeds the inner
// width. Without counting wrapped rows, typing past one visible row makes
// the viewport scroll inside a 1-row window and earlier text vanishes.
// Compute the visual row count by ceil(width / innerWidth) per logical line.
func (m *Model) syncInputHeight() {
	inner := m.input.Width()
	if inner < 1 {
		inner = 1
	}
	n := 0
	for _, line := range strings.Split(m.input.Value(), "\n") {
		w := uniseg.StringWidth(line)
		rows := 1
		if w > inner {
			rows = (w + inner - 1) / inner
		}
		n += rows
	}
	if n < 1 {
		n = 1
	}
	if n > inputHeightCap {
		n = inputHeightCap
	}
	if m.input.Height() != n {
		m.input.SetHeight(n)
	}
}

// Init satisfies tea.Model. Returns the blink cmd so the cursor pulses,
// plus the stream pump when a delta channel is wired and a flush of any
// resumed-session messages so they land in native scrollback at startup.
func (m Model) Init() tea.Cmd {
	// hide terminal cursor so the rendered textinput cursor is the only one
	// visible. textinput.Blink is deliberately omitted so the cursor is static.
	cmds := []tea.Cmd{tea.HideCursor}
	if c := m.waitStream(); c != nil {
		cmds = append(cmds, c)
	}
	if c := m.waitLiveMsg(); c != nil {
		cmds = append(cmds, c)
	}
	if c := m.waitWarn(); c != nil {
		cmds = append(cmds, c)
	}
	if c := m.flush(); c != nil {
		cmds = append(cmds, c)
	}
	if m.introActive {
		cmds = append(cmds, introTickCmd())
	}
	return tea.Batch(cmds...)
}
