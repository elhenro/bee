package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

func (m Model) renderTopBar() string {
	// Slim, dim status line. Hex glyph doubles as a context-fill pie:
	// outline when empty, filled and color-tiered as input tokens grow
	// against the model's context window. Each chunk is independently
	// toggleable via /settings; flipping all five user-visible chunks off
	// collapses the entire row (caller drops the empty string from parts).
	if !m.showBee && !m.showContextPct && !m.showModel && !m.showCwd && !m.showEffort && !m.showTurnTimer && !m.showGitBranch && !m.showTotalTokens {
		return ""
	}
	hex := m.renderContextHex()
	var leftParts []string
	if hex != "" {
		leftParts = append(leftParts, hex)
	}
	if m.showModel {
		leftParts = append(leftParts, m.styles.Dim.Render(m.displayModel()))
	}
	if m.showCwd {
		leftParts = append(leftParts, m.styles.Dim.Render(prettyCwd(m.cwd)))
	}
	if m.showGitBranch {
		if br := gitBranch(m.cwd); br != "" {
			leftParts = append(leftParts, m.styles.Dim.Render("⎇ "+br))
		}
	}
	left := strings.Join(leftParts, "  ")
	if m.costs != nil && !m.isLocalProvider() {
		tot := m.costs.Total()
		// only render badge when there's actual spend — free local models
		// (ollama, lm-studio) report 0 USD and shouldn't show "$0.0000".
		if tot.USD > 0 {
			if left != "" {
				left += "  "
			}
			left += m.renderCostBadge(tot.USD)
		}
	}
	if m.showTotalTokens && m.costs != nil {
		tot := m.costs.Total()
		if n := tot.Input + tot.Output; n > 0 {
			if left != "" {
				left += "  "
			}
			left += m.styles.Dim.Render("Σ" + tokensHuman(n))
		}
	}
	right := ""
	if timer := m.renderTurnTimer(); timer != "" {
		right += timer + "  "
	}
	if m.mode == "auto" {
		right += m.renderModeBadge() + "  "
	}
	if m.showEffort && m.thinking != "" && m.thinking != "off" {
		right += m.styles.Dim.Render("t:"+m.thinking) + " "
	}
	pad := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

// renderTurnTimer formats a tiny right-side chip showing how long the bee
// has been working on the current turn (live) or how long the most recent
// turn took (final). Empty string when neither applies. Live ticks via the
// loaderTickMsg cadence; final persists until the next submit clears it.
//
// Two visual tiers: live uses RoleBee accent (matches the streaming loader
// palette), final uses Dim (a quiet "done" acknowledgement). Hourglass +
// space + duration. No bold, no flash — same restraint as the cost badge.
func (m Model) renderTurnTimer() string {
	if !m.showTurnTimer {
		return ""
	}
	if m.state == StateStreaming && !m.turnStartedAt.IsZero() {
		d := time.Since(m.turnStartedAt)
		return m.styles.RoleBee.Render("⏱ " + formatElapsed(d))
	}
	if m.lastTurnDuration > 0 {
		return m.styles.Dim.Render("⏱ " + formatElapsed(m.lastTurnDuration))
	}
	return ""
}

// formatElapsed returns a human, readable duration string. Sub-second uses
// one decimal so a fast turn doesn't read "0s"; sub-minute drops decimals;
// longer durations switch to compact M m S s / H h M m forms. Designed to
// stay ≤7 chars so the top-bar slot doesn't push other chips around.
func formatElapsed(d time.Duration) string {
	if d <= 0 {
		return "0.0s"
	}
	if d < time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < 10*time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		mins := int(d / time.Minute)
		secs := int((d % time.Minute) / time.Second)
		return fmt.Sprintf("%dm %02ds", mins, secs)
	}
	hrs := int(d / time.Hour)
	mins := int((d % time.Hour) / time.Minute)
	return fmt.Sprintf("%dh %02dm", hrs, mins)
}

// liveBudget returns the row budget available for the streaming live region,
// computed as terminal height minus every other non-mid part (chrome) and
// the inter-part newline separators. Returns 0 when height is unknown or
// chrome already fills the screen — caller treats 0 as "no clipping".
func liveBudget(termH int, parts ...string) int {
	if termH <= 0 {
		return 0
	}
	chrome := 0
	nonEmpty := 0
	for _, p := range parts {
		if p == "" {
			continue
		}
		chrome += lipgloss.Height(p)
		nonEmpty++
	}
	// `parts` joined with "\n"; with mid present there's one extra separator
	// between mid and the rest. Final "\n" between blocks costs 1 row each.
	separators := nonEmpty // mid + nonEmpty parts → nonEmpty separators
	// reserve 1 row for the cursor / inline-render safety margin.
	budget := termH - chrome - separators - 1
	if budget < 1 {
		return 1
	}
	return budget
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
	if !m.showBee && !m.showContextPct {
		return ""
	}
	pct := m.contextPct()
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
	var out string
	if m.showBee {
		out = style.Render("🐝")
	}
	if m.showContextPct && pct > 0 {
		// rounded percent; cap display at 999% to avoid layout breaks if
		// LastInput somehow exceeds the window.
		p := int(pct*100 + 0.5)
		if p > 999 {
			p = 999
		}
		label := style.Render(fmt.Sprintf("%d%%", p))
		if out != "" {
			out += " " + label
		} else {
			out = label
		}
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

// gitBranch returns the current branch name when cwd lives inside a git repo.
// Walks up looking for .git (handles worktree pointer files too) and reads
// HEAD. Returns the branch name from "ref: refs/heads/<name>", or a 7-char
// short sha when HEAD is detached. Empty string when cwd is not in a repo.
// Cheap enough to call per-render: two small file reads at most.
func gitBranch(cwd string) string {
	if cwd == "" {
		return ""
	}
	dir := cwd
	for i := 0; i < 32; i++ {
		gitPath := filepath.Join(dir, ".git")
		st, err := os.Stat(gitPath)
		if err == nil {
			gitDir := gitPath
			if !st.IsDir() {
				// worktree pointer: ".git" file with "gitdir: <path>"
				b, err := os.ReadFile(gitPath)
				if err != nil {
					return ""
				}
				line := strings.TrimSpace(string(b))
				if !strings.HasPrefix(line, "gitdir: ") {
					return ""
				}
				gd := strings.TrimPrefix(line, "gitdir: ")
				if !filepath.IsAbs(gd) {
					gd = filepath.Join(dir, gd)
				}
				gitDir = gd
			}
			head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
			if err != nil {
				return ""
			}
			s := strings.TrimSpace(string(head))
			if rest, ok := strings.CutPrefix(s, "ref: refs/heads/"); ok {
				return rest
			}
			if len(s) >= 7 {
				return s[:7]
			}
			return s
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// tokensHuman formats a token count compactly: 1234 → "1.2k", 1_500_000 → "1.5M".
// Sub-1000 stays bare. One decimal point until 100, none above.
func tokensHuman(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		v := float64(n) / 1000
		if v < 10 {
			return fmt.Sprintf("%.1fk", v)
		}
		return fmt.Sprintf("%dk", int(v+0.5))
	default:
		v := float64(n) / 1_000_000
		if v < 10 {
			return fmt.Sprintf("%.1fM", v)
		}
		return fmt.Sprintf("%dM", int(v+0.5))
	}
}