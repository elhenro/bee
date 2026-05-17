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
)

// IntroStyleDefault is the value used when BEE_BANNER is unset.
const IntroStyleDefault = IntroStyleRandom

// concreteIntroStyles is the pool used by random.
var concreteIntroStyles = []IntroStyle{
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

// introArtRows is the fixed number of braille text rows per frame.
const introArtRows = 3

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
	default:
		return lifecycleFrames(cw, h)
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

