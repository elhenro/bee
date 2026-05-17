package tui

import "math"

// ambient/abstract painters. cohort that reads as motion-but-not-bees —
// waves, comets, orbits, ripples, rain, starfield. used for hold/idle
// phases and the longer wait tail.

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
