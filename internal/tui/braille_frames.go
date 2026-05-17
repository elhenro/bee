package tui

// Braille-rendered loader animations. Every painter takes a frame counter
// and a canvas width in braille cells, returning a single-row braille
// string of exactly that many runes. They are the ONLY animation source
// — the legacy ASCII honeycomb/dance/drip frames were retired in favor
// of dense pixel art.

// brailleLoaderMinCells / Max keep the canvas inside sane bounds even on
// tiny or huge terminals.
const (
	brailleLoaderMinCells = 8
	brailleLoaderMaxCells = 240
)

// clampCells clips a requested cell-width to [Min, Max].
func clampCells(cells int) int {
	if cells < brailleLoaderMinCells {
		return brailleLoaderMinCells
	}
	if cells > brailleLoaderMaxCells {
		return brailleLoaderMaxCells
	}
	return cells
}

// renderBraillePulse — a stylized bee silhouette (4×4 px) that beats its
// wings on a 6-frame cadence. Centered horizontally, gentle.
var brailleBeeIdle = []byte{
	0, 1, 1, 0,
	1, 1, 1, 1,
	1, 1, 1, 1,
	0, 1, 1, 0,
}

var brailleBeeFlap = []byte{
	1, 0, 0, 1,
	1, 1, 1, 1,
	1, 1, 1, 1,
	1, 0, 0, 1,
}

func renderBraillePulse(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	sprite := brailleBeeIdle
	if (frame/6)%2 == 0 {
		sprite = brailleBeeFlap
	}
	c.DrawSprite(sprite, 4, 4, w/2-2, 0)
	return c.ToBraille()
}

// braillePainter is a labeled painter — used by the named-style picker.
type braillePainter struct {
	name string
	fn   func(frame, cells int) string
}

// brailleNamedPainters lists every named loader animation, keyed by the
// canonical name used in BEE_LOADER. Random pick draws from this list.
var brailleNamedPainters = []braillePainter{
	{"pulse", renderBraillePulse},
	{"swarm", renderBrailleSwarm},
	{"wave", renderBrailleWave},
	{"comet", renderBrailleComet},
	{"hex", renderBrailleHex},
	{"ripple", renderBrailleRipple},
	{"rain", renderBrailleRain},
	{"orbit", renderBrailleOrbit},
	{"breath", renderBrailleBreath},
	{"stars", renderBrailleStarfield},
	{"forage", renderBrailleForage},
	{"figure8", renderBrailleFigure8},
	{"vortex", renderBrailleVortex},
	{"gust", renderBrailleGust},
	{"scatter", renderBrailleScatter},
	{"flock", renderBrailleFlock},
	{"dna", renderBrailleDNA},
	{"matrix", renderBrailleMatrix},
	{"heartbeat", renderBrailleHeartbeat},
	{"lightning", renderBrailleLightning},
	{"snake", renderBrailleSnake},
	{"fireworks", renderBrailleFireworks},
	{"drunk", renderBrailleDrunk},
	{"jar", renderBrailleJar},
	{"conga", renderBrailleConga},
	{"queen", renderBrailleQueen},
	{"drip2", renderBrailleDrip},
	{"marathon", renderBrailleMarathon},
}

// braillePainterByName looks up a painter by canonical name. Returns nil
// if unknown.
func braillePainterByName(name string) func(frame, cells int) string {
	for _, p := range brailleNamedPainters {
		if p.name == name {
			return p.fn
		}
	}
	return nil
}

// braillePhases drives the default phased loader. Narrative: single bee
// waking → bees stream out to forage → tight cohesive flock → vortex
// intensifies → cosmic drift on very long waits.
var braillePhases = []func(frame, cells int) string{
	renderBraillePulse,     // 0..80    — chill solo bee
	renderBrailleForage,    // 80..240  — hive stream out
	renderBrailleFlock,     // 240..480 — organized clusters
	renderBrailleVortex,    // 480..960 — swarm intensifies
	renderBrailleStarfield, // 960+     — cosmic drift
}

// braillePhaseFor selects which phase painter applies for the given
// frame. Boundaries are ~9.6s / 28.8s / 57.6s / 115.2s at 120ms ticks.
func braillePhaseFor(frame int) func(frame, cells int) string {
	switch {
	case frame < 80:
		return braillePhases[0]
	case frame < 240:
		return braillePhases[1]
	case frame < 480:
		return braillePhases[2]
	case frame < 960:
		return braillePhases[3]
	default:
		return braillePhases[4]
	}
}
