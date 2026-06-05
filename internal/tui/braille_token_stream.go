package tui

import (
	"fmt"
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
	Seed     int64
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

// formatLoaderReadout builds the inline figures. budget is the rune width
// available; the readout sheds detail as budget shrinks: full labels →
// unlabeled in/out → out only → empty. InTokens 0 renders an em-dash so a
// not-yet-known input never shows a fake number.
func formatLoaderReadout(stats LoaderStats, budget int) string {
	in := "—"
	if stats.InTokens > 0 {
		in = fmtTokens(stats.InTokens)
	}
	out := fmtTokens(stats.OutChars)

	full := fmt.Sprintf("in %s tok · out %s ch", in, out)
	if utf8.RuneCountInString(full) <= budget {
		return full
	}
	mid := fmt.Sprintf("in %s · out %s", in, out)
	if utf8.RuneCountInString(mid) <= budget {
		return mid
	}
	short := "out " + out
	if utf8.RuneCountInString(short) <= budget {
		return short
	}
	if utf8.RuneCountInString(out) <= budget {
		return out
	}
	return ""
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
