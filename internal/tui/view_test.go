package tui

import (
	"testing"

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
