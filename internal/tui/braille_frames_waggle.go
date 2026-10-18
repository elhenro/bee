package tui

import "math"

// renderBrailleWaggleRun — the directed dance. Every bee runs left→right
// at the same speed with the rapid side-to-side waggle wobble, so the
// canvas reads as one message: "go this way, progress is happening".
// Unlike the swarm painters nothing drifts backward — direction is the
// signal. Bees wrap right→left so the run never stops.
func renderBrailleWaggleRun(frame, cells int) string {
	cells = clampCells(cells)
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	n := cells / 4
	if n < 3 {
		n = 3
	}
	cycle := w + 6 // gap past the right edge before re-entering left
	for i := 0; i < n; i++ {
		x := (frame*3 + i*(cycle/n)) % cycle
		if x >= w {
			continue // in the wrap gap
		}
		// waggle wobble: fast vertical shake around the mid lane, phase
		//-offset per bee so the line ripples instead of lockstepping.
		y := braillePxH/2 + int(math.Round(math.Sin(float64(frame)*0.9+float64(i)*1.7)*1.5))
		if y < 0 {
			y = 0
		}
		if y >= braillePxH {
			y = braillePxH - 1
		}
		c.SetPixel(x, y, true)
		if x-1 >= 0 {
			c.SetPixel(x-1, y, true) // 1-px motion trail
		}
	}
	return c.ToBraille()
}
