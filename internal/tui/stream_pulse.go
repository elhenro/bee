package tui

import (
	"fmt"
	"math"

	"github.com/charmbracelet/lipgloss"
)

// full breathe cycle of an in-flight bullet in loader ticks (~2.9s at
// 120ms): slow enough to read as breathing, not blinking
const pendingPulsePeriod = 24

// deepest dip, as the share of the way from honey to faint; stays visible
// at the trough so the bullet never reads as gone
const pendingPulseDepth = 0.7

// pulseColor eases the honey accent toward faint and back on a cosine,
// one cycle per pendingPulsePeriod frames
func pulseColor(frame int) lipgloss.Color {
	phase := float64(frame%pendingPulsePeriod) / pendingPulsePeriod
	t := pendingPulseDepth * (1 - math.Cos(2*math.Pi*phase)) / 2
	hi, lo := adaptiveHex(accentHoney), adaptiveHex(fgOyster)
	var out [3]int
	for i := range out {
		out[i] = int(math.Round(float64(hi[i]) + (float64(lo[i])-float64(hi[i]))*t))
	}
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", out[0], out[1], out[2]))
}

// adaptiveHex picks the variant for the current background and splits it
// into rgb
func adaptiveHex(c lipgloss.AdaptiveColor) [3]int {
	hex := c.Light
	if lipgloss.HasDarkBackground() {
		hex = c.Dark
	}
	var rgb [3]int
	fmt.Sscanf(hex, "#%02x%02x%02x", &rgb[0], &rgb[1], &rgb[2])
	return rgb
}
