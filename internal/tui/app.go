package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rivo/uniseg"

	"github.com/elhenro/bee/internal/commands"
)

// side returns a fresh commands.Side bound to the current model. We rebuild
// per call because bubbletea passes Model by value — caching a *Model
// pointer would observe a stale copy after the next Update.
func (m *Model) side() commands.Side { return &tuiSide{m: m} }

// baseCtx is the model's context, never nil (tests run without one).
func (m Model) baseCtx() context.Context {
	if m.ctx == nil {
		return context.Background()
	}
	return m.ctx
}

// syncStream pushes the display toggles onto the stream renderer.
func (m *Model) syncStream() {
	s := m.stream
	s.SetVerbose(m.verbose)
	s.SetShowThoughts(m.showThoughts)
	s.SetShowNudges(m.showNudges)
	s.SetCompact(m.compact)
	s.SetHighlight(m.highlight)
	s.SetShowCallDetail(m.showCallDetail)
	s.SetLoader(m.loader)
	s.SetShowLoader(m.showLoader)
}

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
	inner := max(m.input.Width(), 1)
	n := 0
	for line := range strings.SplitSeq(m.input.Value(), "\n") {
		n += max(1, (uniseg.StringWidth(line)+inner-1)/inner)
	}
	n = min(n, inputHeightCap)
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
	if c := m.waitThink(); c != nil {
		cmds = append(cmds, c)
	}
	if c := m.waitProgress(); c != nil {
		cmds = append(cmds, c)
	}
	if c := m.waitLiveMsg(); c != nil {
		cmds = append(cmds, c)
	}
	if c := m.waitToolTail(); c != nil {
		cmds = append(cmds, c)
	}
	if c := m.waitWarn(); c != nil {
		cmds = append(cmds, c)
	}
	if c := m.waitJobs(); c != nil {
		cmds = append(cmds, c)
	}
	// Init has a value receiver, so flushing here would advance printedCount
	// on a throwaway copy and the first real flush would print the resumed
	// transcript a second time. Do it in Update instead.
	resumed := m.printedCount < len(m.messages)
	if resumed {
		cmds = append(cmds, func() tea.Msg { return initFlushMsg{} })
	}
	if m.introActive && !resumed {
		cmds = append(cmds, introTickCmd())
	}
	if m.role == "queen" {
		cmds = append(cmds, m.glowTick())
	}
	// one-shot auto-submit: fire the seeded prompt as if the user typed it.
	// banner is non-blocking so submitting now is safe.
	if m.seedPrompt != "" {
		seed := m.seedPrompt
		cmds = append(cmds, func() tea.Msg { return autoSubmitMsg{text: seed} })
	}
	return tea.Batch(cmds...)
}

// initFlushMsg asks Update to flush resumed-session messages into scrollback.
type initFlushMsg struct{}

// autoSubmitMsg carries a seeded prompt from Init into Update so it runs
// through the normal submit path exactly once.
type autoSubmitMsg struct{ text string }
