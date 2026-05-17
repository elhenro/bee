package tui

import "math"

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

// renderBrailleSwarm — bee swarm scaled to canvas width. Particle count
// grows with canvas size so wide terminals feel populated. Each particle
// rides its own phase-offset sine path; a 1-px trail behind the head
// gives a motion-blur read.
func renderBrailleSwarm(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	// scale particle count with width — roughly one bee per 4 cells.
	n := cells / 4
	if n < 3 {
		n = 3
	}
	for i := 0; i < n; i++ {
		// phase per particle uses a coprime gap so they never sync up.
		t := float64(frame)*0.28 + float64(i)*1.379
		x := int(math.Round((math.Sin(t) + 1) * 0.5 * float64(w-1)))
		y := i % braillePxH
		c.SetPixel(x, y, true)
		// 1-px trail behind the head for motion blur.
		tp := t - 0.28
		xp := int(math.Round((math.Sin(tp) + 1) * 0.5 * float64(w-1)))
		if xp != x {
			c.SetPixel(xp, y, true)
		}
	}
	return c.ToBraille()
}

// renderBrailleCaretSwarm — pocket-sized swarm caret. 3 cells wide
// (6×4 px), 3 bees on phase-offset sine paths in both axes with 1-px
// trails. Stands in for the old static ▍ caret at the tail of a
// streaming partial so reader sees swarm-style motion mid-stream
// instead of a single spinning glyph.
func renderBrailleCaretSwarm(frame int) string {
	const cells = 3
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	for i := 0; i < 3; i++ {
		t := float64(frame)*0.32 + float64(i)*1.879
		x := int(math.Round((math.Sin(t) + 1) * 0.5 * float64(w-1)))
		y := int(math.Round((math.Sin(t*0.7+float64(i)) + 1) * 0.5 * float64(braillePxH-1)))
		c.SetPixel(x, y, true)
		tp := t - 0.32
		xp := int(math.Round((math.Sin(tp) + 1) * 0.5 * float64(w-1)))
		if xp != x {
			c.SetPixel(xp, y, true)
		}
	}
	return c.ToBraille()
}

// renderBrailleWave — two layered sine waves at different phases. The
// faster one carries the rhythm; the slower one gives depth.
func renderBrailleWave(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	for x := 0; x < w; x++ {
		// primary wave — full amplitude, slow phase.
		p1 := float64(x)*0.55 - float64(frame)*0.42
		y1 := int(math.Round((math.Sin(p1) + 1) * 0.5 * float64(braillePxH-1)))
		c.SetPixel(x, y1, true)
		// shadow wave — π/2 offset, half amplitude, fades into background.
		if x%2 == 0 {
			p2 := float64(x)*0.55 - float64(frame)*0.42 + math.Pi/2
			y2 := int(math.Round((math.Sin(p2)+1)*0.5*float64(braillePxH-1)/2)) + 1
			if y2 >= 0 && y2 < braillePxH && y2 != y1 {
				c.SetPixel(x, y2, true)
			}
		}
	}
	return c.ToBraille()
}

// renderBrailleComet — a bright head with a decaying tail looping across
// the canvas. Tail length scales with canvas width.
func renderBrailleComet(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	cycle := w + 8
	head := frame % cycle
	y := braillePxH / 2
	tail := w / 3
	if tail < 6 {
		tail = 6
	}
	for i := 0; i < tail; i++ {
		x := head - i
		if x < 0 || x >= w {
			continue
		}
		// density decay: solid near head, every-other middle, sparse rear.
		switch {
		case i <= 2:
			c.SetPixel(x, y, true)
		case i <= tail/2:
			if (head/2)%2 == 0 {
				c.SetPixel(x, y, true)
			}
		default:
			if (head/3)%2 == 0 {
				c.SetPixel(x, y, true)
			}
		}
	}
	// secondary off-row sparks thicken the head only.
	if head >= 0 && head < w {
		c.SetPixel(head, y-1, true)
	}
	if head-2 >= 0 && head-2 < w {
		c.SetPixel(head-2, y+1, true)
	}
	return c.ToBraille()
}

// renderBrailleHex — six vertices on a wide ellipse, rotating. Reads as a
// stylized hex outline orbiting the prompt.
func renderBrailleHex(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	cx, cy := w/2, braillePxH/2
	r := float64(w/2 - 1)
	if r < 6 {
		r = 6
	}
	rh := 1.5
	for i := 0; i < 6; i++ {
		theta := (float64(i)/6 + float64(frame)/72.0) * 2 * math.Pi
		x := cx + int(math.Round(r*math.Cos(theta)))
		y := cy + int(math.Round(rh*math.Sin(theta)))
		c.SetPixel(x, y, true)
	}
	return c.ToBraille()
}

