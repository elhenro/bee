// Package tui implements bee's interactive Bubbletea interface.
//
// Startup intro animation — short braille frame sequence rendered above the
// input bar by the bubbletea model so typing stays available throughout.

package tui

import (
	"math"
	"time"
)

// IntroFrame is one rendered frame of the startup animation.
type IntroFrame struct {
	Text     string
	Subtitle string
}

// IntroStyle controls which animation plays on startup.
type IntroStyle int

const (
	IntroStyleRandom    IntroStyle = iota // pick one of the concrete styles per launch
	IntroStyleLifecycle                   // egg → larva → pupa → flight
	IntroStyleSwarm                       // swarm of dots
	IntroStyleHex                         // hex outline orbit
	IntroStyleDance                       // waggle dance figure-8
	IntroStyleDrip                        // honey drop + ripple
	IntroStyleBloom                       // pollen converge → hex frame + bee → pulse rings
	IntroStyleConstell                    // constellation: dots twinkle into hex tessellation
)

// IntroStyleDefault is the value used when BEE_BANNER is unset.
const IntroStyleDefault = IntroStyleBloom

// concreteIntroStyles is the pool used by random. Bloom + Constellation are
// weighted by listing them more — they are the new flagship designs.
var concreteIntroStyles = []IntroStyle{
	IntroStyleBloom,
	IntroStyleBloom,
	IntroStyleConstell,
	IntroStyleLifecycle,
	IntroStyleSwarm,
	IntroStyleHex,
	IntroStyleDance,
	IntroStyleDrip,
}

// ParseIntroStyle maps BEE_BANNER values to a style.
func ParseIntroStyle(s string) IntroStyle {
	switch s {
	case "", "random", "rand":
		return IntroStyleRandom
	case "default", "life", "lifecycle":
		return IntroStyleLifecycle
	case "swarm":
		return IntroStyleSwarm
	case "hex":
		return IntroStyleHex
	case "dance", "waggle":
		return IntroStyleDance
	case "drip", "honey":
		return IntroStyleDrip
	case "bloom", "pollen":
		return IntroStyleBloom
	case "constell", "constellation", "stars":
		return IntroStyleConstell
	default:
		return IntroStyleRandom
	}
}

// pickStyle resolves Random to a concrete style per launch (rand seeded by time).
func pickStyle(s IntroStyle) IntroStyle {
	if s != IntroStyleRandom {
		return s
	}
	return concreteIntroStyles[time.Now().UnixNano()%int64(len(concreteIntroStyles))]
}

// introArtRows is the fixed number of braille text rows per frame. 5 rows
// (20 px vertical) gives bloom/constell room for a proper hex without
// degenerating into a flat horizontal line on wide terminals.
const introArtRows = 5

// introFrameDelay is the per-frame sleep. Keep total animation short.
const introFrameDelay = 70 * time.Millisecond

func introFrames(style IntroStyle, width int) []IntroFrame {
	cells := clampCells(width)
	cw := cells * braillePxW
	h := introArtRows * braillePxH

	switch pickStyle(style) {
	case IntroStyleSwarm:
		return swarmFrames(cw, h, 24)
	case IntroStyleHex:
		return hexFrames(cw, h, 24)
	case IntroStyleDance:
		return danceFrames(cw, h, 30)
	case IntroStyleDrip:
		return dripFrames(cw, h, 28)
	case IntroStyleLifecycle:
		return lifecycleFrames(cw, h)
	case IntroStyleConstell:
		return constellFrames(cw, h, 32)
	case IntroStyleBloom:
		return bloomFrames(cw, h, 34)
	default:
		return bloomFrames(cw, h, 34)
	}
}

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

