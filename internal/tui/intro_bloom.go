package tui

import "math"

// bloomFrames: pollen particles drift in from offscreen, converge onto a
// hex-shaped perimeter around a bee silhouette, then pulse rings ripple
// outward before particles dissolve away. Cubic easing on convergence
// gives the gather a "settling" feel rather than linear march.
func bloomFrames(cw, h, count int) []IntroFrame {
	out := make([]IntroFrame, 0, count)
	cx, cy := cw/2, h/2
	// hex perimeter radius. Braille pixels are roughly square visually
	// (0.5 char × 0.5 char), so an equilateral hex needs rx ≈ ry * 1.15.
	// Cap rx so on wide terminals the hex stays hex-shaped instead of
	// stretching into a flat horizontal slab.
	ry := float64(h)/2 - 1
	if ry < 3 {
		ry = 3
	}
	rx := ry * 1.6
	if maxRX := float64(cw)/2 - 2; rx > maxRX {
		rx = maxRX
	}
	if rx < 6 {
		rx = 6
	}
	// 30 deterministic particles: targets on hex perimeter, origins offscreen
	const n = 30
	type pt struct{ x, y float64 }
	targets := make([]pt, n)
	origins := make([]pt, n)
	for j := 0; j < n; j++ {
		ang := float64(j)/float64(n)*2*math.Pi - math.Pi/2
		// hex-flavored radius modulation: 6-fold corner accent
		mod := 1.0 + 0.18*math.Cos(ang*3)
		targets[j] = pt{
			float64(cx) + rx*mod*math.Cos(ang),
			float64(cy) + ry*mod*math.Sin(ang),
		}
		// origin scattered far via golden-angle distribution (deterministic)
		oa := float64(j) * 2.3998
		origins[j] = pt{
			float64(cx) + float64(cw)*0.9*math.Cos(oa),
			float64(cy) + float64(h)*1.5*math.Sin(oa),
		}
	}
	// pre-rendered bee silhouette (4×4)
	beeIdle := []byte{
		0, 1, 1, 0,
		1, 1, 1, 1,
		1, 1, 1, 1,
		0, 1, 1, 0,
	}
	beeFlap := []byte{
		1, 0, 0, 1,
		1, 1, 1, 1,
		1, 1, 1, 1,
		1, 0, 0, 1,
	}
	gather := count * 38 / 100
	settle := count * 18 / 100
	pulse := count * 32 / 100
	for i := 0; i < count; i++ {
		c := NewDrawilleCanvas(cw, h)
		switch {
		case i < gather:
			t := float64(i) / float64(gather)
			t = 1 - math.Pow(1-t, 3) // ease-out cubic
			// stagger particles in waves of 3 so they don't all arrive together
			for j := 0; j < n; j++ {
				wave := float64((j % 3)) * 0.18
				tt := math.Max(0, math.Min(1, (t-wave)/(1-wave)))
				if tt <= 0 {
					continue
				}
				o := origins[j]
				tg := targets[j]
				x := int(math.Round(o.x + (tg.x-o.x)*tt))
				y := int(math.Round(o.y + (tg.y-o.y)*tt))
				if x >= 0 && x < cw && y >= 0 && y < h {
					c.SetPixel(x, y, true)
				}
			}
		case i < gather+settle:
			drawHexPerimeter(c, cx, cy, rx, ry)
			t := float64(i-gather) / float64(settle)
			// bee fades in via cluster growth
			r := int(math.Round(t * 2))
			for dy := -r; dy <= r; dy++ {
				for dx := -r; dx <= r; dx++ {
					if dx*dx+dy*dy <= r*r {
						c.SetPixel(cx+dx, cy+dy, true)
					}
				}
			}
			if t > 0.6 {
				c.DrawSprite(beeIdle, 4, 4, cx-2, cy-2)
			}
		case i < gather+settle+pulse:
			drawHexPerimeter(c, cx, cy, rx, ry)
			// wing flap on alternate frames
			if (i/2)%2 == 0 {
				c.DrawSprite(beeFlap, 4, 4, cx-2, cy-2)
				for dx := -2; dx <= -1; dx++ {
					c.SetPixel(cx+dx, cy, true)
				}
				for dx := 2; dx <= 3; dx++ {
					c.SetPixel(cx+dx, cy, true)
				}
			} else {
				c.DrawSprite(beeIdle, 4, 4, cx-2, cy-2)
			}
			// expanding dashed ring — depth via two rings out of phase.
			// Ellipse keeps the same x:y ratio as the hex itself so it
			// stays visually round on the terminal aspect.
			tp := float64(i-gather-settle) / float64(pulse)
			ringMax := math.Min(float64(cw)/2-1, ry*4)
			for ring := 0; ring < 2; ring++ {
				rpx := rx + 2 + tp*ringMax + float64(ring)*3
				rpy := ry + 1 + tp*(ringMax*ry/rx) + float64(ring)*1.5
				for a := 0; a < 36; a++ {
					if (a+i+ring*5)%4 != 0 {
						continue
					}
					ang := float64(a) / 36 * 2 * math.Pi
					x := cx + int(math.Round(rpx*math.Cos(ang)))
					y := cy + int(math.Round(rpy*math.Sin(ang)))
					if x >= 0 && x < cw && y >= 0 && y < h {
						c.SetPixel(x, y, true)
					}
				}
			}
		default:
			// dissolve: particles drift back out, bee remains
			rem := count - gather - settle - pulse
			t := float64(i-gather-settle-pulse) / float64(rem)
			t = math.Pow(t, 2)
			for j := 0; j < n; j++ {
				if (j+i)%2 == 0 {
					continue
				}
				o := origins[j]
				tg := targets[j]
				x := int(math.Round(tg.x + (o.x-tg.x)*t))
				y := int(math.Round(tg.y + (o.y-tg.y)*t))
				if x >= 0 && x < cw && y >= 0 && y < h {
					c.SetPixel(x, y, true)
				}
			}
			c.DrawSprite(beeIdle, 4, 4, cx-2, cy-2)
		}
		sub := ""
		switch {
		case i < gather:
			sub = "gather"
		case i < gather+settle:
			sub = "bloom"
		case i < gather+settle+pulse:
			sub = "hum"
		}
		out = append(out, IntroFrame{Text: c.ToBraille(), Subtitle: sub})
	}
	return out
}

