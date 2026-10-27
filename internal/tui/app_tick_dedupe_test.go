package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func tickingModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t)
	m.state = StateStreaming
	return m
}

// a follow-up submit arms a second loader chain while the old tick is still
// pending. Both ticks carry the same frame: only the first may advance.
func TestLoaderTick_DuplicateChainCollapses(t *testing.T) {
	m := tickingModel(t)
	tick := loaderTickMsg{frame: m.loaderFrame, keyed: true}

	nm, cmd := m.Update(tick)
	m = nm.(Model)
	if cmd == nil || m.loaderFrame != 1 {
		t.Fatalf("first tick must advance and rearm: frame=%d cmd=%v", m.loaderFrame, cmd != nil)
	}
	stalls := m.loaderStallFrames
	nm, cmd = m.Update(tick)
	m = nm.(Model)
	if cmd != nil {
		t.Error("duplicate tick must not rearm")
	}
	if m.loaderFrame != 1 || m.loaderStallFrames != stalls {
		t.Errorf("duplicate tick must not sample: frame=%d stall=%d->%d", m.loaderFrame, stalls, m.loaderStallFrames)
	}
}

// ticks armed by callers that do not know the frame are unkeyed: always
// accepted, and the chain they start displaces the one already running.
func TestLoaderTick_UnkeyedDisplacesOldChain(t *testing.T) {
	m := tickingModel(t)
	old := loaderTickMsg{frame: m.loaderFrame, keyed: true}

	nm, cmd := m.Update(loaderTickMsg{})
	m = nm.(Model)
	if cmd == nil || m.loaderFrame != 1 {
		t.Fatalf("unkeyed tick must be accepted: frame=%d", m.loaderFrame)
	}
	if _, cmd := m.Update(old); cmd != nil {
		t.Error("tick of the displaced chain must die")
	}
}

func TestGlowTick_DuplicateChainCollapses(t *testing.T) {
	m := newTestModel(t)
	m.role = "queen"
	tick := glowTickMsg{frame: m.glowFrame, keyed: true}

	nm, cmd := m.Update(tick)
	m = nm.(Model)
	if cmd == nil || m.glowFrame != 1 {
		t.Fatalf("first tick must advance and rearm: frame=%d", m.glowFrame)
	}
	if nm, cmd = m.Update(tick); cmd != nil || nm.(Model).glowFrame != 1 {
		t.Error("duplicate glow tick must die without advancing")
	}
}

func TestTouchesInput(t *testing.T) {
	for _, msg := range []any{streamDeltaMsg{}, loaderTickMsg{}, glowTickMsg{}, agentTickMsg{}, atIndexMsg{}} {
		if touchesInput(msg) {
			t.Errorf("%T must skip the input sync", msg)
		}
	}
	for _, msg := range []any{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}, turnDoneMsg{}, struct{}{}} {
		if !touchesInput(msg) {
			t.Errorf("%T must sync the input", msg)
		}
	}
}
