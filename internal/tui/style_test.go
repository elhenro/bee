package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestRainbowColorForFrame_Cycles verifies the glow helper sweeps the hue wheel
// and wraps: distinct colors across a cycle, and frame N == frame N+period.
func TestRainbowColorForFrame_Cycles(t *testing.T) {
	const period = 60

	c0 := rainbowColorForFrame(0)
	cHalf := rainbowColorForFrame(period / 2)
	if c0 == cHalf {
		t.Errorf("frame 0 and frame %d gave the same color %q — not cycling", period/2, c0)
	}

	// full period wraps back to the start.
	if got := rainbowColorForFrame(period); got != c0 {
		t.Errorf("frame %d = %q, want wrap to frame 0 = %q", period, got, c0)
	}

	// negative frames are handled (modulo guard), never panicking or empty.
	if got := rainbowColorForFrame(-1); got == "" {
		t.Errorf("frame -1 produced an empty color")
	}

	// every color is a valid 7-char hex string (#RRGGBB).
	for _, f := range []int{0, 7, 15, 23, 31, 59} {
		s := string(rainbowColorForFrame(f))
		if len(s) != 7 || s[0] != '#' {
			t.Errorf("frame %d color = %q, want #RRGGBB", f, s)
		}
	}
}

// the 256 color cube has no quiet dark green or red, so without truecolor a
// diff wash would be a saturated slab: the wash is dropped, not quantized
func TestWash_TruecolorOnly(t *testing.T) {
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	lipgloss.SetColorProfile(termenv.TrueColor)
	if got := wash(bgDiffAddHl).Render("x"); got == "x" {
		t.Fatalf("truecolor wash rendered no background: %q", got)
	}
	for _, p := range []termenv.Profile{termenv.ANSI256, termenv.ANSI} {
		lipgloss.SetColorProfile(p)
		if got := wash(bgDiffAddHl).Render("x"); got != "x" {
			t.Fatalf("profile %v kept a wash: %q", p, got)
		}
	}
}

func TestUserPanel_NeutralLiftWithoutTruecolor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	lipgloss.SetColorProfile(termenv.TrueColor)
	if got := userPanel(); got != bgUserHl {
		t.Fatalf("truecolor panel = %+v, want the warm tint", got)
	}
	lipgloss.SetColorProfile(termenv.ANSI256)
	if got := userPanel(); got == bgUserHl {
		t.Fatal("256 color panel kept the truecolor tint, which quantizes below the terminal background")
	}
}
