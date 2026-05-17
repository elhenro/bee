package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/llm"
)

// osUserHome is a tiny indirection so tests can override $HOME for cwd
// prettifying without touching the global env.
var osUserHome = func() (string, error) { return os.UserHomeDir() }

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
	mid := m.renderLive()
	var parts []string
	if intro := m.renderIntro(); intro != "" {
		parts = append(parts, intro)
	}
	if mid != "" {
		parts = append(parts, mid)
	}
	parts = append(parts, bot, status)
	if w := m.renderWarning(); w != "" {
		parts = append(parts, w)
	}
	if m.showContextBar {
		if bar := m.renderContextBar(); bar != "" {
			parts = append(parts, bar)
		}
	}
	frame := strings.Join(parts, "\n")
	if m.approval.Active {
		return overlayCenter(frame, m.approval.View(), m.width)
	}
	// palette and atpicker render inline above the input in renderBottomBar —
	// no extra overlay needed here.
	if m.history.Active {
		return overlayCenter(frame, m.history.View(), m.width)
	}
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

func (m Model) renderTopBar() string {
	// Slim, dim status line. Hex glyph doubles as a context-fill pie:
	// outline when empty, filled and color-tiered as input tokens grow
	// against the model's context window.
	hex := m.renderContextHex()
	model := m.styles.Dim.Render(m.displayModel())
	cwd := m.styles.Dim.Render(prettyCwd(m.cwd))
	left := hex + "  " + model + "  " + cwd
	if m.costs != nil && !m.isLocalProvider() {
		tot := m.costs.Total()
		// only render badge when there's actual spend — free local models
		// (ollama, lm-studio) report 0 USD and shouldn't show "$0.0000".
		if tot.USD > 0 {
			left += "  " + m.renderCostBadge(tot.USD)
		}
	}
	right := ""
	if m.mode == "auto" {
		right += m.renderModeBadge() + "  "
	}
	if m.thinking != "" && m.thinking != "off" {
		right += m.styles.Dim.Render("t:"+m.thinking) + " "
	}
	pad := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

// renderLive returns the live in-progress slice — streaming partial while
// a turn is in flight, or the latest error line. Past messages are NOT
// rendered here; they live in terminal scrollback. Empty string when idle
// with no error, so the live region collapses to just top + bottom bars.
func (m Model) renderLive() string {
	var parts []string
	if m.state == StateStreaming {
		parts = append(parts, m.stream.RenderStreaming(m.partial, m.loaderFrame))
	}
	if m.compacting {
		parts = append(parts, m.stream.RenderCompacting(m.loaderFrame))
	}
	if m.state == StateError && m.lastErr != "" {
		parts = append(parts, m.styles.Error.Render("✗ "+m.lastErr))
	}
	return strings.Join(parts, "\n")
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
		return quitHint + staged + palette + picker + atp + m.input.View()
	}
	hint := fmt.Sprintf("mode:%s · caveman:%s · think:%s · ^P model · ^R history · ^W ws · ← agents · ^H hive · ^/ caveman · ^I image · shift+↵/^J newline · shift+tab mode · alt+t think · ? hide · esc cancel · ^V verbose", m.mode, string(m.caveLvl), m.thinking)
	return quitHint + staged + palette + picker + atp + m.input.View() + "\n" + m.styles.BottomBar.Render(hint)
}

// contextPct returns the fraction of the active model's context window used
// by the most recent turn's input. 0 when no costs tracked, no events yet,
// or the model's window is unknown.
func (m Model) contextPct() float64 {
	if m.costs == nil {
		return 0
	}
	in := m.costs.LastInput()
	if in <= 0 {
		return 0
	}
	cap := llm.ContextWindow(m.model)
	if cap <= 0 {
		return 0
	}
	return float64(in) / float64(cap)
}

// renderContextHex draws the pie-style fill indicator. A 🐝 emoji with
// colour tier escalates with fill so a glance tells
// you "fresh" vs "almost full". Percent label appears once anything's used.
// you "fresh" vs "almost full". Percent label appears once anything's used.
func (m Model) renderContextHex() string {
	pct := m.contextPct()
	glyph := "🐝"
	if pct > 0 {
		glyph = "🐝"
	}
	var fg lipgloss.TerminalColor
	bold := false
	switch {
	case pct < 0.01:
		fg = fgSquid
	case pct < 0.50:
		fg = accentBee
	case pct < 0.80:
		fg = accentHoney
	case pct < 0.95:
		fg = accentBusy
		bold = true
	default:
		fg = semError
		bold = true
	}
	style := lipgloss.NewStyle().Foreground(fg).Bold(bold)
	out := style.Render(glyph)
	if pct > 0 {
		// rounded percent; cap display at 999% to avoid layout breaks if
		// LastInput somehow exceeds the window.
		p := int(pct*100 + 0.5)
		if p > 999 {
			p = 999
		}
		out += " " + style.Render(fmt.Sprintf("%d%%", p))
	}
	return out
}

