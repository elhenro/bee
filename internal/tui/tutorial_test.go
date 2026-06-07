package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

// enter is the key the walkthrough advances on.
var enterKey = tea.KeyMsg{Type: tea.KeyEnter}

// TestTutorialFlow_StartToFinish_NoContextLeak drives the gate → run → finish
// path and asserts the safety property: every injected card is ephemeral, so
// nonEphemeral() (the real-turn history filter) sees none of the fake session.
func TestTutorialFlow_StartToFinish_NoContextLeak(t *testing.T) {
	t.Setenv("BEE_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	m := newTestModel(t)
	m.tutorial = tutorialState{active: true, phase: tutPhaseGate}

	steps := len(tutorialSteps())
	for i := 0; i < steps*3+5 && m.tutorial.active; i++ {
		nm, _ := m.Update(enterKey) // enter starts, skips typing, advances, finishes
		m = nm.(Model)
	}

	if m.tutorial.active {
		t.Fatalf("tutorial still active after driving enters")
	}
	if len(m.messages) == 0 {
		t.Fatalf("expected fake cards in scrollback")
	}
	for i, msg := range m.messages {
		if !msg.Ephemeral {
			t.Errorf("message %d not ephemeral — would leak into LLM context: %+v", i, msg)
		}
	}
	if leftover := nonEphemeral(m.messages); len(leftover) != 0 {
		t.Errorf("tutorial leaked %d non-ephemeral messages into context", len(leftover))
	}
}

// TestTutorialTypewriter_RevealsAndSettles pumps the tick and checks the typed
// assistant text settles into a full message.
func TestTutorialTypewriter_RevealsAndSettles(t *testing.T) {
	t.Setenv("BEE_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	m := newTestModel(t)
	m.tutorial = tutorialState{active: true, phase: tutPhaseGate}

	nm, _ := m.Update(enterKey) // start tour → step 0 typing
	m = nm.(Model)
	if !m.tutorial.typing {
		t.Fatalf("expected typewriter after start")
	}
	full := m.tutorial.full

	for i := 0; i < len([]rune(full))+10 && m.tutorial.typing; i++ {
		nm, _ := m.onTutorialTick(tutorialTickMsg{})
		m = nm.(Model)
	}
	if m.tutorial.typing {
		t.Fatalf("typewriter never settled")
	}
	found := false
	for _, msg := range m.messages {
		if msg.Role != types.RoleAssistant {
			continue
		}
		for _, b := range msg.Content {
			if b.Type == types.BlockText && b.Text == full {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("settled step missing full assistant text")
	}
}

// TestTutorialGate_MaybeLater_NoPersist confirms "maybe later" closes without
// writing tutorial_done — the gate must reappear next launch.
func TestTutorialGate_MaybeLater_NoPersist(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("BEE_CONFIG", cfgPath)
	eng := &loop.Engine{Cfg: config.Defaults()}
	m := newGateModel(t, eng)

	nm, _ := m.Update(keyRune("2")) // [2] maybe later
	m = nm.(Model)

	if m.tutorial.active {
		t.Fatalf("tutorial should be dismissed")
	}
	if eng.Cfg.TutorialDone {
		t.Errorf("maybe-later must not set TutorialDone")
	}
	if _, err := os.Stat(cfgPath); err == nil {
		t.Errorf("maybe-later must not write config")
	}
}

// TestTutorialGate_Never_Persists confirms "never show again" persists the flag.
func TestTutorialGate_Never_Persists(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("BEE_CONFIG", cfgPath)
	eng := &loop.Engine{Cfg: config.Defaults()}
	m := newGateModel(t, eng)

	nm, _ := m.Update(keyRune("3")) // [3] never show again
	m = nm.(Model)

	if m.tutorial.active {
		t.Fatalf("tutorial should be dismissed")
	}
	if !eng.Cfg.TutorialDone {
		t.Errorf("never must set TutorialDone on the live config")
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("never must persist config: %v", err)
	}
	if !strings.Contains(string(data), "tutorial_done = true") {
		t.Errorf("config missing tutorial_done = true; got:\n%s", data)
	}
}

// newGateModel builds a sized model with the welcome gate active.
func newGateModel(t *testing.T, eng *loop.Engine) Model {
	t.Helper()
	m := NewModel(eng, "/tmp/proj", "test-model", "workspace-write", caveman.Default)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	m.tutorial = tutorialState{active: true, phase: tutPhaseGate}
	return m
}

func keyRune(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}
