package loop

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// compactStubProvider returns a fixed summary. Named to avoid clashing with
// stubProvider in turn_test.go (same package).
type compactStubProvider struct{ summary string }

func (s *compactStubProvider) Name() string { return "stub" }
func (s *compactStubProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	ch := make(chan llm.Event, 2)
	go func() {
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: s.summary}
		ch <- llm.Event{Type: llm.EventDone}
		close(ch)
	}()
	return ch, nil
}

func mkMsg(role types.Role, text string) types.Message {
	return types.Message{
		Role:    role,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: text}},
	}
}

func TestCompact_PreservesTail(t *testing.T) {
	p := &compactStubProvider{summary: "SUMMARY"}
	msgs := []types.Message{
		mkMsg(types.RoleUser, "1"),
		mkMsg(types.RoleAssistant, "2"),
		mkMsg(types.RoleUser, "3"),
		mkMsg(types.RoleAssistant, "4"),
		mkMsg(types.RoleUser, "5"),
		mkMsg(types.RoleAssistant, "6"),
		mkMsg(types.RoleUser, "7"),
		mkMsg(types.RoleAssistant, "8"),
	}
	out, stats, err := Compact(context.Background(), p, "stub-model", msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1+PreserveTail {
		t.Fatalf("want %d msgs (1 summary + %d tail), got %d", 1+PreserveTail, PreserveTail, len(out))
	}
	if !strings.Contains(out[0].Content[0].Text, "SUMMARY") {
		t.Errorf("first msg should be summary, got %q", out[0].Content[0].Text)
	}
	if out[len(out)-1].Content[0].Text != "8" {
		t.Errorf("last msg should be \"8\", got %q", out[len(out)-1].Content[0].Text)
	}
	if stats.BeforeMsgs != len(msgs) || stats.AfterMsgs != len(out) {
		t.Errorf("stats msg counts: before=%d after=%d, want %d/%d", stats.BeforeMsgs, stats.AfterMsgs, len(msgs), len(out))
	}
	if stats.Duration < 0 {
		t.Errorf("stats.Duration should be non-negative, got %v", stats.Duration)
	}
}

func mkToolUse(id string) types.Message {
	return types.Message{
		Role:    types.RoleAssistant,
		Content: []types.ContentBlock{{Type: types.BlockToolUse, Use: &types.ToolUse{ID: id, Name: "read"}}},
	}
}

func mkToolResult(id string) types.Message {
	return types.Message{
		Role:    types.RoleTool,
		Content: []types.ContentBlock{{Type: types.BlockToolResult, Result: &types.ToolResult{UseID: id, Content: "ok"}}},
	}
}

// preserved tail must never open on an orphaned tool result: its assistant
// tool_use would be dropped into the summary, producing a role:"tool" wire
// message with no preceding tool_calls (provider 400: "A tool message must
// follow an assistant or tool message").
func TestCompact_BoundaryNeverSplitsToolPair(t *testing.T) {
	p := &compactStubProvider{summary: "SUMMARY"}
	// default cut = len-4 = 4 lands ON the tool_result. its tool_use sits at
	// index 3; without the walk-back the result is orphaned into the tail.
	msgs := []types.Message{
		mkMsg(types.RoleUser, "1"),
		mkMsg(types.RoleAssistant, "2"),
		mkMsg(types.RoleUser, "3"),
		mkToolUse("call-1"),    // index 3
		mkToolResult("call-1"), // index 4  <- naive cut starts here (orphan)
		mkMsg(types.RoleUser, "5"),
		mkMsg(types.RoleAssistant, "6"),
		mkMsg(types.RoleUser, "7"),
	}
	out, _, err := Compact(context.Background(), p, "stub", msgs)
	if err != nil {
		t.Fatal(err)
	}
	if hasToolResult(out[1]) {
		t.Fatalf("preserved tail opens on orphan tool result: %+v", out[1].Content[0])
	}
}

func TestCompact_NoChangeWhenSmall(t *testing.T) {
	p := &compactStubProvider{summary: "X"}
	msgs := []types.Message{
		mkMsg(types.RoleUser, "1"),
		mkMsg(types.RoleAssistant, "2"),
	}
	out, stats, _ := Compact(context.Background(), p, "stub", msgs)
	if len(out) != 2 {
		t.Errorf("small history should pass through, got %d", len(out))
	}
	if stats.BeforeMsgs != 2 || stats.AfterMsgs != 2 {
		t.Errorf("no-op compaction should report unchanged counts, got before=%d after=%d", stats.BeforeMsgs, stats.AfterMsgs)
	}
}

func TestShouldAutoCompact(t *testing.T) {
	msgs := []types.Message{mkMsg(types.RoleUser, strings.Repeat("x", 4000))}
	if !ShouldAutoCompact("system", msgs, 1000, 0.8) {
		t.Error("want true when over budget")
	}
	if ShouldAutoCompact("system", msgs, 1_000_000, 0.8) {
		t.Error("want false when budget huge")
	}
	if ShouldAutoCompact("system", msgs, 0, 0.8) {
		t.Error("want false when budget=0 (disabled)")
	}
}

func TestShouldAutoCompactWithUsage_PrefersActualTokens(t *testing.T) {
	// tiny history but provider says we're at 90% of a 1000-token budget.
	// estimate-based check would miss; usage-based check trips.
	msgs := []types.Message{mkMsg(types.RoleUser, "hi")}
	if !ShouldAutoCompactWithUsage("system", msgs, 900, 1000, 0.8) {
		t.Error("want true when actual input tokens cross threshold")
	}
	if ShouldAutoCompactWithUsage("system", msgs, 500, 1000, 0.8) {
		t.Error("want false when actual tokens under threshold")
	}
}

func TestFlattenForSummary_IncludesToolResults(t *testing.T) {
	m := types.Message{
		Role: types.RoleTool,
		Content: []types.ContentBlock{
			{Type: types.BlockThinking, Text: "secret scratch reasoning"},
			{Type: types.BlockToolResult, Result: &types.ToolResult{UseID: "1", Content: "func add() {} in math.go"}},
		},
	}
	got := flattenForSummary(m)
	if !strings.Contains(got, "math.go") {
		t.Errorf("summary input must include tool result, got %q", got)
	}
	if strings.Contains(got, "scratch reasoning") {
		t.Errorf("thinking blocks must be dropped from summary input, got %q", got)
	}
}

func TestFlattenForSummary_TruncatesHugeResult(t *testing.T) {
	huge := strings.Repeat("x", 50_000)
	m := types.Message{
		Role:    types.RoleTool,
		Content: []types.ContentBlock{{Type: types.BlockToolResult, Result: &types.ToolResult{UseID: "1", Content: huge}}},
	}
	got := flattenForSummary(m)
	if len(got) > summaryToolResultCap+64 {
		t.Errorf("result should be capped near %d, got %d chars", summaryToolResultCap, len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("truncation should be marked, got tail %q", got[len(got)-32:])
	}
}

func TestCompactWorthwhile(t *testing.T) {
	big := mkMsg(types.RoleUser, strings.Repeat("x", 40_000)) // ~10k tokens
	small := mkMsg(types.RoleUser, "hi")
	// older slice (everything before PreserveTail) holds the big msg → worth it.
	worth := append([]types.Message{big}, repeatMsg(small, PreserveTail)...)
	if !compactWorthwhile(worth, 32768) {
		t.Error("want worthwhile when older slice is large")
	}
	// only the tiny tail remains compactible → not worth re-compacting.
	notWorth := repeatMsg(small, PreserveTail+1)
	if compactWorthwhile(notWorth, 32768) {
		t.Error("want NOT worthwhile when older slice is tiny (overhead-bound)")
	}
	if compactWorthwhile(repeatMsg(small, PreserveTail), 32768) {
		t.Error("want NOT worthwhile when nothing past the preserved tail")
	}
}

func repeatMsg(m types.Message, n int) []types.Message {
	out := make([]types.Message, n)
	for i := range out {
		out[i] = m
	}
	return out
}

func TestTruncate_RuneSafe(t *testing.T) {
	// each "世" is 3 bytes; 10 runes = 30 bytes. Byte-slicing at 5 would
	// split the 2nd rune and emit invalid UTF-8.
	got := truncate(strings.Repeat("世", 10), 5)
	if !utf8.ValidString(got) {
		t.Errorf("truncate split a rune, invalid UTF-8: %q", got)
	}
	if !strings.HasPrefix(got, strings.Repeat("世", 5)) {
		t.Errorf("want first 5 runes preserved, got %q", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("cut should be marked, got %q", got)
	}
	// 5 runes (6 bytes) is at the cap, not over it — pass through unchanged.
	// Byte-based truncate wrongly clipped this to 4 runes.
	if out := truncate("héllo", 5); out != "héllo" {
		t.Errorf("5-rune string should pass through unchanged, got %q", out)
	}
}

func TestEstimateMessageTokens_CountsToolBlocks(t *testing.T) {
	bigOutput := strings.Repeat("x", 4000)
	m := types.Message{
		Role: types.RoleTool,
		Content: []types.ContentBlock{{
			Type:   types.BlockToolResult,
			Result: &types.ToolResult{UseID: "1", Content: bigOutput},
		}},
	}
	got := estimateMessageTokens(m)
	if got < 500 {
		t.Errorf("tool_result content should drive estimate, got %d for 4000-char output", got)
	}
}
