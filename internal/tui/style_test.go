package tui

import "testing"

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
