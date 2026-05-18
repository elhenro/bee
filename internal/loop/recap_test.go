package loop

import (
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func TestParseRecapOutput(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Did X. Next: do Y.", "Did X. Next: do Y."},
		{"trim_prefix", "Recap: Did X. Next: do Y.", "Did X. Next: do Y."},
		{"summary_prefix", "summary: Refactored loop.", "Refactored loop."},
		{"strip_quotes", `"Refactored loop. Next: tests."`, "Refactored loop. Next: tests."},
		{"skip_sentinel", "skip", ""},
		{"skip_padded", "  SKIP  ", ""},
		{"skipped_variant", "skipped", ""},
		{"skipped_parens", "(skipped)", ""},
		{"skip_parens", "(skip)", ""},
		{"collapse_newlines", "Line one.\nLine two.", "Line one. Line two."},
		{"collapse_double_space", "Foo.  Bar.", "Foo. Bar."},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseRecapOutput(tc.in)
			if got != tc.want {
				t.Fatalf("parseRecapOutput(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExtractRecapInput(t *testing.T) {
	asst := func(text string) types.Message {
		return types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: text}},
		}
	}
	user := func(text string) types.Message {
		return types.Message{
			Role:    types.RoleUser,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: text}},
		}
	}
	tool := types.Message{
		Role:    types.RoleTool,
		Content: []types.ContentBlock{{Type: types.BlockToolResult, Result: &types.ToolResult{Content: "ignored"}}},
	}
	thinking := types.Message{
		Role:    types.RoleAssistant,
		Content: []types.ContentBlock{{Type: types.BlockThinking, Text: "secret thoughts"}},
	}

	// only assistant text from the trailing turn should be extracted; the
	// scan stops once we hit the previous user message.
	msgs := []types.Message{
		user("first"),
		asst("first reply"),
		user("second"),
		thinking,
		tool,
		asst("did the work"),
	}
	got := extractRecapInput(msgs)
	want := "did the work"
	if got != want {
		t.Fatalf("extractRecapInput trailing turn = %q, want %q", got, want)
	}

	// empty / no-assistant input returns empty.
	if extractRecapInput(nil) != "" {
		t.Fatal("nil msgs should yield empty input")
	}
	if extractRecapInput([]types.Message{user("hi")}) != "" {
		t.Fatal("user-only msgs should yield empty input")
	}
}
