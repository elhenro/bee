package tui

import (
	"slices"
	"strings"
)

// Braille-pattern pixel canvas (U+2800–U+28FF). Each rune packs 8 binary
// pixels (2 cols × 4 rows) — ~8× vertical density over ASCII art. Bit
// layout inside one cell:
//
//	col0  col1
//	row0  0x01  0x08
//	row1  0x02  0x10
//	row2  0x04  0x20
//	row3  0x40  0x80
const brailleBase = '⠀'

const (
	braillePxW = 2 // pixels per cell column
	braillePxH = 4 // pixels per cell row
)

// brailleBits[row][col] is the bit mask for a pixel inside one 2×4 cell.
var brailleBits = [braillePxH][braillePxW]uint8{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

// DrawilleCanvas is a binary pixel buffer that renders as braille text.
// Out-of-bounds pixel writes are silently clipped so sprite painters can
// overflow the edge without bounds checks.
type DrawilleCanvas struct {
	w, h           int
	cellsW, cellsH int
	grid           []uint8 // row-major, len = cellsW * cellsH
}

// NewDrawilleCanvas allocates a canvas of the given pixel dimensions.
func NewDrawilleCanvas(w, h int) *DrawilleCanvas {
	cw := (w + braillePxW - 1) / braillePxW
	ch := (h + braillePxH - 1) / braillePxH
	return &DrawilleCanvas{
		w: w, h: h,
		cellsW: cw, cellsH: ch,
		grid: make([]uint8, cw*ch),
	}
}

// SetPixel turns one pixel on (true) or off (false). Out-of-bounds is a
// no-op so animations can sweep sprites past the edge cleanly.
func (c *DrawilleCanvas) SetPixel(x, y int, on bool) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return
	}
	cx, cy := x/braillePxW, y/braillePxH
	bit := brailleBits[y%braillePxH][x%braillePxW]
	if on {
		c.grid[cy*c.cellsW+cx] |= bit
	} else {
		c.grid[cy*c.cellsW+cx] &^= bit
	}
}

// ToBraille renders the canvas as multi-line braille text. Lines are
// separated by \n; each line is exactly cellsW runes wide.
func (c *DrawilleCanvas) ToBraille() string {
	var b strings.Builder
	b.Grow((c.cellsW + 1) * c.cellsH)
	for row := 0; row < c.cellsH; row++ {
		if row > 0 {
			b.WriteByte('\n')
		}
		for col := 0; col < c.cellsW; col++ {
			b.WriteRune(brailleBase + rune(c.grid[row*c.cellsW+col]))
		}
	}
	return b.String()
}

// DrawSprite paints a row-major binary sprite onto the canvas at (ox, oy).
// Pixels outside the canvas are clipped. Non-zero sprite bytes light up.
func (c *DrawilleCanvas) DrawSprite(sprite []byte, w, h, ox, oy int) {
	for y := range h {
		for x := range w {
			if sprite[y*w+x] != 0 {
				c.SetPixel(ox+x, oy+y, true)
			}
		}
	}
}

// BrailleSparkline returns a one-line braille sparkline of vals normalized
// to [0, h] pixels. width is in braille cells (each cell encodes 2 samples
// × 4 vertical levels). Empty input returns "".
func BrailleSparkline(vals []float64, cells int) string {
	if len(vals) == 0 || cells <= 0 {
		return ""
	}
	w := cells * braillePxW
	c := NewDrawilleCanvas(w, braillePxH)
	maxV := slices.Max(vals)
	if maxV <= 0 {
		// flat-zero series: render baseline so the bar still appears.
		for x := range w {
			c.SetPixel(x, braillePxH-1, true)
		}
		return c.ToBraille()
	}
	for x := range w {
		// pick value index, allow downsampling/upsampling.
		i := min(x*len(vals)/w, len(vals)-1)
		hPx := min(max(int(vals[i]/maxV*float64(braillePxH)), 1), braillePxH)
		for y := braillePxH - hPx; y < braillePxH; y++ {
			c.SetPixel(x, y, true)
		}
	}
	return c.ToBraille()
}
