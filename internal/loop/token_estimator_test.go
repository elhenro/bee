package loop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func estimatorCorpus() []types.Message {
	return []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{
			{Type: types.BlockText, Text: "hello world, please read the file"},
		}},
		{Role: types.RoleAssistant, Content: []types.ContentBlock{
			{Type: types.BlockThinking, Text: strings.Repeat("reasoning ", 40)},
			{Type: types.BlockText, Text: "looking now"},
			{Type: types.BlockToolUse, Use: &types.ToolUse{
				ID: "u1", Name: "read",
				Input: map[string]any{"path": "internal/loop/compact.go"},
			}},
		}},
		{Role: types.RoleTool, Content: []types.ContentBlock{
			{Type: types.BlockToolResult, Result: &types.ToolResult{
				UseID: "u1", Content: strings.Repeat("x", 8000),
			}},
		}},
		{Role: types.RoleAssistant, Content: []types.ContentBlock{
			{Type: types.BlockText, Text: "done"},
		}},
	}
}

// TestEstimateMessagesTokensSumsMessages pins the slice estimate to the
// per-message heuristic sum.
func TestEstimateMessagesTokensSumsMessages(t *testing.T) {
	corpus := estimatorCorpus()
	want := 0
	for _, m := range corpus {
		want += estimateMessageTokens(m)
	}
	if got := estimateMessagesTokens(corpus); got != want {
		t.Fatalf("estimateMessagesTokens = %d, want %d (sum of estimateMessageTokens)", got, want)
	}
}

// TestTokenEstimatorEmpty guards the empty-slice boundary.
func TestTokenEstimatorEmpty(t *testing.T) {
	if got := estimateMessagesTokens(nil); got != 0 {
		t.Fatalf("estimateMessagesTokens(nil) = %d, want 0", got)
	}
}

// TestTokenEstimatorNoProviderImport is a source-level import-cycle guard: the
// estimator seam must not depend on any provider package under internal/llm,
// so a future provider-specific counter can be injected without dragging the
// wire layer into the loop's core accounting.
func TestTokenEstimatorNoProviderImport(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	src, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "token_estimator.go"))
	if err != nil {
		t.Fatalf("read estimator source: %v", err)
	}
	if strings.Contains(string(src), "internal/llm") {
		t.Fatalf("token_estimator.go must not import an internal/llm provider package:\n%s", src)
	}
}

// the wire drops unsigned reasoning, so it must not count toward compaction;
// a signed block is sent back and does.
func TestEstimateThinkingOnlyCountsWhenSigned(t *testing.T) {
	think := strings.Repeat("reasoning ", 400)
	msg := func(sig string) types.Message {
		return types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{
			{Type: types.BlockThinking, Text: think, Signature: sig},
		}}
	}
	if got := estimateMessageTokens(msg("")); got != 0 {
		t.Errorf("unsigned thinking = %d tokens, want 0", got)
	}
	if got, want := estimateMessageTokens(msg("sig")), len(think)/4; got != want {
		t.Errorf("signed thinking = %d tokens, want %d", got, want)
	}
}

// the walk stays close to the size of the encoded input, without allocating.
func TestEstimateToolUseMatchesEncodedSize(t *testing.T) {
	use := &types.ToolUse{ID: "u1", Name: "apply_patch", Input: map[string]any{
		"patch": strings.Repeat("--- a/x\n+++ b/x\n", 100),
		"opts":  map[string]any{"dry": true, "ctx": 3.0, "files": []any{"a.go", "b.go"}},
	}}
	m := types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{{Type: types.BlockToolUse, Use: use}}}
	enc, err := json.Marshal(use.Input)
	if err != nil {
		t.Fatal(err)
	}
	want := (len(enc) + len(use.Name)) / 4
	got := estimateMessageTokens(m)
	if got < want*9/10 || got > want*11/10 {
		t.Errorf("estimate = %d, want within 10%% of %d", got, want)
	}
	if allocs := testing.AllocsPerRun(50, func() { estimateMessageTokens(m) }); allocs != 0 {
		t.Errorf("estimate allocates %v times per call, want 0", allocs)
	}
}
