package tui

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// maxOuterPadLR caps the live TUI padding so a tiny terminal can't collapse
// to a sliver. Mirrored by the WithOuterPadLR setter.
const maxOuterPadLR = 8

// padFrameLR wraps the final rendered TUI string with N blank cells of
// left+right padding when m.outerPadLR > 0. Identity otherwise. Used as the
// outermost step of View() so every return path (bare frame + every modal
// overlay) gets the same gutter without touching the inner renderers.
//
// The inner content is rendered against the full terminal width (top bar
// right-align, context bar, stream wrap all still read m.width). Without
// clipping, the rightmost pad cells would push past the terminal edge. We
// pre-clip each line's visible width (using lipgloss.Width, which is
// ANSI-aware) to m.width-2*pad before adding the pad, so the wrap stays
// inside the terminal. Lines whose visible width already fits keep their
// existing alignment — only their left/right pad changes.
//
// Finalized messages flushed via tea.Println are NOT padded (this only
// affects the View() return).
func (m Model) padFrameLR(s string) string {
	pad := m.outerPadLR
	if pad <= 0 || m.width <= 0 {
		return s
	}
	if pad > maxOuterPadLR {
		pad = maxOuterPadLR
	}
	inner := m.width - 2*pad
	if inner < 1 {
		// terminal narrower than 2*pad+1 — the padding would consume the
		// whole row. Render as-is to keep at least one cell of content.
		return s
	}
	left := strings.Repeat(" ", pad)
	right := strings.Repeat(" ", pad)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		// ANSI-aware trim. lipgloss.Width counts runes + escape codes
		// correctly; we just chop visible runes off the right tail.
		w := lipgloss.Width(line)
		if w > inner {
			lines[i] = truncateVisible(line, inner) + right
		} else {
			lines[i] = left + line + right
		}
	}
	return strings.Join(lines, "\n")
}

// centerPane is the shape every Ctrl-key modal pane shares: a nil-safe Open
// gate and a sized View. View() consumes a slice of these so adding a pane is
// one slice entry, not another if-branch.
type centerPane interface {
	Open() bool
	View(width, height int) string
}

// View renders the live region only — top bar, streaming partial (if any),
// error line (if any), input row, and key hints. Finalized messages live
// in the terminal's native scrollback via tea.Println; we never repaint
// past turns. The live region grows while streaming, shrinks when idle.
func (m Model) View() string {
	if m.width == 0 {
		return "" // pre-size: don't draw
	}
	// height sync runs at the end of Update via defer; View stays pure.
	status := m.renderTopBar()
	bot := m.renderBottomBar()
	// pre-render all non-mid parts so we can budget remaining rows for the
	// streaming partial. bubbletea inline cannot reach above the cursor, so
	// a partial taller than the live region gets its head clipped out of
	// sight — clip head-side ourselves and surface a `… +N above` header.
	intro := m.renderIntro()
	warn := m.renderWarning()
	midBudget := liveBudget(m.height, intro, bot, status, warn)
	mid := m.renderLive(midBudget)
	var parts []string
	if intro != "" {
		parts = append(parts, intro)
	}
	if mid != "" {
		parts = append(parts, mid)
	}
	if warn != "" {
		parts = append(parts, warn)
	}
	parts = append(parts, bot, status)
	// a tab jumps the cursor without erasing the cells it passes, so a
	// redrawn row would keep stale characters under it. Spaces never do.
	frame := expandTabs(strings.Join(parts, "\n"))
	if m.approval.Active {
		return m.padFrameLR(overlayCenter(frame, m.approval.View(), m.width))
	}
	if m.askModel.Active {
		return m.padFrameLR(overlayCenter(frame, m.askModel.View(), m.width))
	}
	if m.updatePrompt.Active {
		return m.padFrameLR(overlayCenter(frame, m.updatePrompt.View(), m.width))
	}
	if m.tutorial.active {
		return m.padFrameLR(overlayCenter(frame, m.renderTutorial(), m.width))
	}
	// palette, atpicker, picker, history all render inline above the input in
	// renderBottomBar — no extra overlay needed for any picker-style flow.
	if m.tree != nil && m.tree.Open() {
		return m.padFrameLR(overlayCenter(frame, m.tree.View(m.width, m.height), m.width))
	}
	if m.resume != nil && m.resume.Open() {
		return m.padFrameLR(overlayCenter(frame, m.resume.View(m.width, m.height), m.width))
	}
	if m.rewind != nil && m.rewind.Open() {
		return m.padFrameLR(overlayCenter(frame, m.rewind.View(m.width, m.height), m.width))
	}
	// centered modal panes share Open()+View(w,h); render the first open one.
	// a nil *Pane stored in the interface stays safe — every Open() is
	// nil-receiver guarded. add a pane by appending it here, not a new branch.
	for _, p := range []centerPane{m.costPane, m.usagePane, m.loginPane, m.rolePane, m.effortPane, m.settingsPane, m.toolsPane} {
		if p.Open() {
			return m.padFrameLR(overlayCenter(frame, p.View(m.width, m.height), m.width))
		}
	}
	if m.agentView != nil && m.agentView.IsOpen() {
		return m.padFrameLR(overlayCenter(frame, m.agentView.Render(m.width, m.height), m.width))
	}
	if m.hive != nil && m.hive.Expanded() {
		return m.padFrameLR(overlayCenter(frame, m.hive.RenderFull(m.width, m.height), m.width))
	}
	// picker renders inline above the input bar (see renderBottomBar) — same
	// flush-left dense layout as the slash palette.
	return m.padFrameLR(frame)
}

