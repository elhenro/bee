package tui

import (
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// LoaderStyle selects which braille animation runs while waiting on the
// first token. Default is the phased pipeline (chill → swarm → orbit →
// ripple → starfield) which evolves the longer the wait runs. Named
// values pin a single painter.
type LoaderStyle int

const (
	LoaderStyleDefault   LoaderStyle = iota // phased: pulse → swarm → orbit → ripple → starfield
	LoaderStylePulse                        // centered bee, gentle wing flap
	LoaderStyleSwarm                        // multi-particle swarm, scales with width
	LoaderStyleWave                         // layered sine waves
	LoaderStyleComet                        // bright head + decaying tail
	LoaderStyleHex                          // rotating hexagonal outline
	LoaderStyleRipple                       // concentric ellipses expanding
	LoaderStyleRain                         // falling drops
	LoaderStyleOrbit                        // particles on elliptical orbit
	LoaderStyleBreath                       // bar expanding/contracting from center
	LoaderStyleStars                        // drifting starfield
	LoaderStyleForage                       // bees leave hive, drift, return
	LoaderStyleFigure8                      // waggle-dance lemniscate
	LoaderStyleVortex                       // 3 nested rotating rings
	LoaderStyleGust                         // swarm in wind with gust spikes
	LoaderStyleScatter                      // alarm dispersal + regroup
	LoaderStyleFlock                        // 3 cohesive bee clusters
	LoaderStyleDNA                          // double helix
	LoaderStyleMatrix                       // variable-speed vertical streams
	LoaderStyleHeartbeat                    // EKG flatline + spike
	LoaderStyleLightning                    // sudden bolt + decay
	LoaderStyleSnake                        // segments chasing head
	LoaderStyleFireworks                    // radiating bursts with gravity
	LoaderStyleDrunk                        // bee wobbles, drank fermented honey
	LoaderStyleJar                          // bee trapped, bouncing off jar walls
	LoaderStyleConga                        // bee conga line, undulating march
	LoaderStyleQueen                        // queen + 4 attendants procession
	LoaderStyleDrip                         // fat honey droplet + accumulating pool
	LoaderStyleMarathon                     // bees racing toward finish line
)

// ParseLoaderStyle maps BEE_LOADER values to a style. Unset / "random" /
// unknown → phased default. Legacy aliases (swarm/comb/dance/drip) map
// to their nearest braille equivalent so old configs keep working.
func ParseLoaderStyle(s string) LoaderStyle {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "default", "phased", "auto":
		return LoaderStyleDefault
	case "swarm", "bee", "bees":
		return LoaderStyleSwarm
	case "pulse", "bee-flap":
		return LoaderStylePulse
	case "wave", "comb": // comb is a legacy alias from the old ASCII era
		return LoaderStyleWave
	case "comet", "trail":
		return LoaderStyleComet
	case "hex", "hexagon":
		return LoaderStyleHex
	case "ripple", "rings":
		return LoaderStyleRipple
	case "rain", "drip", "honey": // drip/honey are legacy aliases
		return LoaderStyleRain
	case "orbit", "dance", "waggle": // dance/waggle are legacy aliases
		return LoaderStyleOrbit
	case "breath", "breathe":
		return LoaderStyleBreath
	case "stars", "starfield", "cosmic":
		return LoaderStyleStars
	case "forage", "hive", "foraging":
		return LoaderStyleForage
	case "figure8", "fig8", "lemniscate":
		return LoaderStyleFigure8
	case "vortex", "spin", "tornado":
		return LoaderStyleVortex
	case "gust", "wind", "breeze":
		return LoaderStyleGust
	case "scatter", "alarm", "disperse":
		return LoaderStyleScatter
	case "flock", "cluster", "boids":
		return LoaderStyleFlock
	case "dna", "helix", "double-helix":
		return LoaderStyleDNA
	case "matrix", "cascade", "rain2":
		return LoaderStyleMatrix
	case "heartbeat", "ekg", "pulse-line":
		return LoaderStyleHeartbeat
	case "lightning", "bolt", "strike":
		return LoaderStyleLightning
	case "snake", "serpent", "chase":
		return LoaderStyleSnake
	case "fireworks", "burst", "explosion":
		return LoaderStyleFireworks
	case "drunk", "tipsy", "fermented":
		return LoaderStyleDrunk
	case "jar", "trapped", "stuck":
		return LoaderStyleJar
	case "conga", "line", "party":
		return LoaderStyleConga
	case "queen", "royal", "procession":
		return LoaderStyleQueen
	case "drip2", "droplet", "honey-drop":
		return LoaderStyleDrip
	case "marathon", "race", "finish":
		return LoaderStyleMarathon
	case "random", "rand", "?":
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		return LoaderStyle(1 + r.Intn(len(brailleNamedPainters)))
	default:
		return LoaderStyleDefault
	}
}

// envLoaderStyle reads BEE_LOADER lazily so tests can override per-call by
// setting the env var before NewStreamRenderer. Indirected for clarity.
func envLoaderStyle() string { return os.Getenv("BEE_LOADER") }

