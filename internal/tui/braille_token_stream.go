package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Token-stream loader: the default "generating" animation. A procedurally
// seeded particle stream whose density tracks live output throughput, with
// an inline readout of the actual input tokens + output chars for the turn.
// Replaces the phased braille default; named BEE_LOADER styles are untouched.

// LoaderStats carries the live turn figures the strip visualizes. InTokens
// is the real context-token count sent (costs.LastInput); OutChars is the
// cumulative chars generated this turn (text + reasoning); Rate is chars
// produced since the previous frame (drives particle density); Seed varies
// the procedural layout per turn.
type LoaderStats struct {
	InTokens int
	OutChars int
	Rate     int
	// RateTokS is the EMA-smoothed generation throughput in tok/s for the
	// readout. Windowed, so it reflects current speed rather than the turn's
	// cumulative average.
	RateTokS float64
	Seed     int64
	// figure visibility toggles (/settings). Default true; when all three
	// are off the readout collapses to empty and only the stream shows.
	ShowIn   bool
	ShowOut  bool
	ShowRate bool
}

// tokenStreamSpeed is the per-frame horizontal step (in px) of a particle.
const tokenStreamSpeed = 2

// renderTokenStream paints the particle stream into a cells-wide braille row.
// Particles flow left→right and wrap; count scales with throughput so an
// idle wait drifts sparse and fast generation packs dense. seed-derived
// per-particle phase + lane + trail length keep every turn visually distinct.
func renderTokenStream(stats LoaderStats, frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)

	// particle count: a sparse floor for the idle wait plus a throughput
	// term. Capped at one bee per cell so wide rows don't smear solid.
	n := 3 + stats.Rate/3
	if n > cells {
		n = cells
	}
	if n < 3 {
		n = 3
	}

	rng := newLCG(stats.Seed)
	cycle := w + 8
	for i := 0; i < n; i++ {
		// per-particle procedural params from the seeded stream.
		off := int(rng.next() % uint64(cycle))
		lane := int(rng.next() % uint64(braillePxH))
		trail := 1 + int(rng.next()%2)

		x := (frame*tokenStreamSpeed + off) % cycle
		if x >= w {
			continue // in the gap past the right edge — momentarily hidden
		}
		c.SetPixel(x, lane, true)
		for t := 1; t <= trail; t++ {
			if x-t >= 0 {
				c.SetPixel(x-t, lane, true)
			}
		}
	}
	return c.ToBraille()
}

// formatLoaderReadout builds the inline figures, honoring the per-figure
// visibility toggles (ShowIn/ShowOut/ShowRate). budget is the rune width
// available; when the enabled figures don't fit it sheds from the left
// (↑in first, then ↓out) keeping the rightmost figure as long as possible.
func formatLoaderReadout(stats LoaderStats, budget int) string {
	// parts in display order; each entry self-contained so dropping the
	// leftmost yields a still-valid string.
	var parts []string
	if stats.ShowIn {
		parts = append(parts, "↑ "+fmtTokens(stats.InTokens))
	}
	if stats.ShowOut {
		parts = append(parts, "↓ "+fmtTokens(stats.OutChars))
	}
	if stats.ShowRate {
		if rate := formatTokRate(stats.RateTokS); rate != "" {
			parts = append(parts, rate)
		}
	}
	// widest joined run that fits, shedding leftmost figures first.
	for i := 0; i < len(parts); i++ {
		s := strings.Join(parts[i:], " ")
		if utf8.RuneCountInString(s) <= budget {
			return s
		}
	}
	return ""
}

// charsPerToken is the rough chars-per-token divisor used to turn the live
// char count into a tok/s estimate. ~4 fits typical English/code; exact
// tokenization isn't available mid-stream.
const charsPerToken = 4.0

// formatTokRate renders the smoothed throughput as "<n>tok/s". Empty below 1
// tok/s so an idle gap shows nothing rather than "0tok/s".
func formatTokRate(tokS float64) string {
	if tokS < 1 {
		return ""
	}
	return fmt.Sprintf("%stok/s", fmtTokens(int(tokS)))
}

// lcg is a tiny deterministic generator so seeded layouts reproduce exactly
// in tests without touching the global math/rand state.
type lcg struct{ s uint64 }

func newLCG(seed int64) *lcg {
	// fold seed into a nonzero state; constant is a common LCG multiplier.
	return &lcg{s: uint64(seed)*2862933555777941757 + 3037000493}
}

func (g *lcg) next() uint64 {
	g.s = g.s*6364136223846793005 + 1442695040888963407
	return g.s >> 11
}