// renderBrailleRipple — two concentric ellipses expanding outward from
// center, phase-offset so a new ripple starts before the old one fades.
func renderBrailleRipple(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	cx, cy := w/2, braillePxH/2
	maxR := float64(w / 2)
	for k := 0; k < 2; k++ {
		t := float64(frame)*0.35 + float64(k)*math.Pi
		phase := math.Mod(t, 2*math.Pi) / (2 * math.Pi)
		r := phase * maxR
		// trace an ellipse — use enough samples to keep it solid even
		// on very wide canvases.
		samples := int(math.Max(24, r*4))
		for s := 0; s < samples; s++ {
			a := float64(s) / float64(samples) * 2 * math.Pi
			x := cx + int(math.Round(r*math.Cos(a)))
			y := cy + int(math.Round(r/maxR*1.5*math.Sin(a)))
			c.SetPixel(x, y, true)
		}
	}
	return c.ToBraille()
}

// renderBrailleRain — falling drops on every-other column. Each column
// has its own offset so the rain doesn't sync. Density scales with width.
func renderBrailleRain(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	// every 3rd column gets a drop — sparse feels rain-like.
	for x := 1; x < w; x += 3 {
		// coprime offset per column so drops never align.
		off := (x * 7) % (braillePxH + 2)
		head := (frame + off) % (braillePxH + 2)
		if head < braillePxH {
			c.SetPixel(x, head, true)
			if head-1 >= 0 {
				c.SetPixel(x, head-1, true)
			}
		}
	}
	return c.ToBraille()
}

// renderBrailleOrbit — particles on the same elliptical orbit, evenly
// spaced. Particle count scales gently with width so the orbit doesn't
// look anemic on wide terminals.
func renderBrailleOrbit(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	cx, cy := w/2, braillePxH/2
	r := float64(w/2 - 2)
	if r < 6 {
		r = 6
	}
	rh := 1.5
	n := 3 + cells/8
	if n > 8 {
		n = 8
	}
	t := float64(frame) * 0.22
	for i := 0; i < n; i++ {
		a := t + float64(i)*(2*math.Pi/float64(n))
		x := cx + int(math.Round(r*math.Cos(a)))
		y := cy + int(math.Round(rh*math.Sin(a)))
		c.SetPixel(x, y, true)
		// 1-step trail
		ap := a - 0.22
		xp := cx + int(math.Round(r*math.Cos(ap)))
		yp := cy + int(math.Round(rh*math.Sin(ap)))
		if xp != x || yp != y {
			c.SetPixel(xp, yp, true)
		}
	}
	return c.ToBraille()
}

// renderBrailleBreath — single horizontal bar expanding/contracting from
// center on a slow cosine. Reads as breathing. Scales to canvas width.
func renderBrailleBreath(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	cx := w / 2
	t := float64(frame) * 0.18
	maxW := w - 4
	if maxW < 2 {
		maxW = 2
	}
	bar := int(math.Round((1-math.Cos(t))/2*float64(maxW))) + 2
	y := braillePxH / 2
	for x := cx - bar/2; x <= cx+bar/2; x++ {
		c.SetPixel(x, y, true)
	}
	// inner glow — half-length on rows above/below
	wInner := bar / 2
	for x := cx - wInner/2; x <= cx+wInner/2; x++ {
		c.SetPixel(x, y-1, true)
		c.SetPixel(x, y+1, true)
	}
	return c.ToBraille()
}

// renderBrailleForage — bees stream out of left-edge hive, drift right
// with sine wander, fade past right edge. Particles wrap so the stream
// stays populated. Reads as foraging bees leaving and returning.
func renderBrailleForage(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	n := cells / 3
	if n < 4 {
		n = 4
	}
	cycle := w + 16
	for i := 0; i < n; i++ {
		// stagger so bees emerge one after another, not in a wall.
		off := (i * cycle) / n
		x := (frame + off) % cycle
		// sine wander on the vertical, phase-shifted per bee.
		t := float64(frame)*0.2 + float64(i)*0.9
		y := int(math.Round((math.Sin(t)+1)*0.5*float64(braillePxH-1)))
		c.SetPixel(x, y, true)
		// short trail toward the hive.
		if x-1 >= 0 {
			c.SetPixel(x-1, y, true)
		}
	}
	// hive marker — solid block at left edge, always lit.
	for y := 0; y < braillePxH; y++ {
		c.SetPixel(0, y, true)
	}
	return c.ToBraille()
}

// renderBrailleFigure8 — waggle-dance lemniscate. Three bees evenly phased
// along the figure-8 path. This is the actual pattern honeybees use to
// encode flower direction and distance back at the hive.
func renderBrailleFigure8(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	cx, cy := w/2, braillePxH/2
	r := float64(w/2 - 2)
	if r < 6 {
		r = 6
	}
	rh := 1.5
	n := 3
	t := float64(frame) * 0.15
	for i := 0; i < n; i++ {
		tp := t + float64(i)*(2*math.Pi/float64(n))
		// lemniscate: x = r·sin(2t), y = rh·sin(t)·cos(t) (8x denser vertical).
		x := cx + int(math.Round(r*math.Sin(2*tp)))
		y := cy + int(math.Round(rh*math.Sin(tp)*math.Cos(tp)*2))
		c.SetPixel(x, y, true)
		// 1-step trail
		tp2 := tp - 0.15
		x2 := cx + int(math.Round(r*math.Sin(2*tp2)))
		y2 := cy + int(math.Round(rh*math.Sin(tp2)*math.Cos(tp2)*2))
		if x2 != x || y2 != y {
			c.SetPixel(x2, y2, true)
		}
	}
	return c.ToBraille()
}

