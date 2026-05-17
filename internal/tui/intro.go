// Package tui implements bee's interactive Bubbletea interface.
//
// Startup intro animation — short in-place braille animation that redraws
// in the same screen rows, then leaves the cursor below for the banner.

package tui

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// IntroFrame is one rendered frame of the startup animation.
type IntroFrame struct {
	Text     string
	Subtitle string
}

// IntroStyle controls which animation plays on startup.
type IntroStyle int

const (
	IntroStyleDefault IntroStyle = iota // lifecycle: egg → larva → pupa → flight
	IntroStyleSwarm                     // swarm only
	IntroStyleHex                       // hex outline only
)

// ParseIntroStyle maps BEE_BANNER values to a style.
func ParseIntroStyle(s string) IntroStyle {
	switch s {
	case "", "default", "life", "lifecycle":
		return IntroStyleDefault
	case "swarm":
		return IntroStyleSwarm
	case "hex":
		return IntroStyleHex
	default:
		return IntroStyleDefault
	}
}

// introArtRows is the fixed number of braille text rows per frame.
const introArtRows = 3

// introFrameDelay is the per-frame sleep. Keep total animation short.
const introFrameDelay = 70 * time.Millisecond

func introFrames(style IntroStyle, width int) []IntroFrame {
	cells := clampCells(width)
	cw := cells * braillePxW
	h := introArtRows * braillePxH

	switch style {
	case IntroStyleSwarm:
		return swarmFrames(cw, h, 24)
	case IntroStyleHex:
		return hexFrames(cw, h, 24)
	default:
		return lifecycleFrames(cw, h)
	}
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
	add(5, "egg", func(i, n int) *DrawilleCanvas { return makeEggFrame(cw, h, i, n) })
	add(6, "larva", func(i, n int) *DrawilleCanvas { return makeLarvaFrame(cw, h, i, n) })
	add(5, "pupa", func(i, n int) *DrawilleCanvas { return makePupaFrame(cw, h, i, n) })
	add(4, "emerge", func(i, n int) *DrawilleCanvas { return makeEmergeFrame(cw, h, i, n) })
	add(8, "flight", func(i, n int) *DrawilleCanvas { return makeFlightFrame(cw, h, i, n) })
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

func makeFlightFrame(cw, h, idx, total int) *DrawilleCanvas {
	c := NewDrawilleCanvas(cw, h)
	t := float64(idx) / float64(total)
	x := int(-4 + t*float64(cw+8))
	y := h/2 + int(math.Sin(float64(idx)*0.6)*float64(h/4))
	sprite := []byte{0, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1, 1, 0, 1, 1, 0}
	c.DrawSprite(sprite, 4, 4, x, y-2)
	if (idx/2)%2 == 0 {
		for dx := -2; dx <= -1; dx++ {
			if x+dx >= 0 && x+dx < cw {
				c.SetPixel(x+dx, y, true)
			}
		}
		for dx := 4; dx <= 5; dx++ {
			if x+dx >= 0 && x+dx < cw {
				c.SetPixel(x+dx, y, true)
			}
		}
	}
	return c
}

// PrintIntro plays the startup animation to stderr, then returns the
// static banner string. Animation redraws in place using ANSI cursor-up
// codes; all output goes to a single stream (stderr).
func PrintIntro(version string) string {
	honey := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
	dim := lipgloss.NewStyle().Foreground(fgOyster)

	info := "bee"
	if version != "" {
		info += " " + version
	}
	banner := honey.Render("⬢") + "  " + dim.Render(info) + "\n"

	if os.Getenv("BEE_NO_INTRO") == "1" {
		return banner
	}

	// terminal width (fallback to 60)
	width := 60
	if s := os.Getenv("COLUMNS"); s != "" {
		if n, ok := strToInt(s); ok && n > 16 {
			width = n
		}
	}

	style := ParseIntroStyle(os.Getenv("BEE_BANNER"))
	frames := introFrames(style, width)
	if len(frames) == 0 {
		return banner
	}

	w := os.Stderr
	totalRows := introArtRows + 1 // art + subtitle

	// reserve space: print totalRows blank lines, then move cursor back up
	for i := 0; i < totalRows; i++ {
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "\033[%dA", totalRows)
	// hide cursor during animation
	fmt.Fprint(w, "\033[?25l")
	defer fmt.Fprint(w, "\033[?25h")

	for fi, f := range frames {
		artLines := strings.Split(f.Text, "\n")
		for r := 0; r < introArtRows; r++ {
			fmt.Fprint(w, "\r\033[2K")
			if r < len(artLines) {
				fmt.Fprint(w, artLines[r])
			}
			fmt.Fprintln(w)
		}
		// subtitle row
		fmt.Fprint(w, "\r\033[2K")
		if f.Subtitle != "" {
			fmt.Fprint(w, dim.Render("  "+f.Subtitle))
		}
		fmt.Fprintln(w)

		if fi < len(frames)-1 {
			fmt.Fprintf(w, "\033[%dA", totalRows)
			time.Sleep(introFrameDelay)
		}
	}

	return banner
}

// strToInt converts a decimal string to int (returns 0,false on parse failure).
func strToInt(s string) (int, bool) {
	if len(s) == 0 {
		return 0, false
	}
	n, neg, i := 0, false, 0
	if s[0] == '-' {
		neg = true
		i = 1
	}
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	if neg {
		n = -n
	}
	return n, true
}
