package tui

import "math"

// swarm/bee-motion painters. cohort that reads as live bees in flight —
// scattering, foraging, dancing, regrouping.

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
