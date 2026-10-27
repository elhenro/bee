package llm

import "testing"

func TestContextWindow_SnapshotsAndFamilies(t *testing.T) {
	ResetLiveContextLengths()
	defer ResetLiveContextLengths()

	cases := []struct {
		id   string
		want int
	}{
		// dated and alias suffixes resolve to the family entry
		{"claude-sonnet-4-5-20250929", 200000},
		{"claude-sonnet-4-5-latest", 200000},
		{"anthropic/claude-sonnet-4.5", 200000},
		{"gpt-4o-2024-08-06", 128000},
		{"gpt-4o-mini-2024-07-18", 128000},
		// longest prefix wins, only on a segment boundary
		{"o1-preview", 200000},
		{"o1-mini-2024-09-12", 128000},
		{"o10", 0},
		{"deepseek-chatty", 0},
		// current claude family
		{"claude-sonnet-5-5", 1000000},
		{"claude-opus-5-5-20261001", 1000000},
		{"claude-fable-5-1", 1000000},
		{"claude-haiku-5-5", 1000000},
		// fallback-list ids resolve without a table entry
		{"gpt-5.5", 272000},
		{"MiniMax-M2.7", 200000},
		// unknown claude ids get the conservative default, other unknowns stay 0
		{"claude-brand-new-9", claudeDefaultWindow},
		{"anthropic/claude-3.5-sonnet", claudeDefaultWindow},
		{"mystery-model-20250929", 0},
	}
	for _, c := range cases {
		if got := ContextWindow(c.id); got != c.want {
			t.Errorf("ContextWindow(%q) = %d, want %d", c.id, got, c.want)
		}
	}
}

// a window the API reported outranks the table, for the dated alias too.
func TestContextWindow_LiveBeatsTableForSnapshot(t *testing.T) {
	ResetLiveContextLengths()
	defer ResetLiveContextLengths()

	RememberContextLength("claude-sonnet-4-5", 555000)
	if got := ContextWindow("claude-sonnet-4-5-20250929"); got != 555000 {
		t.Errorf("got %d, want live 555000", got)
	}
	RememberContextLength("claude-sonnet-4-5-20250929", 777000)
	if got := ContextWindow("claude-sonnet-4-5-20250929"); got != 777000 {
		t.Errorf("exact live id got %d, want 777000", got)
	}
}
