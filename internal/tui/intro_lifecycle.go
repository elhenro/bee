package tui

import "math"

// lifecycleFrames plays a short egg → larva → pupa → emerge → flight story.
// Total ~28 frames * 70ms ≈ 2s.
func lifecycleFrames(cw, h int) []IntroFrame {
	var frames []IntroFrame
	add := func(n int, sub string, mk func(i, total int) *DrawilleCanvas) {
		for i := 0; i < n; i++ {
			c := mk(i, n)
			frames = append(frames, IntroFrame{Text: c.ToBraille(), Subtitle: sub})
		}
	}
	add(10, "egg", func(i, n int) *DrawilleCanvas { return makeEggFrame(cw, h, i, n) })
	add(12, "larva", func(i, n int) *DrawilleCanvas { return makeLarvaFrame(cw, h, i, n) })
	add(10, "pupa", func(i, n int) *DrawilleCanvas { return makePupaFrame(cw, h, i, n) })
	add(8, "emerge", func(i, n int) *DrawilleCanvas { return makeEmergeFrame(cw, h, i, n) })
	add(14, "flight", func(i, n int) *DrawilleCanvas { return makeFlightFrame(cw, h, i, n) })
	return frames
}

func makeEggFrame(cw, h, idx, total int) *DrawilleCanvas {
	c := NewDrawilleCanvas(cw, h)
	t := float64(idx) / float64(total)
	cx := cw / 2
	rx := int(4 + t*8)
	ry := int(6 + t*4)
	for a := 0; a < 20; a++ {
		ang := float64(a) / 20.0 * 2 * math.Pi
		x := cx + int(float64(rx)*math.Cos(ang))
		y := h/2 + int(float64(ry)*math.Sin(ang))
		if x >= 0 && x < cw && y >= 0 && y < h {
			c.SetPixel(x, y, true)
		}
	}
	return c
}

func makeLarvaFrame(cw, h, idx, total int) *DrawilleCanvas {
	_ = total
	c := NewDrawilleCanvas(cw, h)
	for s := 0; s < 5; s++ {
		pt := float64(idx-s*3) * 0.5
		x := int((math.Sin(pt*0.3)+1)*0.5*float64(cw-4)) + 2
		amp := float64(s) / 5.0 * 0.5
		y := h/2 + int(math.Sin(pt*2)*amp*float64(h))
		if x >= 0 && x < cw && y >= 0 && y < h {
			c.SetPixel(x, y, true)
		}
	}
	return c
}

func makePupaFrame(cw, h, idx, total int) *DrawilleCanvas {
	t := float64(idx) / float64(total)
	c := NewDrawilleCanvas(cw, h)
	cx := cw / 2
	rx := int(10 - t*2)
	ry := int(7 - t*1.5)
	if rx < 3 {
		rx = 3
	}
	if ry < 3 {
		ry = 3
	}
	for a := 0; a < 24; a++ {
		ang := float64(a) / 24.0 * 2 * math.Pi
		x := cx + int(float64(rx)*math.Cos(ang))
		y := h/2 + int(float64(ry)*math.Sin(ang))
		if x >= 0 && x < cw && y >= 0 && y < h {
			c.SetPixel(x, y, true)
		}
	}
	return c
}

func makeEmergeFrame(cw, h, idx, total int) *DrawilleCanvas {
	t := float64(idx) / float64(total)
	c := NewDrawilleCanvas(cw, h)
	cx := cw / 2
	for a := 0; a < 24; a++ {
		if t > 0.3 && a > 8 && a < 16 {
			continue
		}
		ang := float64(a) / 24.0 * 2 * math.Pi
		rx := int(8 * (1 + t))
		ry := int(5 * (1 - t*0.3))
		x := cx + int(float64(rx)*math.Cos(ang))
		y := h/2 + int(float64(ry)*math.Sin(ang))
		if x >= 0 && x < cw && y >= 0 && y < h {
			c.SetPixel(x, y, true)
		}
	}
	c.SetPixel(cx, h/2, true)
	return c
}

// makeFlightFrame: bee enters from left, settles in middle and hovers.
func makeFlightFrame(cw, h, idx, total int) *DrawilleCanvas {
	c := NewDrawilleCanvas(cw, h)
	t := float64(idx) / float64(total)
	cx := cw / 2
	// ease into center over first 60%, then bob
	var bx int
	if t < 0.6 {
		bx = int(-4 + (t/0.6)*float64(cx-(-4)))
	} else {
		bob := math.Sin((t-0.6)*8) * 2
		bx = cx + int(bob)
	}
	y := h/2 + int(math.Sin(float64(idx)*0.6)*float64(h/4))
	sprite := []byte{0, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1, 1, 0, 1, 1, 0}
	c.DrawSprite(sprite, 4, 4, bx, y-2)
	// wing flap on alternate frames
	if (idx/2)%2 == 0 {
		for dx := -2; dx <= -1; dx++ {
			if bx+dx >= 0 && bx+dx < cw {
				c.SetPixel(bx+dx, y, true)
			}
		}
		for dx := 4; dx <= 5; dx++ {
			if bx+dx >= 0 && bx+dx < cw {
				c.SetPixel(bx+dx, y, true)
			}
		}
	}
	return c
}
