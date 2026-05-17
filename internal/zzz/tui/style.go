// Package tui drives the bee-zzz live interface: animated sleeping bee at
// the footer, moon-phase iteration timeline, transcript pane, and a
// textarea for steering commands (/stop /abort /note <text>).
//
// Palette is duplicated from internal/tui (same hex values) so the look
// matches the main chat without exporting unexported color vars.
package tui

import "github.com/charmbracelet/lipgloss"

var (
	fgOyster = lipgloss.AdaptiveColor{Light: "#7C7A86", Dark: "#605F6B"}
	fgSquid  = lipgloss.AdaptiveColor{Light: "#5D5B6A", Dark: "#858392"}
	fgSmoke  = lipgloss.AdaptiveColor{Light: "#3F3E4A", Dark: "#BFBCC8"}
	fgAsh    = lipgloss.AdaptiveColor{Light: "#201F26", Dark: "#DFDBDD"}
	fgButter = lipgloss.AdaptiveColor{Light: "#201F26", Dark: "#FFFAF1"}

	bgCharcoal = lipgloss.AdaptiveColor{Light: "#D2CEC6", Dark: "#3A3943"}

	accentHoney = lipgloss.AdaptiveColor{Light: "#A56F00", Dark: "#FFB000"}
	accentBee   = lipgloss.AdaptiveColor{Light: "#8A5A00", Dark: "#FFC857"}
	accentYou   = lipgloss.AdaptiveColor{Light: "#0064B5", Dark: "#00A4FF"}
	accentTool  = lipgloss.AdaptiveColor{Light: "#6B4A93", Dark: "#B084CC"}
	semSuccess  = lipgloss.AdaptiveColor{Light: "#007A52", Dark: "#00FFB2"}
	semWarning  = lipgloss.AdaptiveColor{Light: "#7C7300", Dark: "#F5EF34"}
	semError    = lipgloss.AdaptiveColor{Light: "#C0203F", Dark: "#EB4268"}
)

var (
	styHoney   = lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
	styBee     = lipgloss.NewStyle().Foreground(accentBee).Bold(true)
	styYou     = lipgloss.NewStyle().Foreground(accentYou).Bold(true)
	styTool    = lipgloss.NewStyle().Foreground(accentTool)
	styDim     = lipgloss.NewStyle().Foreground(fgOyster)
	stySmoke   = lipgloss.NewStyle().Foreground(fgSmoke)
	styBody    = lipgloss.NewStyle().Foreground(fgAsh)
	styBright  = lipgloss.NewStyle().Foreground(fgButter).Bold(true)
	stySuccess = lipgloss.NewStyle().Foreground(semSuccess)
	styWarning = lipgloss.NewStyle().Foreground(semWarning)
	styError   = lipgloss.NewStyle().Foreground(semError).Bold(true)
	styBorder  = lipgloss.NewStyle().Foreground(bgCharcoal)
	styItalic  = lipgloss.NewStyle().Foreground(fgOyster).Italic(true)
)

// phaseGlyph maps iteration status to a bee/flower icon in the timeline.
// honey-jar for a successful commit (bee made the honey), busy-bee while
// foraging, empty flower for noop (visited but no nectar), wilted for
// reset, splat for failure, sprout for not-yet-started.
func phaseGlyph(status string) string {
	switch status {
	case "committed":
		return "🌼"
	case "running":
		return "🐝"
	case "noop":
		return "🍃"
	case "reset":
		return "🥀"
	case "failed":
		return "💥"
	case "pending":
		return "🌱"
	}
	return "·"
}

func phaseColor(status string) lipgloss.Style {
	switch status {
	case "committed":
		return stySuccess
	case "noop":
		return styDim
	case "reset", "failed":
		return styError
	case "running":
		return styBee
	}
	return stySmoke
}
