package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/caveman"
)

func spinnerRenderer() *StreamRenderer {
	r := NewStreamRenderer(DefaultStyles(), 100)
	r.SetLoaderStats(LoaderStats{
		InTokens: 3400, OutTokens: 340, RateTokS: 45, Seed: 3,
		ShowIn: true, ShowOut: true, ShowRate: true,
	})
	return r
}

func TestRenderSpinner_ShowsVerbElapsedTokensAndHint(t *testing.T) {
	r := spinnerRenderer()
	out := stripANSI(r.RenderSpinner(2, 75*time.Second))
	for _, want := range []string{r.spinnerVerb() + "…", "1m 15s", "↑ 3.4k", "↓ 340", "45 tok/s", interruptHint} {
		if !strings.Contains(out, want) {
			t.Fatalf("busy row missing %q in %q", want, out)
		}
	}
}

func TestRenderSpinner_DanceChangesWithFrame(t *testing.T) {
	r := spinnerRenderer()
	seen := map[string]bool{}
	for f := range waggleCycle {
		seen[stripANSI(r.RenderSpinner(f, 0))[1:5]] = true
	}
	if len(seen) != waggleCycle {
		t.Fatalf("expected %d distinct dance frames across a cycle, got %d", waggleCycle, len(seen))
	}
}

func TestRenderSpinner_StaticMarkWhenMotionOff(t *testing.T) {
	r := spinnerRenderer()
	r.SetShowLoader(false)
	a, b := stripANSI(r.RenderSpinner(1, 0)), stripANSI(r.RenderSpinner(7, 0))
	if a != b || !strings.Contains(a, glyphBee) {
		t.Fatalf("motion off should hold a static bee mark: %q vs %q", a, b)
	}
}

func TestRenderSpinner_VerbHoldsStillPerTurn(t *testing.T) {
	r := spinnerRenderer()
	v := r.spinnerVerb()
	for f := range 20 {
		if !strings.Contains(stripANSI(r.RenderSpinner(f, 0)), v) {
			t.Fatalf("verb changed mid-turn at frame %d", f)
		}
	}
	r.SetLoaderStats(LoaderStats{Seed: -7})
	if got := r.spinnerVerb(); got == "" {
		t.Fatalf("negative seed must still pick a verb")
	}
}

func TestRenderSpinnerLabel_CustomLabelNoHint(t *testing.T) {
	r := spinnerRenderer()
	out := stripANSI(r.RenderSpinnerLabel(0, "Compacting context…", 0, ""))
	if !strings.Contains(out, "Compacting context…") || strings.Contains(out, interruptHint) {
		t.Fatalf("custom label row wrong: %q", out)
	}
}

func TestFormatClock(t *testing.T) {
	cases := map[time.Duration]string{
		0:                       "0s",
		7400 * time.Millisecond: "7s",
		59 * time.Second:        "59s",
		64 * time.Second:        "1m 04s",
	}
	for d, want := range cases {
		if got := formatClock(d); got != want {
			t.Errorf("formatClock(%v) = %q, want %q", d, got, want)
		}
	}
}

// the resize rebuild must carry show_loader over to the new renderer
func TestWindowSize_KeepsShowLoaderOff(t *testing.T) {
	m := NewModel(nil, "/tmp/work", "m", "", caveman.Default).WithShowLoader(false)
	for _, w := range []int{100, 60} {
		nm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		m = nm.(Model)
		if m.stream.showLoader {
			t.Fatalf("width %d: renderer lost show_loader=false", w)
		}
	}
}
