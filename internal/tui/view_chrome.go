package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// renderTopBar draws the configurable status line. Item set and order come
// from the status registry (see statusline.go) via the tui.status_line config
// list; an absent key falls back to the built-in default order, and an empty
// list hides the row. Kept as a thin alias so callers (View, live-budget)
// stay unchanged. The hex glyph doubles as a context-fill pie: outline when
// empty, filled and color-tiered as input tokens grow against the model's
// context window.
func (m Model) renderTopBar() string {
	return m.renderStatusLine()
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
		// the busy row ticks it; only pinned loaders need it here
		if m.stream != nil && m.stream.isDefaultLoader() {
			return ""
		}
		return m.styles.RoleBee.Render(formatElapsed(time.Since(m.turnStartedAt)))
	}
	// a bare "0.8s" says nothing: long turns only
	if m.lastTurnDuration >= 3*time.Second {
		return m.styles.Dim.Render("took " + formatElapsed(m.lastTurnDuration))
	}
	return ""
}

// formatElapsed returns a human, readable duration string. Sub-second uses
// one decimal so a fast turn doesn't read "0s"; sub-minute drops decimals;
// longer durations switch to compact M m S s / H h M m forms. Designed to
// stay ≤7 chars so the top-bar slot doesn't push other chips around.
func formatElapsed(d time.Duration) string {
	switch {
	case d <= 0:
		return "0.0s"
	case d < 10*time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		return formatClock(d)
	}
	return fmt.Sprintf("%dh %02dm", int(d/time.Hour), int((d%time.Hour)/time.Minute))
}

// formatElapsed without decimals, so a ticking row doesn't strobe
func formatClock(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm %02ds", int(d/time.Minute), int((d%time.Minute)/time.Second))
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

// renderRoleBadge renders the active-role chip in the top bar, always visible
// so shift+tab cycling is legible. worker = quiet squid (resting default),
// scout = honey, queen = lilac (regal, pairs with the queen rainbow glow).
func (m Model) renderRoleBadge() string {
	if m.role == "" {
		return ""
	}
	var fg lipgloss.TerminalColor
	switch m.role {
	case "scout":
		fg = accentHoney
	case "queen":
		fg = accentTool
	default: // worker
		fg = fgSquid
	}
	return lipgloss.NewStyle().Foreground(fg).Bold(true).Render(m.role)
}

// renderEffortBadge formats the active reasoning-budget level as "t:<level>".
// Hidden when the resolved value is empty or "auto" — auto is the default and
// has no signal to convey (the role chip already implies the baked budget).
// Colour tracks intensity: off/auto dim, low/medium honey, high/max tool-accent.
func (m Model) renderEffortBadge() string {
	v := strings.ToLower(strings.TrimSpace(m.thinking))
	if v == "" || v == "auto" {
		return ""
	}
	var fg lipgloss.TerminalColor
	switch v {
	case "off":
		fg = fgSquid
	case "low", "med", "medium":
		fg = accentHoney
	default: // high, max
		fg = accentTool
	}
	return lipgloss.NewStyle().Foreground(fg).Render("t:" + v)
}

// renderYoloBadge renders a loud inverse-red YOLO block, shown only while the
// yolo toggle is armed (auto-approves dangerous shell — pay attention).
func (m Model) renderYoloBadge() string {
	if !m.yolo {
		return ""
	}
	return lipgloss.NewStyle().
		Foreground(fgButter).Background(semError).
		Bold(true).Padding(0, 1).
		Render("YOLO")
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
