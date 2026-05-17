package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/loop"
)

func TestDisplayModel_PrefixesBareIDs(t *testing.T) {
	cases := []struct {
		provider, model, want string
	}{
		{"ollama", "llama3.1:8b", "ollama/llama3.1:8b"},
		{"lmstudio", "qwen2.5-coder", "lmstudio/qwen2.5-coder"},
		{"openai", "gpt-5-codex", "openai/gpt-5-codex"},
		// already-namespaced ids stay as-is.
		{"openrouter", "anthropic/claude-opus-4-7", "anthropic/claude-opus-4-7"},
		{"openrouter", "deepseek/deepseek-v4-flash", "deepseek/deepseek-v4-flash"},
	}
	for _, c := range cases {
		eng := &loop.Engine{Cfg: config.Config{DefaultProvider: c.provider}}
		m := NewModel(eng, "/tmp/work", c.model, "workspace-write", caveman.Default)
		if got := m.displayModel(); got != c.want {
			t.Errorf("provider=%q model=%q got=%q want=%q", c.provider, c.model, got, c.want)
		}
	}
}

// no engine → bare model. headless and pre-init paths shouldn't crash on a
// missing provider.
func TestDisplayModel_NilEngineReturnsBare(t *testing.T) {
	m := NewModel(nil, "/tmp/work", "llama3.1:8b", "workspace-write", caveman.Default)
	if got := m.displayModel(); got != "llama3.1:8b" {
		t.Errorf("nil engine: got %q want %q", got, "llama3.1:8b")
	}
}

func TestFormatElapsed(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0.0s"},
		{350 * time.Millisecond, "0.3s"},
		{1500 * time.Millisecond, "1.5s"},
		{12 * time.Second, "12s"},
		{75 * time.Second, "1m 15s"},
		{2*time.Hour + 4*time.Minute, "2h 04m"},
	}
	for _, c := range cases {
		if got := formatElapsed(c.d); got != c.want {
			t.Errorf("formatElapsed(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// while streaming the chip reads the live wall-clock; final mode persists
// lastTurnDuration. Empty state when neither applies — top bar stays quiet.
func TestRenderTurnTimer_LiveAndFinal(t *testing.T) {
	m := NewModel(nil, "/tmp/work", "gpt-4o-mini", "workspace-write", caveman.Default)
	if got := m.renderTurnTimer(); got != "" {
		t.Errorf("idle/empty: got %q want empty", got)
	}
	m.lastTurnDuration = 12 * time.Second
	if got := stripANSI(m.renderTurnTimer()); !strings.Contains(got, "12s") {
		t.Errorf("final: got %q want contains 12s", got)
	}
	m.state = StateStreaming
	m.turnStartedAt = time.Now().Add(-3500 * time.Millisecond)
	got := stripANSI(m.renderTurnTimer())
	if !strings.Contains(got, "s") || !strings.HasPrefix(got, "⏱") {
		t.Errorf("live: got %q want timer chip", got)
	}
}
