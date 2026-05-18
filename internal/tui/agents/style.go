package agents

import "github.com/charmbracelet/lipgloss"

// Palette mirrors internal/tui's honey-on-dark system (kept in sync by eye,
// not import, so this package stays self-contained and unit-testable).
var (
	honey     = lipgloss.AdaptiveColor{Light: "#A56F00", Dark: "#FFB000"}
	body      = lipgloss.AdaptiveColor{Light: "#3F3E4A", Dark: "#BFBCC8"}
	dim       = lipgloss.AdaptiveColor{Light: "#7C7A86", Dark: "#605F6B"}
	good      = lipgloss.AdaptiveColor{Light: "#007A52", Dark: "#00FFB2"}
	warn      = lipgloss.AdaptiveColor{Light: "#7C7300", Dark: "#F5EF34"}
	bad       = lipgloss.AdaptiveColor{Light: "#C0203F", Dark: "#EB4268"}
	busy      = lipgloss.AdaptiveColor{Light: "#5E6E00", Dark: "#E8FF27"}
	bgUser    = lipgloss.AdaptiveColor{Light: "#FFE9B8", Dark: "#3A2E14"}
	bgSurface = lipgloss.AdaptiveColor{Light: "#E8E4DD", Dark: "#2D2C35"}
)

var (
	titleStyle     = lipgloss.NewStyle().Foreground(honey).Bold(true)
	subtitleStyle  = lipgloss.NewStyle().Foreground(dim).Italic(true)
	sectionStyle   = lipgloss.NewStyle().Foreground(honey).Bold(true)
	errSectionStyle = lipgloss.NewStyle().Foreground(bad).Bold(true)
	dimStyle       = lipgloss.NewStyle().Foreground(dim)
	bodyStyle      = lipgloss.NewStyle().Foreground(body)
	warnStyle      = lipgloss.NewStyle().Foreground(warn)
	badStyle       = lipgloss.NewStyle().Foreground(bad)
	goodStyle      = lipgloss.NewStyle().Foreground(good)
	busyStyle      = lipgloss.NewStyle().Foreground(busy)
	cursorStyle    = lipgloss.NewStyle().Foreground(honey).Bold(true)
	modelChipStyle = lipgloss.NewStyle().Foreground(dim)
	headerStyle    = lipgloss.NewStyle().Foreground(body)
)
