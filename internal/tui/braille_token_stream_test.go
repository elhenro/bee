package tui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRenderTokenStreamWidth(t *testing.T) {
	for _, cells := range []int{8, 20, 40, 120} {
		got := renderTokenStream(LoaderStats{Seed: 1, Rate: 10}, 5, cells)
		if n := utf8.RuneCountInString(got); n != clampCells(cells) {
			t.Errorf("cells=%d: width=%d want=%d", cells, n, clampCells(cells))
		}
	}
}

func TestRenderTokenStreamClampsTiny(t *testing.T) {
	// below the floor still yields a valid min-width row, never panics.
	got := renderTokenStream(LoaderStats{Seed: 2}, 0, 1)
	if n := utf8.RuneCountInString(got); n != brailleLoaderMinCells {
		t.Errorf("tiny width=%d want=%d", n, brailleLoaderMinCells)
	}
}

func TestRenderTokenStreamDeterministic(t *testing.T) {
	a := renderTokenStream(LoaderStats{Seed: 42, Rate: 30}, 7, 24)
	b := renderTokenStream(LoaderStats{Seed: 42, Rate: 30}, 7, 24)
	if a != b {
		t.Error("same seed/frame/cells must reproduce identical output")
	}
}

func TestRenderTokenStreamSeedVaries(t *testing.T) {
	// across a window of frames at least one differs between seeds — guards
	// against a seed that has no visible effect.
	diff := false
	for f := 0; f < 12; f++ {
		if renderTokenStream(LoaderStats{Seed: 1, Rate: 20}, f, 30) !=
			renderTokenStream(LoaderStats{Seed: 999, Rate: 20}, f, 30) {
			diff = true
			break
		}
	}
	if !diff {
		t.Error("different seeds produced identical streams across 12 frames")
	}
}

func TestFormatLoaderReadout(t *testing.T) {
	stats := LoaderStats{InTokens: 12300, OutChars: 1847, ShowIn: true, ShowOut: true}
	full := formatLoaderReadout(stats, 40)
	if !strings.Contains(full, "↑") || !strings.Contains(full, "↓") {
		t.Errorf("wide budget should keep both arrows: %q", full)
	}
	short := formatLoaderReadout(stats, 9)
	if strings.Contains(short, "↑") || !strings.HasPrefix(short, "↓ ") {
		t.Errorf("small budget should be down-only: %q", short)
	}
	if got := formatLoaderReadout(stats, 2); got != "" {
		t.Errorf("tiny budget should be empty: %q", got)
	}
}

func TestFormatLoaderReadoutZeroInput(t *testing.T) {
	// zero input (first turn) renders 0, not an em-dash.
	got := formatLoaderReadout(LoaderStats{InTokens: 0, OutChars: 5, ShowIn: true, ShowOut: true}, 40)
	if got != "↑ 0 ↓ 5" {
		t.Errorf("zero input should render 0: %q", got)
	}
}
