package agents

import "github.com/charmbracelet/lipgloss"

// Palette mirrors internal/tui's honey-minimal system (kept in sync by eye,
// not import, so this package stays self-contained and unit-testable). Chrome
// stays neutral; honey marks what is live, semantic hues stay quiet.
var (
	honey     = lipgloss.AdaptiveColor{Light: "#A56F00", Dark: "#FFB000"}
	body      = lipgloss.AdaptiveColor{Light: "#45434F", Dark: "#B9B6C2"}
	dim       = lipgloss.AdaptiveColor{Light: "#8B8996", Dark: "#5F5E6A"}
	good      = lipgloss.AdaptiveColor{Light: "#0A7A4F", Dark: "#6FD6A0"}
	warn      = lipgloss.AdaptiveColor{Light: "#8A6A00", Dark: "#E6C35C"}
	bad       = lipgloss.AdaptiveColor{Light: "#C0203F", Dark: "#EE6A82"}
	busy      = honey
	bgUser    = lipgloss.AdaptiveColor{Light: "#F6ECD0", Dark: "#2B261A"}
	bgSurface = lipgloss.AdaptiveColor{Light: "#E8E4DD", Dark: "#2D2C35"}
)

var (
	titleStyle      = lipgloss.NewStyle().Foreground(honey).Bold(true)
	subtitleStyle   = lipgloss.NewStyle().Foreground(dim).Italic(true)
	sectionStyle    = lipgloss.NewStyle().Foreground(honey).Bold(true)
	errSectionStyle = lipgloss.NewStyle().Foreground(bad).Bold(true)
	dimStyle        = lipgloss.NewStyle().Foreground(dim)
	bodyStyle       = lipgloss.NewStyle().Foreground(body)
	warnStyle       = lipgloss.NewStyle().Foreground(warn)
	badStyle        = lipgloss.NewStyle().Foreground(bad)
	goodStyle       = lipgloss.NewStyle().Foreground(good)
	busyStyle       = lipgloss.NewStyle().Foreground(busy)
	cursorStyle     = lipgloss.NewStyle().Foreground(honey).Bold(true)
	modelChipStyle  = lipgloss.NewStyle().Foreground(dim)
	headerStyle     = lipgloss.NewStyle().Foreground(body)
)