// renderBrailleVortex — three nested rings of bees rotating around the
// center, all the same direction. Tight, fast read — like a hive being
// defended or a queen flight.
func renderBrailleVortex(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	cx, cy := w/2, braillePxH/2
	maxR := float64(w/2 - 1)
	if maxR < 6 {
		maxR = 6
	}
	rings := []struct {
		rx, ry float64
		n      int
		speed  float64
	}{
		{maxR, 1.5, 6, 0.18},        // outer wide ring
		{maxR * 0.6, 1.0, 4, 0.30},  // middle
		{maxR * 0.25, 0.5, 3, 0.45}, // tight inner
	}
	for _, ring := range rings {
		t := float64(frame) * ring.speed
		for i := 0; i < ring.n; i++ {
			a := t + float64(i)*(2*math.Pi/float64(ring.n))
			x := cx + int(math.Round(ring.rx*math.Cos(a)))
			y := cy + int(math.Round(ring.ry*math.Sin(a)))
			c.SetPixel(x, y, true)
		}
	}
	return c.ToBraille()
}

// renderBrailleGust — swarm caught in wind. Base sinusoidal motion + a
// global lateral bias that breathes; every ~40 frames a gust spike
// shoves the whole swarm rightward briefly. Looks like bees fighting wind.
func renderBrailleGust(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	n := cells / 4
	if n < 3 {
		n = 3
	}
	// slow lateral breath — bees lean into wind.
	bias := math.Sin(float64(frame)*0.04) * float64(w) * 0.15
	// gust spike: every 40 frames, half-period gust kicks for 8 frames.
	if g := frame % 40; g < 8 {
		bias += math.Sin(float64(g)*math.Pi/8) * float64(w) * 0.25
	}
	for i := 0; i < n; i++ {
		t := float64(frame)*0.28 + float64(i)*1.379
		base := (math.Sin(t) + 1) * 0.5 * float64(w-1)
		x := int(math.Round(base + bias))
		if x < 0 {
			x = 0
		}
		if x >= w {
			x = w - 1
		}
		y := i % braillePxH
		c.SetPixel(x, y, true)
	}
	return c.ToBraille()
}

// renderBrailleScatter — alarm response. Bees explode radially outward
// from center, drift, then regroup. Cycle: explode → drift → regroup →
// repeat. Reads as a predator scare.
func renderBrailleScatter(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	cx, cy := w/2, braillePxH/2
	maxR := float64(w / 2)
	n := cells / 3
	if n < 6 {
		n = 6
	}
	// phase 0..1 — slow cosine cycle: 0 = grouped, 1 = fully scattered.
	phase := (1 - math.Cos(float64(frame)*0.07)) / 2
	r := phase * maxR
	for i := 0; i < n; i++ {
		// fixed angle per bee — they always scatter in their own direction.
		a := float64(i) * (2 * math.Pi / float64(n))
		x := cx + int(math.Round(r*math.Cos(a)))
		y := cy + int(math.Round(r/maxR*1.5*math.Sin(a)))
		c.SetPixel(x, y, true)
	}
	return c.ToBraille()
}

// renderBrailleFlock — 3 cohesive sub-groups of bees flying together.
// Each cluster has its own center motion; bees inside the cluster jitter
// around the center. Boids-lite — cohesion + alignment without separation.
func renderBrailleFlock(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	clusters := 3
	perCluster := 3
	for k := 0; k < clusters; k++ {
		// cluster center moves on its own sine, phase-offset per cluster.
		t := float64(frame)*0.22 + float64(k)*2.094
		cx := int(math.Round((math.Sin(t) + 1) * 0.5 * float64(w-1)))
		cy := (k * braillePxH) / clusters
		for i := 0; i < perCluster; i++ {
			// per-bee offset oscillates so the cluster doesn't look static.
			ot := t*1.3 + float64(i)*1.7
			dx := int(math.Round(math.Sin(ot) * 1.5))
			dy := int(math.Round(math.Cos(ot)))
			c.SetPixel(cx+dx, cy+dy, true)
		}
	}
	return c.ToBraille()
}

// renderBrailleStarfield — pseudorandom blinking points moving slowly
// rightward. Reads as drifting stars for the cosmic/long-wait phase.
func renderBrailleStarfield(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	// scatter ~cells/2 stars; LCG-style hash for stable positions per index.
	n := cells / 2
	for i := 0; i < n; i++ {
		// position drifts slowly rightward; wraps.
		base := (i*73 + frame/3) % w
		y := (i * 31) % braillePxH
		// blink — only render when (frame + idx_hash) bit set.
		if ((frame+i*17)/4)%3 != 0 {
			c.SetPixel(base, y, true)
		}
	}
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
