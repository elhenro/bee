package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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
	var ctxBar string
	if m.showContextBar {
		ctxBar = m.renderContextBar()
	}
	midBudget := liveBudget(m.height, intro, bot, status, warn, ctxBar)
	mid := m.renderLive(midBudget)
	var parts []string
	if intro != "" {
		parts = append(parts, intro)
	}
	if mid != "" {
		parts = append(parts, mid)
	}
	parts = append(parts, bot, status)
	if warn != "" {
		parts = append(parts, warn)
	}
	if ctxBar != "" {
		parts = append(parts, ctxBar)
	}
	frame := strings.Join(parts, "\n")
	if m.approval.Active {
		return overlayCenter(frame, m.approval.View(), m.width)
	}
	if m.askModel.Active {
		return overlayCenter(frame, m.askModel.View(), m.width)
	}
	if m.updatePrompt.Active {
		return overlayCenter(frame, m.updatePrompt.View(), m.width)
	}
	// palette, atpicker, picker, history all render inline above the input in
	// renderBottomBar — no extra overlay needed for any picker-style flow.
	if m.tree != nil && m.tree.Open() {
		return overlayCenter(frame, m.tree.View(m.width, m.height), m.width)
	}
	if m.resume != nil && m.resume.Open() {
		return overlayCenter(frame, m.resume.View(m.width, m.height), m.width)
	}
	if m.costPane != nil && m.costPane.Open() {
		return overlayCenter(frame, m.costPane.View(m.width, m.height), m.width)
	}
	if m.loginPane != nil && m.loginPane.Open() {
		return overlayCenter(frame, m.loginPane.View(m.width, m.height), m.width)
	}
	if m.effortPane != nil && m.effortPane.Open() {
		return overlayCenter(frame, m.effortPane.View(m.width, m.height), m.width)
	}
	if m.settingsPane != nil && m.settingsPane.Open() {
		return overlayCenter(frame, m.settingsPane.View(m.width, m.height), m.width)
	}
	if m.toolsPane != nil && m.toolsPane.Open() {
		return overlayCenter(frame, m.toolsPane.View(m.width, m.height), m.width)
	}
	if m.agentView != nil && m.agentView.IsOpen() {
		return overlayCenter(frame, m.agentView.Render(m.width, m.height), m.width)
	}
	if m.hive != nil && m.hive.Expanded() {
		return overlayCenter(frame, m.hive.RenderFull(m.width, m.height), m.width)
	}
	// picker renders inline above the input bar (see renderBottomBar) — same
	// flush-left dense layout as the slash palette.
	return frame
}

// renderLive returns the live in-progress slice — streaming partial while
// a turn is in flight, or the latest error line. Past messages are NOT
// rendered here; they live in terminal scrollback. Empty string when idle
// with no error, so the live region collapses to just top + bottom bars.
// maxRows caps the streaming partial to the tail when budget > 0; bubbletea
// inline can't render above the cursor, so without clipping a long partial
// would hide its newest tokens off the bottom of the visible region.
func (m Model) renderLive(maxRows int) string {
	var parts []string
	if m.state == StateStreaming {
		// reasoning streams above the answer in a dim/italic block. Same
		// styling as the finalized BlockThinking block so the live view
		// matches the persisted scrollback. Empty while thoughts hidden
		// or no thinking deltas arrived yet.
		if think := m.thinkPartial; think != "" && m.showThoughts {
			parts = append(parts, m.stream.RenderThinkingPartial(think))
		}
		// progressive flush may have pushed a prefix of m.partial to scroll-
		// back already; only render what's still live.
		visible := m.partial
		if n := len(m.streamFlushed); n > 0 && n <= len(m.partial) {
			visible = m.partial[n:]
		}
		out := m.stream.RenderStreaming(visible, m.loaderFrame)
		if maxRows > 0 && visible != "" {
			out = m.stream.ClipStreamingTail(out, maxRows)
		}
		parts = append(parts, out)
	}
	if m.compacting {
		parts = append(parts, m.stream.RenderCompacting(m.loaderFrame))
	}
	if m.state == StateError && m.lastErr != "" {
		parts = append(parts, m.renderErrorBlock(m.lastErr))
	}
	return strings.Join(parts, "\n")
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

// renderBottomBar shows just the input by default. Hints surface only when
// the user presses `?` (m.showHelp). A staged-image indicator pops above the
// input line whenever the user has Ctrl+I'd an image but not yet submitted.
// Horizontal rules frame the input above and below at full terminal width.
func (m Model) renderBottomBar() string {
	var quitHint string
	if m.quitArmed {
		quitHint = lipgloss.NewStyle().Foreground(accentHoney).Render("press ctrl+d again to quit") + "\n"
	}
	var staged string
	if len(m.pendingImage) > 0 {
		staged = m.styles.Dim.Render("📎 image staged ("+bytesHuman(len(m.pendingImage))+") — submit to attach") + "\n"
	}
	var palette string
	if m.palette.Active {
		palette = m.palette.View() + "\n"
	}
	var picker string
	if m.picker != nil && m.picker.Active() {
		picker = m.picker.View() + "\n"
	}
	var atp string
	if m.atpicker.Active {
		atp = m.atpicker.View() + "\n"
	}
	var history string
	if m.history.Active {
		history = m.history.View() + "\n"
	}
	// shell-mode visual: when buffer starts with `!` the user is about to
	// dispatch a shell command, not a chat turn. Tint the typed text honey
	// and bold the prompt so the mode is unmistakable. `m` is a value copy
	// so the mutation is scoped to this render.
	val := m.input.Value()
	if strings.HasPrefix(val, "!") {
		honey := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
		// CursorLine wins over Text on whichever row the cursor sits, so we
		// must set both — otherwise the active line stays default-colored.
		m.input.FocusedStyle.Prompt = honey
		m.input.FocusedStyle.Text = honey
		m.input.FocusedStyle.CursorLine = honey
		m.input.BlurredStyle.Prompt = honey
		m.input.BlurredStyle.Text = honey
		m.input.BlurredStyle.CursorLine = honey
		// textarea caches an unexported *Style pointer at Focus()/Blur() time;
		// value-copying the Model leaves that pointer aimed at the *original*
		// FocusedStyle (in the outer Model), so the mutations above wouldn't
		// take effect on View(). Re-focusing re-points the cached style to
		// our local copy. Discard the returned cmd (cursor uses Static mode).
		_ = m.input.Focus()
	}
	if !m.showHelp {
		return quitHint + staged + palette + picker + atp + history + m.input.View()
	}
	hint := fmt.Sprintf("mode:%s · caveman:%s · think:%s · ^P model · ^R history · ^W ws · ← agents · ^H hive · ^/ caveman · ^I image · shift+↵/^J newline · shift+tab mode · alt+t think · ? hide · esc cancel · ^V verbose", m.mode, string(m.caveLvl), m.thinking)
	return quitHint + staged + palette + picker + atp + history + m.input.View() + "\n" + m.styles.BottomBar.Render(hint)
}

// overlayCenter draws modal beneath base, centered to width. bubbletea has no
// true overlay primitive — for v0.1 we append; tests assert substrings only.
func overlayCenter(base, modal string, w int) string {
	if modal == "" {
		return base
	}
	return base + "\n\n" + lipgloss.NewStyle().Width(w).Align(lipgloss.Center).Render(modal)
}
