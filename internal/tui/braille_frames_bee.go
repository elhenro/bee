package tui

import "math"

// bee-themed comedy painters. mostly silly scenarios — fermented-honey
// drunk bee, bee trapped in a jar, conga line, queen procession, honey
// drip, marathon finish.

// renderBrailleDrunk — single bee with wobble + overshoot. drank too much
// fermented nectar; can't fly straight. erratic horizontal motion with
// random direction reversal driven by a stuttering hash. trail shows the
// stagger path.
func renderBrailleDrunk(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	// base drift very slow, but stutter shoves it around.
	t := float64(frame) * 0.06
	stutter := math.Sin(float64(frame)*0.41) + math.Sin(float64(frame)*0.83)*0.6
	x := int(math.Round((math.Sin(t)+1)*0.5*float64(w-1) + stutter*float64(w)*0.12))
	if x < 0 {
		x = 0
	}
	if x >= w {
		x = w - 1
	}
	// vertical wobble — drunk bee can't hold altitude either.
	y := int(math.Round((math.Sin(t*3)+1)*0.5*float64(braillePxH-1)))
	c.SetPixel(x, y, true)
	// stagger trail: 3 prior positions, sparser the further back.
	for k := 1; k <= 3; k++ {
		tp := t - float64(k)*0.06
		sp := math.Sin(float64(frame-k)*0.41) + math.Sin(float64(frame-k)*0.83)*0.6
		xp := int(math.Round((math.Sin(tp)+1)*0.5*float64(w-1) + sp*float64(w)*0.12))
		yp := int(math.Round((math.Sin(tp*3) + 1) * 0.5 * float64(braillePxH-1)))
		if xp >= 0 && xp < w && (k == 1 || k%2 == 0) {
			c.SetPixel(xp, yp, true)
		}
	}
	return c.ToBraille()
}

// renderBrailleJar — bee trapped in a glass jar. walls drawn around the
// canvas; bee has velocity, bounces off each wall. perfect "stuck waiting"
// gag.
func renderBrailleJar(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	// jar walls: top, bottom, left, right edges as dotted lines.
	for x := 0; x < w; x += 2 {
		c.SetPixel(x, 0, true)
		c.SetPixel(x, braillePxH-1, true)
	}
	for y := 0; y < braillePxH; y++ {
		c.SetPixel(0, y, true)
		c.SetPixel(w-1, y, true)
	}
	// interior bounds: bee bounces between (1..w-2, 1..h-2).
	innerW := w - 2
	innerH := braillePxH - 2
	if innerW < 2 || innerH < 1 {
		return c.ToBraille()
	}
	// x velocity ~3 px/frame so it actually bounces fast.
	periodX := 2 * (innerW - 1)
	if periodX < 2 {
		periodX = 2
	}
	periodY := 2 * (innerH)
	if periodY < 2 {
		periodY = 2
	}
	px := (frame * 3) % periodX
	if px >= innerW {
		px = periodX - px
	}
	py := frame % periodY
	if py >= innerH {
		py = periodY - py
	}
	c.SetPixel(1+px, 1+py, true)
	return c.ToBraille()
}

// renderBrailleConga — bee conga line. evenly-spaced segments along a
// sine path, all marching to the right. they leave one side and re-enter
// the other so the party never stops.
func renderBrailleConga(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	n := cells / 4
	if n < 4 {
		n = 4
	}
	gap := 6
	for i := 0; i < n; i++ {
		x := ((frame*2)+i*gap)%(w+gap) - gap/2
		if x < 0 || x >= w {
			continue
		}
		// shared wavy path so the whole line undulates together.
		t := float64(x)*0.18 - float64(frame)*0.15
		y := int(math.Round((math.Sin(t)+1)*0.5*float64(braillePxH-1)))
		c.SetPixel(x, y, true)
	}
	return c.ToBraille()
}

// renderBrailleQueen — queen bee centered (2×2 sprite), 4 attendants
// orbit at fixed radius. attendants form a court that always faces her.
var brailleQueen = []byte{
	1, 1,
	1, 1,
}

func renderBrailleQueen(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	cx, cy := w/2, braillePxH/2
	c.DrawSprite(brailleQueen, 2, 2, cx-1, cy-1)
	// 4 attendants on rotating cardinal-points orbit.
	r := float64(w/2 - 2)
	if r < 4 {
		r = 4
	}
	t := float64(frame) * 0.16
	for i := 0; i < 4; i++ {
		a := t + float64(i)*(math.Pi/2)
		x := cx + int(math.Round(r*math.Cos(a)))
		y := cy + int(math.Round(1.4*math.Sin(a)))
		c.SetPixel(x, y, true)
	}
	return c.ToBraille()
}

// renderBrailleDrip — single fat honey droplet falls slowly from top,
// hits the pool at the bottom, ripples briefly, then a new drop forms.
// pool grows as drops accumulate, then resets when full. very slow tempo.
func renderBrailleDrip(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	// pool at bottom row, width grows with drop count then resets.
	dropPeriod := 24
	dropIdx := frame / dropPeriod
	poolW := (dropIdx % 8) * (w / 10)
	if poolW > w {
		poolW = w
	}
	for x := w/2 - poolW/2; x <= w/2+poolW/2 && x < w; x++ {
		if x >= 0 {
			c.SetPixel(x, braillePxH-1, true)
		}
	}
	// drop position: falls top→bottom-1 over drop period.
	t := frame % dropPeriod
	dropY := (t * (braillePxH - 1)) / dropPeriod
	cx := w / 2
	c.SetPixel(cx, dropY, true)
	// a slight pendant tail above head for the first half of fall.
	if t < dropPeriod/2 && dropY-1 >= 0 {
		c.SetPixel(cx, dropY-1, true)
	}
	// splash on impact frame.
	if dropY == braillePxH-2 {
		if cx-2 >= 0 {
			c.SetPixel(cx-2, braillePxH-2, true)
		}
		if cx+2 < w {
			c.SetPixel(cx+2, braillePxH-2, true)
		}
	}
	return c.ToBraille()
}

// renderBrailleMarathon — bees racing toward finish line at right edge.
// finish line is a dashed vertical at w-3. bees cross one by one, then
// pop back at the start (lap mode). reads as a sports event.
func renderBrailleMarathon(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	// dashed finish line near right edge.
	finishX := w - 3
	if finishX < 4 {
		finishX = w - 1
	}
	for y := 0; y < braillePxH; y += 2 {
		c.SetPixel(finishX, y, true)
	}
	// 4 lanes, one bee each, different speeds so they stagger across the
	// finish.
	lanes := braillePxH
	speeds := []int{2, 3, 2, 4}
	offsets := []int{0, 6, 12, 18}
	cycle := w + 6
	for lane := 0; lane < lanes && lane < len(speeds); lane++ {
		x := (frame*speeds[lane] + offsets[lane]) % cycle
		if x < 0 || x >= w {
			continue
		}
		c.SetPixel(x, lane, true)
		// trail one step back.
		xp := x - speeds[lane]
		if xp >= 0 && xp < w {
			c.SetPixel(xp, lane, true)
		}
	}
	return c.ToBraille()
}