// renderContextBar draws a thin full-width progress strip pinned to the
// terminal's bottom edge. Empty state is a quiet ─ rule in oyster; as the
// active turn's input tokens fill the model's context window, the leading
// portion thickens to ━ and steps through the same color tiers as the hex
// glyph (bee → honey → busy → error). Always rendered so the rule reads as
// elegant chrome, not a transient indicator.
func (m Model) renderContextBar() string {
	if m.width <= 0 {
		return ""
	}
	pct := m.contextPct()
	if pct > 1 {
		pct = 1
	}
	fill := int(pct*float64(m.width) + 0.5)
	if fill < 0 {
		fill = 0
	}
	if fill > m.width {
		fill = m.width
	}
	var fg lipgloss.TerminalColor
	bold := false
	switch {
	case pct < 0.01:
		fg = fgOyster
	case pct < 0.50:
		fg = accentBee
	case pct < 0.80:
		fg = accentHoney
	case pct < 0.95:
		fg = accentBusy
		bold = true
	default:
		fg = semError
		bold = true
	}
	filled := lipgloss.NewStyle().Foreground(fg).Bold(bold).Render(strings.Repeat("━", fill))
	rest := lipgloss.NewStyle().Foreground(fgOyster).Render(strings.Repeat("─", m.width-fill))
	return filled + rest
}

// formatUSD picks a precision that keeps small per-turn figures readable:
// 4 decimals under a dollar, 2 above. Always prefixed with $.
func formatUSD(usd float64) string {
	if usd < 1 {
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

// costTierColor maps the running total to a colour, so a glance at the bar
// tells you "still cheap" vs "into the dollars". Magnitude buckets stay
// stable across the flash animation — only intensity cycles.
func costTierColor(usd float64) lipgloss.TerminalColor {
	switch {
	case usd < 0.01:
		return fgSquid // sub-cent: barely visible
	case usd < 0.10:
		return accentBee // soft honey
	case usd < 1.0:
		return accentHoney // bright honey
	default:
		return accentBusy // citron — pay attention
	}
}

// renderCostBadge formats the running session total. After a turn it briefly
// brightens the number and tails a "(+$delta)" chip, then settles back to
// the resting tier colour. No bold, no shimmer — a quiet acknowledgement.
func (m Model) renderCostBadge(usd float64) string {
	flashActive := m.costFlashUntil > 0 && m.costFlashFrame < m.costFlashUntil
	fg := costTierColor(usd)
	if flashActive && m.costFlashFrame < m.costFlashUntil/2 {
		// first half: lift one notch to accentHoney for a subtle pulse
		fg = accentHoney
	}

	number := lipgloss.NewStyle().Foreground(fg).Render(formatUSD(usd))

	if flashActive && m.costFlashDelta > 0 {
		delta := lipgloss.NewStyle().Foreground(fgOyster).Render(" (+" + formatUSD(m.costFlashDelta) + ")")
		return number + delta
	}
	return number
}

// renderModeBadge renders a mode chip. Only shown when mode is auto
// (classifier picks per turn, colored busy citron). Plan and edit modes
// hide the badge so chrome stays quiet.
func (m Model) renderModeBadge() string {
	var fg lipgloss.TerminalColor
	switch m.mode {
	case "plan":
		fg = accentHoney
	case "auto":
		fg = accentBusy
	default:
		fg = fgSquid
	}
	return lipgloss.NewStyle().Foreground(fg).Bold(true).Render(m.mode)
}

// displayModel returns the model name namespaced with its provider when the
// id lacks a "/" separator. Local providers (ollama/lmstudio) and the
// chatgpt OAuth flow ship bare ids like "llama3.1:8b" or "gpt-5"; prefixing
// disambiguates them from hosted "openrouter/..." routes that already carry
// the namespace. No engine → bare model.
func (m Model) displayModel() string {
	prov := ""
	if m.eng != nil {
		prov = m.eng.Cfg.DefaultProvider
	}
	if prov == "" || strings.Contains(m.model, "/") {
		return m.model
	}
	return prov + "/" + m.model
}

// prettyCwd shortens $HOME to ~ for a tidier status line.
func prettyCwd(p string) string {
	if home, err := osUserHome(); err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// overlayCenter draws modal beneath base, centered to width. bubbletea has no
// true overlay primitive — for v0.1 we append; tests assert substrings only.
func overlayCenter(base, modal string, w int) string {
	if modal == "" {
		return base
	}
	return base + "\n\n" + lipgloss.NewStyle().Width(w).Align(lipgloss.Center).Render(modal)
}

// renderIntro draws the current frame of the non-blocking startup animation.
// Each row is colored via a honey gradient (bright honey → soft amber →
// quiet squid) so the braille pixel art reads as molten gold rather than
// flat dim text. Empty before width is known or after animation ends.
func (m Model) renderIntro() string {
	if !m.introActive || len(m.introFrames) == 0 {
		return ""
	}
	if m.introIdx < 0 || m.introIdx >= len(m.introFrames) {
		return ""
	}
	f := m.introFrames[m.introIdx]
	// gradient palette top→bottom — top sits brightest, fades to subtle
	rowColors := []lipgloss.AdaptiveColor{accentHoney, accentBee, fgSquid, fgOyster}
	lines := strings.Split(f.Text, "\n")
	for i, ln := range lines {
		col := rowColors[len(rowColors)-1]
		if i < len(rowColors) {
			col = rowColors[i]
		}
		lines[i] = lipgloss.NewStyle().Foreground(col).Render(ln)
	}
	art := strings.Join(lines, "\n")
	if f.Subtitle == "" {
		return art
	}
	sub := lipgloss.NewStyle().Foreground(fgOyster).Italic(true).Render("  " + f.Subtitle)
	return art + "\n" + sub
}

// renderWarning returns a tiny dim notice line for transient engine events
// (stream retry, watchdog stall). Empty when no warning is active.
func (m Model) renderWarning() string {
	if m.warning == "" {
		return ""
	}
	bee := lipgloss.NewStyle().Foreground(accentHoney).Render("◌")
	body := lipgloss.NewStyle().Foreground(semWarning).Italic(true).Render(m.warning)
	return bee + " " + body
}