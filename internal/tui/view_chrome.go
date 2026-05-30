package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

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
	if gl := m.goalStatusLine(); gl != "" {
		if left != "" {
			left += "  "
		}
		left += m.styles.Dim.Render("◎ " + gl)
	}
	right := ""
	if timer := m.renderTurnTimer(); timer != "" {
		right += timer + "  "
	}
	if badge := m.renderModeBadge(); badge != "" {
		right += badge + "  "
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
		return m.styles.RoleBee.Render(formatElapsed(d))
	}
	if m.lastTurnDuration > 0 {
		return m.styles.Dim.Render(formatElapsed(m.lastTurnDuration))
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

// renderModeBadge renders a mode chip in the top bar, always visible so
// shift+tab cycling is legible. plan = honey, auto = citron, yolo = sriracha
// (auto-approves, pay attention), edit = quiet squid (the resting default).
func (m Model) renderModeBadge() string {
	if m.mode == "" {
		return ""
	}
	var fg lipgloss.TerminalColor
	switch m.mode {
	case "plan":
		fg = accentHoney
	case "auto":
		fg = accentBusy
	case "yolo":
		fg = semError
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