// animatedCaret renders a tiny 3-cell bee swarm at the tail of a
// streaming partial. Replaces the old static ▍ caret: when the model
// paused between deltas (mid-text reasoning, slow tool-call emission,
// network gap) the static caret gave no signal that work was still in
// flight — looked stuck. loaderFrame keeps ticking at loaderTickInterval
// (~120ms) for the full duration of StateStreaming, so the swarm drifts
// even when no new deltas arrive. Same swarm shape as the
// LoaderStyleSwarm full-width loader, just shrunk to caret size.
// pulseStyle alternates color so motion is visible on dim terminals.
func (r *StreamRenderer) animatedCaret(frame int) string {
	nf := frame
	if nf < 0 {
		nf = -nf
	}
	return r.pulseStyle(nf).Render(renderBrailleCaretSwarm(nf))
}

// pulseStyle picks the loader color for this frame — alternates between
// accent and dim every 6 ticks so the art breathes.
func (r *StreamRenderer) pulseStyle(nf int) lipgloss.Style {
	if (nf/6)%2 == 0 {
		return r.styles.RoleBee
	}
	return lipgloss.NewStyle().Foreground(fgSquid)
}

// loaderCells returns the canvas width in braille cells. The loader sits
// after a leading glyph + space (and a 1-col outer gutter in clean mode),
// so we deduct those before clamping. Tiny terminals fall back to the
// minimum cell count.
func (r *StreamRenderer) loaderCells() int {
	prefix := 3 // glyph + 2 spaces
	if !r.compact {
		prefix++ // outer gutter
	}
	cells := r.width - prefix
	if cells < brailleLoaderMinCells {
		cells = brailleLoaderMinCells
	}
	return cells
}

// renderLoader draws the streaming-wait animation. Every style is a
// braille painter; the default style is a phased painter that escalates
// over time. Output is always single-row, ~r.width chars wide.
func (r *StreamRenderer) renderLoader(frame int) string {
	nf := frame
	if nf < 0 {
		nf = -nf
	}
	cells := r.loaderCells()
	painter := r.painterFor(nf)
	art := painter(nf, cells)
	return r.pulseStyle(nf).Render(art)
}

// painterFor resolves the active loader style to a braille painter. The
// default style returns the phase-appropriate painter for the current
// frame; named styles return their pinned painter.
func (r *StreamRenderer) painterFor(nf int) func(frame, cells int) string {
	switch r.loaderStyle {
	case LoaderStylePulse:
		return renderBraillePulse
	case LoaderStyleSwarm:
		return renderBrailleSwarm
	case LoaderStyleWave:
		return renderBrailleWave
	case LoaderStyleComet:
		return renderBrailleComet
	case LoaderStyleHex:
		return renderBrailleHex
	case LoaderStyleRipple:
		return renderBrailleRipple
	case LoaderStyleRain:
		return renderBrailleRain
	case LoaderStyleOrbit:
		return renderBrailleOrbit
	case LoaderStyleBreath:
		return renderBrailleBreath
	case LoaderStyleStars:
		return renderBrailleStarfield
	case LoaderStyleForage:
		return renderBrailleForage
	case LoaderStyleFigure8:
		return renderBrailleFigure8
	case LoaderStyleVortex:
		return renderBrailleVortex
	case LoaderStyleGust:
		return renderBrailleGust
	case LoaderStyleScatter:
		return renderBrailleScatter
	case LoaderStyleFlock:
		return renderBrailleFlock
	case LoaderStyleDNA:
		return renderBrailleDNA
	case LoaderStyleMatrix:
		return renderBrailleMatrix
	case LoaderStyleHeartbeat:
		return renderBrailleHeartbeat
	case LoaderStyleLightning:
		return renderBrailleLightning
	case LoaderStyleSnake:
		return renderBrailleSnake
	case LoaderStyleFireworks:
		return renderBrailleFireworks
	case LoaderStyleDrunk:
		return renderBrailleDrunk
	case LoaderStyleJar:
		return renderBrailleJar
	case LoaderStyleConga:
		return renderBrailleConga
	case LoaderStyleQueen:
		return renderBrailleQueen
	case LoaderStyleDrip:
		return renderBrailleDrip
	case LoaderStyleMarathon:
		return renderBrailleMarathon
	default:
		return braillePhaseFor(nf)
	}
}

// RenderCompacting draws the /compact-specific loader: a braille bar
// that folds inward from both edges to the center, then bounces back.
// Reads as memory being squeezed into a summary.
func (r *StreamRenderer) RenderCompacting(frame int) string {
	nf := frame
	if nf < 0 {
		nf = -nf
	}
	cells := r.loaderCells()
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	// triangle wave: edges → center → edges over 2*half frames.
	half := w / 2
	if half < 4 {
		half = 4
	}
	step := (nf / 2) % (half * 2)
	gap := step
	if gap > half {
		gap = half*2 - step
	}
	// fill from each edge inward up to (half - gap) pixels.
	fill := half - gap
	if fill < 0 {
		fill = 0
	}
	y := braillePxH / 2
	for x := 0; x < fill; x++ {
		c.SetPixel(x, y, true)
		c.SetPixel(w-1-x, y, true)
		// inner glow rows for thickness
		if x < fill-1 {
			c.SetPixel(x, y-1, true)
			c.SetPixel(w-1-x, y+1, true)
		}
	}
	// single bright center dot — always lit so the bar never goes dark.
	c.SetPixel(half, y, true)
	body := r.styles.RoleBee.Render("⬢") + " " + r.pulseStyle(nf).Render(c.ToBraille())
	if r.compact {
		return body
	}
	return outerGutter + body
}
