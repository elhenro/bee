package tui

import "math"

// danceFrames draws a waggle-dance figure-8 trail.
func danceFrames(cw, h, count int) []IntroFrame {
	out := make([]IntroFrame, 0, count)
	cx := cw / 2
	cy := h / 2
	rx := float64(cw)/3 - 1
	ry := float64(h)/2 - 1
	if rx < 6 {
		rx = 6
	}
	if ry < 2 {
		ry = 2
	}
	for i := 0; i < count; i++ {
		c := NewDrawilleCanvas(cw, h)
		// trail: last ~12 positions
		for t := 0; t < 12; t++ {
			step := float64(i-t) * 0.35
			x := cx + int(rx*math.Sin(step))
			y := cy + int(ry*math.Sin(step*2)*0.5)
			if x >= 0 && x < cw && y >= 0 && y < h {
				c.SetPixel(x, y, true)
			}
		}
		// bee head (3-pixel cluster)
		step := float64(i) * 0.35
		hx := cx + int(rx*math.Sin(step))
		hy := cy + int(ry*math.Sin(step*2)*0.5)
		for _, dp := range [][2]int{{0, 0}, {1, 0}, {0, 1}} {
			x, y := hx+dp[0], hy+dp[1]
			if x >= 0 && x < cw && y >= 0 && y < h {
				c.SetPixel(x, y, true)
			}
		}
		out = append(out, IntroFrame{Text: c.ToBraille(), Subtitle: "waggle"})
	}
	return out
}

// dripFrames draws a honey drop falling and rippling on the ground.
func dripFrames(cw, h, count int) []IntroFrame {
	out := make([]IntroFrame, 0, count)
	cx := cw / 2
	fall := count / 2
	for i := 0; i < count; i++ {
		c := NewDrawilleCanvas(cw, h)
		if i < fall {
			// falling drop: teardrop at growing y
			y := int(float64(i) / float64(fall) * float64(h-2))
			c.SetPixel(cx, y, true)
			if y >= 1 {
				c.SetPixel(cx-1, y-1, true)
				c.SetPixel(cx+1, y-1, true)
				c.SetPixel(cx, y-1, true)
			}
		} else {
			// ripple: concentric rings expanding from ground
			t := float64(i-fall) / float64(count-fall)
			gy := h - 2
			rad := int(t * float64(cw/4))
			if rad < 1 {
				rad = 1
			}
			for a := 0; a < 20; a++ {
				ang := float64(a) / 20.0 * 2 * math.Pi
				x := cx + int(float64(rad)*math.Cos(ang))
				y := gy + int(float64(rad)*math.Sin(ang)*0.4)
				if x >= 0 && x < cw && y >= 0 && y < h {
					c.SetPixel(x, y, true)
				}
			}
			// puddle base
			for dx := -1; dx <= 1; dx++ {
				if cx+dx >= 0 && cx+dx < cw {
					c.SetPixel(cx+dx, gy, true)
				}
			}
		}
		out = append(out, IntroFrame{Text: c.ToBraille(), Subtitle: "honey"})
	}
	return out
}

func swarmFrames(cw, h, count int) []IntroFrame {
	out := make([]IntroFrame, 0, count)
	for i := 0; i < count; i++ {
		c := NewDrawilleCanvas(cw, h)
		for j := 0; j < 4; j++ {
			t := float64(i)*0.3 + float64(j)*1.379
			x := int(math.Round((math.Sin(t) + 1) * 0.5 * float64(cw-1)))
			y := j % braillePxH
			c.SetPixel(x, y, true)
		}
		out = append(out, IntroFrame{Text: c.ToBraille(), Subtitle: "swarm"})
	}
	return out
}

func hexFrames(cw, h, count int) []IntroFrame {
	out := make([]IntroFrame, 0, count)
	for i := 0; i < count; i++ {
		c := NewDrawilleCanvas(cw, h)
		cx, cy := cw/2, h/2
		r := float64(cw/2 - 1)
		if r < 6 {
			r = 6
		}
		for j := 0; j < 6; j++ {
			theta := (float64(j)/6 + float64(i)/72.0) * 2 * math.Pi
			x := cx + int(math.Round(r*math.Cos(theta)))
			y := cy + int(math.Round(1.5*math.Sin(theta)))
			c.SetPixel(x, y, true)
		}
		out = append(out, IntroFrame{Text: c.ToBraille(), Subtitle: "hex"})
	}
	return out
}
