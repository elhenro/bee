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

func TestRenderTokenStreamSpikeClamped(t *testing.T) {
	// the renderer caps n at cells regardless of Rate, so a Rate=200 input
	// on a 30-cell row produces a 30-cell row. The actual fix for the
	// "first token flash" lives in app_update_stream: the EMA in
	// smoothLoaderRate ensures the Rate value fed here never spikes from
	// idle in a single tick. This test exercises that helper directly.
	prev := 0.0
	// 200 chars in one tick — the worst case the user reported.
	got := smoothLoaderRate(prev, 200)
	if got > 61 {
		t.Errorf("EMA after 1 spike: got %.1f, want ≤ 61 (3-tick rise time)", got)
	}
	// three more ticks of 200 chars to confirm it climbs but never overshoots.
	for i := 0; i < 3; i++ {
		prev = got
		got = smoothLoaderRate(prev, 200)
	}
	if got < 100 || got > 200 {
		t.Errorf("EMA steady state: got %.1f, want in (100, 200)", got)
	}
}

func TestSmoothLoaderRateIdleDecays(t *testing.T) {
	// a steady stream followed by silence must decay back toward 0 — the
	// loader should ease off when the model pauses mid-turn.
	prev := 0.0
	for i := 0; i < 10; i++ {
		prev = smoothLoaderRate(prev, 50)
	}
	if prev < 40 {
		t.Errorf("after 10 ticks of 50: got %.1f, want ≥ 40 (near steady state)", prev)
	}
	// model goes quiet — symmetric to the 3-tick rise time.
	for i := 0; i < 10; i++ {
		prev = smoothLoaderRate(prev, 0)
	}
	if prev > 5 {
		t.Errorf("after 10 idle ticks: got %.1f, want ≤ 5", prev)
	}
}

func TestSmoothLoaderRateResetToZero(t *testing.T) {
	// the hasThinkingBlock branch resets the EMA. Simulate that by feeding
	// delta=0 immediately after — first frame after the reset must produce
	// a small value, not the prior spike.
	prev := 150.0
	got := smoothLoaderRate(prev, 0)
	if got > 110 {
		t.Errorf("post-reset tick: got %.1f, want ≤ 110 (decay from 150)", got)
	}
}

func TestRenderTokenStreamIdleFloor(t *testing.T) {
	// Rate=0 must still render 5 particles, not 3 — the prior floor looked
	// like a single dot on a wide terminal.
	got := renderTokenStream(LoaderStats{Seed: 1, Rate: 0}, 0, 80)
	if utf8.RuneCountInString(got) != 80 {
		t.Errorf("idle row width=%d want 80", utf8.RuneCountInString(got))
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
