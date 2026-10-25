package tui

import "math"

// /compact painters: swarm variants that gather memory into a summary.

// renderBrailleCompactConverge — /compact variant 1. Swarm streams in
// from both edges toward center while wandering vertically on a sine,
// then breathes back out. Reuses the multi-particle phase-offset sine
// technique from renderBrailleSwarm. Reads as memory chunks being
// gathered to be folded into a summary.
func renderBrailleCompactConverge(frame, cells int) string {
	cells = clampCells(cells)
	c, w := newLoaderCanvas(cells)
	cx := w / 2
	n := max(cells/3, 5)
	// phase 0..1..0 — 0 = converged at center, 1 = back at edges.
	phase := (1 - math.Cos(float64(frame)*0.06)) / 2
	for i := 0; i < n; i++ {
		side := 1.0
		if i%2 == 1 {
			side = -1.0
		}
		// rest jitter so converged bees don't collapse onto one pixel.
		rest := float64((i%3)-1) * 1.5
		edge := side * float64(cx-1)
		x := int(math.Round(float64(cx) + rest + phase*(edge-rest)))
		x = min(max(x, 0), w-1)
		// vertical wander — phase-offset sine, coprime gap per bee.
		t := float64(frame)*0.28 + float64(i)*1.379
		y := int(math.Round((math.Sin(t) + 1) * 0.5 * float64(braillePxH-1)))
		c.SetPixel(x, y, true)
		// trail one px toward center for motion-blur read.
		dx := -1
		if side < 0 {
			dx = 1
		}
		if phase > 0.05 {
			tx := x + dx
			c.SetPixel(tx, y, true)
		}
	}
	// center anchor — always lit so the bar never goes fully dark.
	c.SetPixel(cx, braillePxH/2, true)
	return c.ToBraille()
}

// renderBrailleCompactVortex — /compact variant 2. Bees orbit center on
// shrinking elliptical paths, collapsing inward to a tight cluster then
// flaring back out. Layered radii (sin-perturbed per bee) avoid the bees
// looking like a single ring. Vertical squashed since braille canvas is
// only 4 px tall.
func renderBrailleCompactVortex(frame, cells int) string {
	cells = clampCells(cells)
	c, w := newLoaderCanvas(cells)
	cx, cy := w/2, braillePxH/2
	maxR := max(float64(cx-1), 4)
	n := max(cells/2, 6)
	// radial breath: 1 = scattered to maxR, 0 = pinned at center.
	phase := (1 - math.Cos(float64(frame)*0.05)) / 2
	for i := 0; i < n; i++ {
		a := float64(frame)*0.20 + float64(i)*(2*math.Pi/float64(n))
		// per-bee radius modulation so they don't look like one ring.
		layer := 0.6 + 0.4*math.Sin(float64(i)*1.7)
		r := phase * maxR * layer
		x := cx + int(math.Round(r*math.Cos(a)))
		// y compressed (×0.35) — braille is 8x denser horizontally than vertically.
		y := cy + int(math.Round(r*0.35*math.Sin(a)))
		if x < 0 || x >= w {
			continue
		}
		c.SetPixel(x, y, true)
		// short trail along the orbit tangent for swirl read.
		ap := a - 0.20
		xp := cx + int(math.Round(r*math.Cos(ap)))
		yp := cy + int(math.Round(r*0.35*math.Sin(ap)))
		if xp >= 0 && xp < w && (xp != x || yp != y) {
			c.SetPixel(xp, yp, true)
		}
	}
	// pulsing center marker — destination of the compression.
	c.SetPixel(cx, cy, true)
	return c.ToBraille()
}

// renderBrailleCompactFunnel — /compact variant 3. Bees evenly spread
// across the full width get pulled toward the center column, like
// memory pages funneling into a digest. Density visibly increases near
// the center as phase rises. Vertical sine wander keeps each bee alive
// during the squeeze.
func renderBrailleCompactFunnel(frame, cells int) string {
	cells = clampCells(cells)
	c, w := newLoaderCanvas(cells)
	cx := w / 2
	n := max(cells, 8)
	// 0 = scattered across full width, 1 = collapsed onto center column.
	phase := (1 - math.Cos(float64(frame)*0.045)) / 2
	for i := 0; i < n; i++ {
		home := int(math.Round(float64(i) * float64(w-1) / float64(n-1)))
		x := home + int(math.Round(phase*float64(cx-home)))
		x = min(max(x, 0), w-1)
		t := float64(frame)*0.22 + float64(i)*0.78
		y := int(math.Round((math.Sin(t) + 1) * 0.5 * float64(braillePxH-1)))
		c.SetPixel(x, y, true)
	}
	// once collapsed past ~70%, paint accumulating column at center so
	// the digest read is unambiguous.
	if phase > 0.7 {
		for y := 0; y < braillePxH; y++ {
			c.SetPixel(cx, y, true)
		}
	}
	return c.ToBraille()
}
