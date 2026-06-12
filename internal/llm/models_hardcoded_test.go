package llm

import (
	"testing"
)

// hardcoded `contextLengths` table covers the openrouter-routed MiniMax models
// the user hit, plus the bare trailing segment so a local TOML config that
// only specifies the model name (no vendor prefix) still gets a non-zero
// context window. Both forms resolve via the slash-aware ContextWindow lookup.
func TestContextWindow_MiniMaxHardcoded(t *testing.T) {
	ResetLiveContextLengths()
	defer ResetLiveContextLengths()

	cases := []struct {
		id   string
		want int
	}{
		{"minimax/MiniMax-M3", 200000},
		{"minimax/MiniMax-M2", 200000},
		{"MiniMax-M3", 200000},
		{"MiniMax-M2", 200000},
	}
	for _, c := range cases {
		if got := ContextWindow(c.id); got != c.want {
			t.Errorf("ContextWindow(%q) = %d, want %d", c.id, got, c.want)
		}
	}
}

// unknown model id (not in the hardcoded table, never learned from the API)
// must return 0 so callers can show a "?" instead of a silently-wrong 0%.
func TestContextWindow_UnknownReturnsZero(t *testing.T) {
	ResetLiveContextLengths()
	defer ResetLiveContextLengths()

	if got := ContextWindow("totally-fake-model-9999"); got != 0 {
		t.Errorf("ContextWindow(unknown) = %d, want 0", got)
	}
	if got := ContextWindow("minimax/MiniMax-X99-future"); got != 0 {
		t.Errorf("ContextWindow(unknown with vendor) = %d, want 0", got)
	}
	if got := ContextWindow(""); got != 0 {
		t.Errorf("ContextWindow(empty) = %d, want 0", got)
	}
}
