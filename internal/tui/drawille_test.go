package tui

import (
	"strings"
	"testing"
)

func TestDrawilleCanvas_EmptyIsAllSpaces(t *testing.T) {
	c := NewDrawilleCanvas(8, 4)
	got := c.ToBraille()
	want := strings.Repeat(string(brailleBase), 4)
	if got != want {
		t.Fatalf("empty 8×4 canvas: got %q want %q", got, want)
	}
}

func TestDrawilleCanvas_AllOnIsFullDots(t *testing.T) {
	c := NewDrawilleCanvas(2, 4)
	for y := 0; y < 4; y++ {
		for x := 0; x < 2; x++ {
			c.SetPixel(x, y, true)
		}
	}
	got := c.ToBraille()
	if got != "⣿" {
		t.Fatalf("all-on 2×4 canvas: got %q want ⣿", got)
	}
}

func TestDrawilleCanvas_PixelBits(t *testing.T) {
	// Each pixel maps to a distinct braille bit per the layout.
	cases := []struct {
		x, y int
		want rune
	}{
		{0, 0, '⠁'},
		{0, 1, '⠂'},
		{0, 2, '⠄'},
		{0, 3, '⡀'},
		{1, 0, '⠈'},
		{1, 1, '⠐'},
		{1, 2, '⠠'},
		{1, 3, '⢀'},
	}
	for _, tc := range cases {
		c := NewDrawilleCanvas(2, 4)
		c.SetPixel(tc.x, tc.y, true)
		got := []rune(c.ToBraille())[0]
		if got != tc.want {
			t.Errorf("pixel (%d,%d): got %q want %q", tc.x, tc.y, got, tc.want)
		}
	}
}

func TestDrawilleCanvas_OutOfBoundsIsNoOp(t *testing.T) {
	c := NewDrawilleCanvas(4, 4)
	// must not panic.
	c.SetPixel(-1, 0, true)
	c.SetPixel(0, -5, true)
	c.SetPixel(99, 0, true)
	c.SetPixel(0, 99, true)
	// canvas still blank.
	want := strings.Repeat(string(brailleBase), 2)
	if got := c.ToBraille(); got != want {
		t.Fatalf("expected blank canvas, got %q", got)
	}
}

func TestDrawilleCanvas_Clear(t *testing.T) {
	c := NewDrawilleCanvas(2, 4)
	c.SetPixel(0, 0, true)
	c.Clear()
	if got := c.ToBraille(); got != string(brailleBase) {
		t.Fatalf("Clear() did not reset canvas, got %q", got)
	}
}

func TestBrailleSparkline_FlatZero(t *testing.T) {
	got := BrailleSparkline([]float64{0, 0, 0, 0}, 2)
	// each pixel column at row 3 → bit 0x40 (col0) / 0x80 (col1) → ⡀ + ⢀ = ⣀
	if !strings.ContainsRune(got, '⣀') {
		t.Fatalf("flat-zero sparkline should baseline-render ⣀, got %q", got)
	}
}

func TestBrailleSparkline_Empty(t *testing.T) {
	if got := BrailleSparkline(nil, 4); got != "" {
		t.Errorf("empty input should return empty string, got %q", got)
	}
	if got := BrailleSparkline([]float64{1, 2}, 0); got != "" {
		t.Errorf("zero cells should return empty string, got %q", got)
	}
}

func TestBrailleLoaderPainters_AllRenderFixedWidth(t *testing.T) {
	// every named painter must render exactly the requested cell count,
	// across many frames and several widths.
	for _, cells := range []int{8, 20, 40, 120} {
		for _, p := range brailleNamedPainters {
			for frame := 0; frame < 80; frame++ {
				got := p.fn(frame, cells)
				runes := []rune(got)
				if len(runes) != cells {
					t.Errorf("painter %q frame %d cells=%d: width %d runes, want %d (%q)", p.name, frame, cells, len(runes), cells, got)
					break
				}
			}
		}
	}
}

func TestBrailleLoaderPainters_TinyClampsToMin(t *testing.T) {
	// request fewer than min cells — every painter should silently clamp
	// up to brailleLoaderMinCells.
	for _, p := range brailleNamedPainters {
		got := p.fn(0, 1)
		if r := []rune(got); len(r) != brailleLoaderMinCells {
			t.Errorf("painter %q cells=1: got %d runes, want clamp to %d", p.name, len(r), brailleLoaderMinCells)
		}
	}
}

func TestBraillePhases_CoverAllFrameBoundaries(t *testing.T) {
	for _, frame := range []int{0, 79, 80, 239, 240, 479, 480, 959, 960, 9999} {
		p := braillePhaseFor(frame)
		if p == nil {
			t.Errorf("braillePhaseFor(%d) returned nil", frame)
			continue
		}
		// painter must produce a stable-width result for any frame.
		got := p(frame, 16)
		if r := []rune(got); len(r) != 16 {
			t.Errorf("phase painter at frame %d: got %d runes, want 16", frame, len(r))
		}
	}
}