// drawHexPerimeter draws a flat-top-ish hexagon outline by lerping six
// vertex pairs. Vertical squash via ry mirrors terminal cell aspect.
func drawHexPerimeter(c *DrawilleCanvas, cx, cy int, rx, ry float64) {
	verts := make([][2]float64, 6)
	for k := 0; k < 6; k++ {
		ang := float64(k)/6*2*math.Pi - math.Pi/2
		verts[k] = [2]float64{
			float64(cx) + rx*math.Cos(ang),
			float64(cy) + ry*math.Sin(ang),
		}
	}
	const steps = 14
	for k := 0; k < 6; k++ {
		a := verts[k]
		b := verts[(k+1)%6]
		for s := 0; s <= steps; s++ {
			t := float64(s) / float64(steps)
			x := int(math.Round(a[0] + (b[0]-a[0])*t))
			y := int(math.Round(a[1] + (b[1]-a[1])*t))
			c.SetPixel(x, y, true)
		}
	}
}

// constellFrames: scattered "stars" twinkle in via deterministic phase
// offsets, drifting slowly. Surprise: at midpoint, three stars connect
// to form a brief hex constellation that then fades back to drift.
func constellFrames(cw, h, count int) []IntroFrame {
	out := make([]IntroFrame, 0, count)
	const n = 18
	// deterministic star positions via golden-angle
	type pt struct{ x, y int }
	stars := make([]pt, n)
	for j := 0; j < n; j++ {
		a := float64(j) * 2.3998
		stars[j] = pt{
			(int(math.Round((math.Sin(a*1.3)+1)*0.5*float64(cw-2))) + 1) % cw,
			(int(math.Round((math.Cos(a*1.7)+1)*0.5*float64(h-1))) + j) % h,
		}
		if stars[j].x < 0 {
			stars[j].x += cw
		}
		if stars[j].y < 0 {
			stars[j].y += h
		}
	}
	mid := count / 2
	band := count / 6
	for i := 0; i < count; i++ {
		c := NewDrawilleCanvas(cw, h)
		// twinkle: each star on/off via phase
		for j, s := range stars {
			phase := (i + j*3) % 7
			if phase < 5 {
				c.SetPixel(s.x, s.y, true)
				// halo on bright phase
				if phase < 2 {
					if s.x+1 < cw {
						c.SetPixel(s.x+1, s.y, true)
					}
					if s.y+1 < h {
						c.SetPixel(s.x, s.y+1, true)
					}
				}
			}
		}
		// midpoint reveal: hex constellation lines for a few frames
		if i >= mid-band && i <= mid+band {
			cx, cy := cw/2, h/2
			ry := float64(h)/2 - 1
			if ry < 3 {
				ry = 3
			}
			rx := ry * 1.6
			if maxRX := float64(cw)/2 - 2; rx > maxRX {
				rx = maxRX
			}
			drawHexPerimeter(c, cx, cy, rx, ry)
		}
		sub := ""
		switch {
		case i < mid-band:
			sub = "drift"
		case i <= mid+band:
			sub = "align"
		default:
			sub = "scatter"
		}
		out = append(out, IntroFrame{Text: c.ToBraille(), Subtitle: sub})
	}
	return out
}
