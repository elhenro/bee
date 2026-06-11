package tui

import (
	"strings"
	"testing"
)

// buffered tool-call modes (jsonmode/textmode) withhold text deltas; the
// engine forwards withheld char counts on ProgressCh instead. The ↓ figure
// must track those too, else it reads 0 for the whole call.
func TestLoaderReadout_OutTracksProgress(t *testing.T) {
	m := newTestModel(t)
	m = m.WithShowLoaderIn(true).WithShowLoaderOut(true).WithShowLoaderRate(true)
	m.state = StateStreaming

	m2, _ := m.Update(progressMsg{N: 2048})
	m = m2.(Model)

	if m.turnOutChars != 2048 {
		t.Fatalf("turnOutChars = %d, want 2048", m.turnOutChars)
	}

	out := stripANSI(m.View())
	idx := strings.Index(out, "↓ ")
	if idx < 0 {
		t.Fatalf("no ↓ figure in view: %q", out)
	}
	rest := out[idx+len("↓ "):]
	if strings.HasPrefix(rest, "0") {
		t.Fatalf("↓ figure reads 0, view: %q", out)
	}
}

// ↓ figure should track streamed output chars.
func TestLoaderReadout_OutTracksStreamDeltas(t *testing.T) {
	m := newTestModel(t)
	m = m.WithShowLoaderIn(true).WithShowLoaderOut(true).WithShowLoaderRate(true)
	m.state = StateStreaming

	m2, _ := m.Update(streamDeltaMsg{Delta: strings.Repeat("x", 1234)})
	m = m2.(Model)

	if m.turnOutChars != 1234 {
		t.Fatalf("turnOutChars = %d, want 1234", m.turnOutChars)
	}

	out := stripANSI(m.View())
	idx := strings.Index(out, "↓ ")
	if idx < 0 {
		t.Fatalf("no ↓ figure in view: %q", out)
	}
	rest := out[idx+len("↓ "):]
	if strings.HasPrefix(rest, "0") {
		t.Fatalf("↓ figure reads 0, view: %q", out)
	}
}
