package loop

import (
	"context"
	"strings"
	"testing"

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
	if stats.Duration <= 0 {
		t.Errorf("stats.Duration should be positive, got %v", stats.Duration)
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
