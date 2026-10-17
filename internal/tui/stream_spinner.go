package tui

import (
	"strings"
	"time"
)

// braille dots: one cell, smooth, reads as "working" in any font
var spinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// flavor words for the busy row, one per turn (from the loader seed) so
// the word holds still while the glyph turns
var spinnerVerbs = [...]string{
	"Foraging", "Buzzing", "Pollinating", "Humming",
	"Brewing", "Gathering", "Waggling", "Swarming",
}

const interruptHint = "esc to interrupt"

func (r *StreamRenderer) spinnerVerb() string {
	seed := r.loaderStats.Seed
	if seed < 0 {
		seed = -seed
	}
	return spinnerVerbs[seed%int64(len(spinnerVerbs))]
}

// busy row for a streaming turn:
//
//	⠹ Foraging…  12s · ↑ 3.4k · ↓ 340 · esc to interrupt
//
// only the glyph turns; with show_loader off it's the static bee mark
func (r *StreamRenderer) RenderSpinner(frame int, elapsed time.Duration) string {
	return r.RenderSpinnerLabel(frame, r.spinnerVerb()+"…", elapsed, interruptHint)
}

// RenderSpinner with a custom label and hint (compaction, handoff)
func (r *StreamRenderer) RenderSpinnerLabel(frame int, label string, elapsed time.Duration, hint string) string {
	if frame < 0 {
		frame = -frame
	}
	glyph := glyphBee
	if r.showLoader {
		glyph = spinnerFrames[frame%len(spinnerFrames)]
	}
	row := r.styles.Mark.Render(glyph) + " " + r.styles.Key.Render(label)

	var meta []string
	if elapsed >= time.Second {
		meta = append(meta, formatClock(elapsed))
	}
	st := r.loaderStats
	if st.ShowIn && st.InTokens > 0 {
		meta = append(meta, "↑ "+tokensHuman(st.InTokens))
	}
	if st.ShowOut && st.OutTokens > 0 {
		meta = append(meta, "↓ "+tokensHuman(st.OutTokens))
	}
	if st.ShowRate {
		if st.RateTokS >= 1 {
			meta = append(meta, tokensHuman(int(st.RateTokS))+" tok/s")
		}
	}
	if hint != "" {
		meta = append(meta, hint)
	}
	if len(meta) > 0 {
		row += "  " + r.styles.Dim.Render(strings.Join(meta, " "+glyphSep+" "))
	}
	return r.gutter(clipLine(row, r.width-2))
}
