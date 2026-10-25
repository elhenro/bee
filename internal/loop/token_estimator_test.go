package loop

import (
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
