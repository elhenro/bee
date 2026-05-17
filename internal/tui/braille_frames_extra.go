package tui

import "math"

// extra braille-pattern loader painters — second wave beyond the bee-themed
// set in braille_frames.go. each painter takes a frame counter and canvas
// width in braille cells, returns a single-row braille string.

// renderBrailleDNA — two phase-shifted sine strands weaving over/under each
// other. classic double-helix read; both strands drawn at every column, the
// one in front (lower y) renders solid, the back strand thins out where
// they cross.
func renderBrailleDNA(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	for x := 0; x < w; x++ {
		p := float64(x)*0.42 - float64(frame)*0.35
		a := (math.Sin(p) + 1) * 0.5
		b := (math.Sin(p+math.Pi) + 1) * 0.5
		y1 := int(math.Round(a * float64(braillePxH-1)))
		y2 := int(math.Round(b * float64(braillePxH-1)))
		c.SetPixel(x, y1, true)
		// back strand: skip every other column where strands are closest,
		// gives an over-under braid feel.
		if math.Abs(a-b) > 0.2 || x%2 == 0 {
			c.SetPixel(x, y2, true)
		}
	}
	return c.ToBraille()
}

// renderBrailleMatrix — vertical streams with per-column speed and length
// variance. distinct from rain: longer trails, never-aligned cadence, more
// columns active. reads as data cascading.
func renderBrailleMatrix(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	period := braillePxH + 4
	for x := 0; x < w; x += 2 {
		// per-column speed scalar 1..3, coprime offset so cols never sync.
		spd := 1 + (x*37)%3
		off := (x * 13) % period
		head := ((frame*spd)/2 + off) % period
		trail := 3
		for k := 0; k < trail; k++ {
			y := head - k
			if y >= 0 && y < braillePxH {
				c.SetPixel(x, y, true)
			}
		}
	}
	return c.ToBraille()
}

// renderBrailleHeartbeat — EKG line. flatlines for most of the cycle, then
// fires a sharp QRS spike that travels across the canvas. cycle ~60 frames
// — slow, organic rhythm.
func renderBrailleHeartbeat(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	baseY := braillePxH / 2
	period := 60
	spike := frame % period
	// baseline runs the full width.
	for x := 0; x < w; x++ {
		c.SetPixel(x, baseY, true)
	}
	// spike head position scrolls left→right across cycle.
	headX := (spike * w) / period
	// QRS shape: up, down-deep, up — 5 px around head.
	shape := []struct{ dx, dy int }{
		{-2, 0}, {-1, -1}, {0, -2}, {1, 1}, {2, 0},
	}
	if spike < period/2 {
		for _, s := range shape {
			x := headX + s.dx
			y := baseY + s.dy
			if x >= 0 && x < w && y >= 0 && y < braillePxH {
				c.SetPixel(x, y, true)
			}
		}
	}
	return c.ToBraille()
}

// renderBrailleLightning — sudden vertical bolt with branches, decays then
// re-strikes from a new column. zig-zag path computed from hash of strike
// id so each bolt looks fresh.
func renderBrailleLightning(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	period := 24
	t := frame % period
	strikeID := frame / period
	// bolt only visible first half of cycle, fades by skipping pixels.
	if t >= period/2 {
		return c.ToBraille()
	}
	// pseudo-random base column from strike id.
	baseX := (strikeID*97 + 13) % w
	// trace bolt top→bottom with zig-zag offset per row.
	x := baseX
	for y := 0; y < braillePxH; y++ {
		// fade: in second quarter of visible phase, sparsen.
		visible := t < period/4 || (y+t)%2 == 0
		if visible && x >= 0 && x < w {
			c.SetPixel(x, y, true)
		}
		// zig-zag step; pseudo-random direction per (strike, row).
		dx := ((strikeID*31+y*17)%5 - 2)
		x += dx
		if x < 0 {
			x = 0
		}
		if x >= w {
			x = w - 1
		}
	}
	return c.ToBraille()
}

// renderBrailleSnake — segments following a head along a sine path. body
// length scales with width; tail fades to sparse pixels. eternal chase.
func renderBrailleSnake(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	bodyLen := w / 2
	if bodyLen < 8 {
		bodyLen = 8
	}
	for i := 0; i < bodyLen; i++ {
		t := float64(frame-i) * 0.18
		x := int(math.Round((math.Sin(t)+1)*0.5*float64(w-1))) - 0
		// path of head — phase shifts with i so segments lag behind.
		y := int(math.Round((math.Sin(t*1.5)+1)*0.5*float64(braillePxH-1)))
		if x < 0 || x >= w {
			continue
		}
		// sparse tail past 2/3 of length.
		if i > (bodyLen*2)/3 && i%2 == 0 {
			continue
		}
		c.SetPixel(x, y, true)
	}
	return c.ToBraille()
}

// renderBrailleFireworks — periodic explosions from rotating origin. burst
// expands radially, then sparks "fall" downward as they fade. multiple
// bursts overlap so canvas always has something happening.
func renderBrailleFireworks(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	bursts := 3
	period := 30
	for b := 0; b < bursts; b++ {
		// each burst phase-offset so they don't sync.
		t := (frame + b*period/bursts) % period
		// origin walks across canvas per burst id.
		origID := (frame/period + b) % 5
		ox := (origID*w + w/4) / 5 % w
		oy := braillePxH / 2
		// radius grows then sparks fall.
		phase := float64(t) / float64(period)
		r := phase * float64(w/4)
		// 6 sparks radiating evenly.
		for i := 0; i < 6; i++ {
			a := float64(i) * (2 * math.Pi / 6)
			x := ox + int(math.Round(r*math.Cos(a)))
			// gravity: y drifts down as phase progresses.
			y := oy + int(math.Round(r*0.5*math.Sin(a))) + int(phase*float64(braillePxH/2))
			// fade after 70% of cycle: skip every-other.
			if phase > 0.7 && i%2 == 0 {
				continue
			}
			if x >= 0 && x < w && y >= 0 && y < braillePxH {
				c.SetPixel(x, y, true)
			}
		}
	}
	return c.ToBraille()
}
