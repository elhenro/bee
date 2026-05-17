package tui

import "math"

// rainFrames: staggered honey drops fall at multiple columns with short
// trails. Columns chosen by golden-angle for even spread without RNG.
func rainFrames(cw, h, count int) []IntroFrame {
	out := make([]IntroFrame, 0, count)
	const drops = 7
	cols := make([]int, drops)
	offsets := make([]int, drops)
	for j := 0; j < drops; j++ {
		cols[j] = int(math.Mod(float64(j)*2.3998*float64(cw)/(2*math.Pi), float64(cw)))
		if cols[j] < 0 {
			cols[j] += cw
		}
		offsets[j] = (j * 5) % count
	}
	for i := 0; i < count; i++ {
		c := NewDrawilleCanvas(cw, h)
		for j := 0; j < drops; j++ {
			t := (i + offsets[j]) % count
			y := int(float64(t) / float64(count) * float64(h+3))
			x := cols[j]
			for k := 0; k < 4; k++ {
				yy := y - k
				if yy >= 0 && yy < h && x >= 0 && x < cw {
					c.SetPixel(x, yy, true)
				}
			}
		}
		out = append(out, IntroFrame{Text: c.ToBraille(), Subtitle: "rain"})
	}
	return out
}

// spiralFrames: a single particle traces an inward logarithmic spiral,
// leaving a fading trail behind it.
func spiralFrames(cw, h, count int) []IntroFrame {
	out := make([]IntroFrame, 0, count)
	cx, cy := cw/2, h/2
	maxR := float64(cw)/2 - 1
	if maxR < 6 {
		maxR = 6
	}
	if ry := float64(h)/2 - 1; ry < maxR*0.4 {
		maxR = ry / 0.4
	}
	for i := 0; i < count; i++ {
		c := NewDrawilleCanvas(cw, h)
		for t := 0; t < 20; t++ {
			step := i - t
			if step < 0 {
				continue
			}
			progress := float64(step) / float64(count)
			r := maxR * (1 - progress)
			if r < 1 {
				r = 1
			}
			ang := float64(step) * 0.45
			x := cx + int(r*math.Cos(ang))
			y := cy + int(r*math.Sin(ang)*0.4)
			if x >= 0 && x < cw && y >= 0 && y < h {
				c.SetPixel(x, y, true)
			}
		}
		out = append(out, IntroFrame{Text: c.ToBraille(), Subtitle: "spiral"})
	}
	return out
}

// waveFrames: a sine wave sweeps horizontally with a brighter crest cluster.
func waveFrames(cw, h, count int) []IntroFrame {
	out := make([]IntroFrame, 0, count)
	cy := h / 2
	amp := float64(h)/2 - 1
	if amp < 2 {
		amp = 2
	}
	for i := 0; i < count; i++ {
		c := NewDrawilleCanvas(cw, h)
		phase := float64(i) * 0.35
		crestX := int(float64(cw) * float64(i) / float64(count))
		for x := 0; x < cw; x++ {
			y := cy + int(amp*math.Sin(float64(x)*0.18-phase))
			if y >= 0 && y < h {
				c.SetPixel(x, y, true)
			}
		}
		for _, dp := range [][2]int{{0, 0}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			x := crestX + dp[0]
			y := cy + int(amp*math.Sin(float64(crestX)*0.18-phase)) + dp[1]
			if x >= 0 && x < cw && y >= 0 && y < h {
				c.SetPixel(x, y, true)
			}
		}
		out = append(out, IntroFrame{Text: c.ToBraille(), Subtitle: "wave"})
	}
	return out
}

// orbitFrames: three particles orbit a central dot at different radii
// and speeds — small solar-system feel.
func orbitFrames(cw, h, count int) []IntroFrame {
	out := make([]IntroFrame, 0, count)
	cx, cy := cw/2, h/2
	baseR := float64(cw)/6 - 1
	if baseR < 3 {
		baseR = 3
	}
	radii := []float64{baseR, baseR * 1.8, baseR * 2.6}
	speeds := []float64{0.42, 0.28, 0.18}
	for i := 0; i < count; i++ {
		c := NewDrawilleCanvas(cw, h)
		// center
		c.SetPixel(cx, cy, true)
		for j, r := range radii {
			ang := float64(i)*speeds[j] + float64(j)*1.3
			x := cx + int(r*math.Cos(ang))
			y := cy + int(r*math.Sin(ang)*0.4)
			for _, dp := range [][2]int{{0, 0}, {1, 0}} {
				xx, yy := x+dp[0], y+dp[1]
				if xx >= 0 && xx < cw && yy >= 0 && yy < h {
					c.SetPixel(xx, yy, true)
				}
			}
			// faint trail
			for t := 1; t < 6; t++ {
				ta := ang - float64(t)*0.12
				tx := cx + int(r*math.Cos(ta))
				ty := cy + int(r*math.Sin(ta)*0.4)
				if tx >= 0 && tx < cw && ty >= 0 && ty < h && t%2 == 0 {
					c.SetPixel(tx, ty, true)
				}
			}
		}
		out = append(out, IntroFrame{Text: c.ToBraille(), Subtitle: "orbit"})
	}
	return out
}
