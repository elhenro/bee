package tui

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

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
			lines[i] = left + truncateVisible(line, inner) + right
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
	// panes below replace the live region while open and hug their content.
	if pane, ok := m.openPane(); ok {
		return m.padFrameLR(pane)
	}
	return m.padFrameLR(frame)
}

// the view of the first open full pane, if any. centered modal panes share
// Open()+View(w,h); a nil *Pane stored in the interface stays safe, every
// Open() is nil-receiver guarded. add a pane by appending it to the slice, not
// a new branch.
func (m Model) openPane() (string, bool) {
	switch {
	case m.tree != nil && m.tree.Open():
		return m.tree.View(m.width, m.height), true
	case m.resume != nil && m.resume.Open():
		return m.resume.View(m.width, m.height), true
	case m.rewind != nil && m.rewind.Open():
		return m.rewind.View(m.width, m.height), true
	}
	for _, p := range []centerPane{m.costPane, m.usagePane, m.loginPane, m.rolePane, m.effortPane, m.settingsPane, m.toolsPane} {
		if p.Open() {
			return p.View(m.width, m.height), true
		}
	}
	switch {
	case m.agentView != nil && m.agentView.IsOpen():
		return m.agentView.Render(m.width, m.height), true
	case m.hive != nil && m.hive.Expanded():
		return m.hive.RenderFull(m.width, m.height), true
	}
	return "", false
}

// renderQueuedMidCompact paints a plain-text submit that landed while
// /compact was running, as the user turn it will become: same bar and panel
// as a sent one, tagged so it reads as held rather than delivered.
func (m Model) renderQueuedMidCompact() string {
	return m.stream.gutter(m.stream.renderUser("queued: " + m.queuedMidCompact))
}

// renderErrorBlock formats m.lastErr for the live region as a block like any
// other: marker column, message hanging under it, blank row above. strips
// noisy provider JSON tails and wraps on word boundaries; nothing about the
// message is truncated, long errors wrap to more rows instead of being
// clipped off-screen.
func (m Model) renderErrorBlock(s string) string {
	lines := []string{cleanErrorMessage(s)}
	if m.width > 0 {
		lines = wrapHanging(lines[0], max(m.width-markIndent-2, minTextWidth))
	}
	for i := range lines {
		if i == 0 {
			lines[i] = glyphFail + " " + lines[i]
		} else {
			lines[i] = strings.Repeat(" ", markIndent) + lines[i]
		}
	}
	out := m.stream.gutter(m.styles.Error.Render(strings.Join(lines, "\n")))
	if m.stream.compact {
		return out
	}
	return "\n" + out
}

// the `post: Post "url": ` net/http wraps around a transport failure; the
// cause after it is what matters
var httpVerbNoise = regexp.MustCompile(`(?i)\b(post|get): (post|get) "[^"]*": `)

// cleanErrorMessage trims trailing provider JSON envelopes off raw error
// strings so the user sees the human-readable message instead of a wall of
// `{"error":{"message":"..."}}`. Parses the JSON tail when present and
// folds `.error.message` (or top-level `.message`) back into the prefix.
// Falls back to the prefix-only form when the JSON is truncated/unparsable,
// and to the original string when no JSON envelope is found.
func cleanErrorMessage(s string) string {
	s = httpVerbNoise.ReplaceAllString(s, "")
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
	if n <= 0 || lipgloss.Width(s) <= n {
		return []string{s}
	}
	var out []string
	for lipgloss.Width(s) > n {
		// widest prefix that fits n cells, remembering the last space inside
		w, cut, lastSpace := 0, 0, -1
		for i, r := range s {
			rw := lipgloss.Width(string(r))
			if w+rw > n {
				break
			}
			if r == ' ' {
				lastSpace = i
			}
			w += rw
			cut = i + utf8.RuneLen(r)
		}
		switch {
		case cut < len(s) && s[cut] == ' ':
			// window ends right at a word boundary
		case lastSpace > 0:
			cut = lastSpace
		case cut == 0:
			_, cut = utf8.DecodeRuneInString(s) // rune wider than n: take it whole
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
