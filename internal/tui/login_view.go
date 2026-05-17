package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/commands"
)

// View renders the modal. width/height match the host frame; boxModal
// applies the rounded border + padding.
func (p *LoginPane) View(width, height int) string {
	if p == nil || !p.open {
		return ""
	}
	if p.inputting {
		return p.viewInput(width, height)
	}
	title := lipgloss.NewStyle().
		Foreground(accentHoney).
		Bold(true).
		Render("⬢ Login")

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")
	if len(p.list) == 0 {
		b.WriteString(StyleLabel.Render("no providers configured. add a [providers.<name>] block to ~/.bee/config.toml"))
	} else {
		nameW := 0
		for _, p2 := range p.list {
			n := len(p2.Name)
			if p2.IsDefault {
				n += len(" (default)")
			}
			if n > nameW {
				nameW = n
			}
		}
		for i, p2 := range p.list {
			b.WriteString(renderLoginRow(p2, i == p.cursor, nameW))
			b.WriteString("\n")
		}
	}
	if p.status != "" {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(accentBee).Render(p.status))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(StyleLabel.Render("↑/↓ pick · enter login/enter key · d remove · r refresh · esc close"))
	return boxModal(b.String(), width, height)
}

// viewInput renders the api-key entry sub-mode.
func (p *LoginPane) viewInput(width, height int) string {
	title := lipgloss.NewStyle().
		Foreground(accentHoney).
		Bold(true).
		Render("⬢ Login · " + p.inputFor)
	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString(StyleLabel.Render("api key (input hidden):"))
	b.WriteString("\n")
	b.WriteString(p.keyInput.View())
	b.WriteString("\n")
	if p.status != "" {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(accentBee).Render(p.status))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(StyleLabel.Render("enter save · esc cancel"))
	return boxModal(b.String(), width, height)
}

// renderLoginRow draws one provider row with cursor marker, name, default
// flag, and auth-state indicators.
func renderLoginRow(p commands.ProviderAuth, selected bool, nameW int) string {
	marker := "  "
	nameStyle := lipgloss.NewStyle().Foreground(fgOyster)
	if selected {
		marker = lipgloss.NewStyle().Foreground(accentHoney).Render("▸ ")
		nameStyle = nameStyle.Foreground(accentHoney).Bold(true)
	}
	name := p.Name
	if p.IsDefault {
		name += " (default)"
	}
	return marker + nameStyle.Render(padRightVisible(name, nameW)) + "  " + StyleLabel.Render(loginRowState(p))
}

// loginRowState mirrors commands.authSummary (which lives in the commands
// package and isn't exported). Returns a terse, glyph-prefixed line.
func loginRowState(p commands.ProviderAuth) string {
	var parts []string
	if p.HasOAuth {
		if p.TokenSaved {
			parts = append(parts, "✓ oauth (token saved)")
		} else {
			parts = append(parts, "○ oauth (press enter)")
		}
	}
	switch {
	case p.EnvKey == "":
		if !p.HasOAuth {
			parts = append(parts, "no auth (local)")
		}
	case p.EnvSet:
		parts = append(parts, "✓ env "+p.EnvKey)
	case p.KeySaved:
		parts = append(parts, "✓ key saved ("+p.EnvKey+")")
	case p.KeyOptional:
		parts = append(parts, "○ "+p.EnvKey+" optional")
	default:
		parts = append(parts, "○ "+p.EnvKey+" (press enter)")
	}
	return strings.Join(parts, " · ")
}

// padRightVisible pads s with spaces so its display width = n. Stays
// ASCII-safe since provider names + " (default)" are all ASCII.
func padRightVisible(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
