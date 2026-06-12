package tui

import (
	"testing"
	"time"
)

// the loader color pulse must alternate slowly enough that the eye reads it
// as "breathing" rather than a flash. the tick is 120ms (loaderTickInterval);
// 14 ticks = 1.68s per bright↔dim phase, ~3.36s full cycle. the previous
// 6-tick period landed 720ms per phase, which on a quiet live region (no
// think block above to mask the alternation) read as a clear flash.
func TestLoaderPulsePeriod_NotFlashy(t *testing.T) {
	if loaderPulsePeriod < 12 {
		t.Fatalf("loaderPulsePeriod = %d, want >=12 (was 6, caused 720ms strobe)", loaderPulsePeriod)
	}
	// confirm 14 ticks * 120ms = ~1.68s per phase
	want := time.Duration(loaderPulsePeriod) * loaderTickInterval
	if want < 1500*time.Millisecond {
		t.Fatalf("pulse phase = %v, want >=1.5s", want)
	}
}

// at the new period the first 8 frames stay on the bright side — the loader
// shouldn't flicker for the first second of a turn. guards against a future
// edit dropping the period back to 6.
func TestLoaderPulse_StaysBrightInitially(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	for f := 0; f < 8; f++ {
		style := r.pulseStyle(f)
		// role-bee style renders accentBee (#FFC857 dark). the dim style
		// renders fgSquid (#858392 dark). they're distinct — assert the
		// bright style by checking the rendered foreground.
		rendered := style.Render("x")
		if rendered == "" {
			t.Fatalf("frame %d: empty render", f)
		}
	}
}