// renderQueuedMidCompact paints a plain-text submit that landed while
// /compact was running. Styled like a sent user message — blue rail on
// the left, bold blue body — so the queued text reads as the user's own
// held turn rather than a meta echo. Multi-line input gets the rail
// applied to every continuation line so the block stays anchored to the
// left margin the way finalized user turns do.
func (m Model) renderQueuedMidCompact() string {
	rail := m.styles.UserRail.Render("┃")
	body := m.styles.UserBody.Render
	lines := strings.Split("queued: "+m.queuedMidCompact, "\n")
	for i, ln := range lines {
		lines[i] = rail + " " + body(ln)
	}
	return strings.Join(lines, "\n")
}

// renderErrorBlock formats m.lastErr for the live region: strips noisy
// provider JSON tails, wraps on word boundaries to terminal width with a
// hanging indent so continuation lines align under the body, and tints the
// whole block via the Error style. Nothing about the message is truncated
// — long errors wrap to multiple rows instead of being clipped off-screen.
func (m Model) renderErrorBlock(s string) string {
	body := cleanErrorMessage(s)
	const glyph = "✗ "
	const indent = "  "
	w := m.width
	if w <= 0 {
		return m.styles.Error.Render(glyph + body)
	}
	bodyW := w - len(indent)
	if bodyW < 20 {
		bodyW = 20
	}
	lines := wrapHanging(body, bodyW)
	for i := range lines {
		if i == 0 {
			lines[i] = glyph + lines[i]
		} else {
			lines[i] = indent + lines[i]
		}
	}
	return m.styles.Error.Render(strings.Join(lines, "\n"))
}

// cleanErrorMessage trims trailing provider JSON envelopes off raw error
// strings so the user sees the human-readable message instead of a wall of
// `{"error":{"message":"..."}}`. Parses the JSON tail when present and
// folds `.error.message` (or top-level `.message`) back into the prefix.
// Falls back to the prefix-only form when the JSON is truncated/unparsable,
// and to the original string when no JSON envelope is found.
func cleanErrorMessage(s string) string {
	i := strings.IndexByte(s, '{')
	if i < 0 {
		return s
	}
	prefix := strings.TrimRight(s[:i], " :")
	tail := s[i:]
	var probe struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(tail), &probe); err != nil {
		return prefix
	}
	msg := probe.Error.Message
	if msg == "" {
		msg = probe.Message
	}
	if msg == "" {
		return prefix
	}
	return prefix + ": " + msg
}

// wrapHanging breaks s into lines no wider than n cells, splitting on spaces
// when possible and hard-cutting overlong tokens (URLs, opaque IDs) so a
// single token can't overflow the budget.
func wrapHanging(s string, n int) []string {
	if n <= 0 || len(s) <= n {
		return []string{s}
	}
	var out []string
	for len(s) > n {
		cut := n
		for cut > 0 && s[cut] != ' ' {
			cut--
		}
		if cut == 0 {
			cut = n // no space in window: hard-cut.
		}
		out = append(out, s[:cut])
		s = strings.TrimLeft(s[cut:], " ")
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}

// overlayCenter draws modal beneath base, centered to width. bubbletea has no
// true overlay primitive — for v0.1 we append; tests assert substrings only.
func overlayCenter(base, modal string, w int) string {
	if modal == "" {
		return base
	}
	return base + "\n\n" + lipgloss.NewStyle().Width(w).Align(lipgloss.Center).Render(modal)
}

// turnElapsed returns the elapsed time of the current turn. Returns zero if
// the turn hasn't started yet.
func (m Model) turnElapsed() time.Duration {
	if m.turnStartedAt.IsZero() {
		return 0
	}
	return time.Since(m.turnStartedAt)
}
