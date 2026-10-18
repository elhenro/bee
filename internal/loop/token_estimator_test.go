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

// TestTokenEstimatorDefaultMatchesHeuristic pins the default implementation to
// the historical estimateMessageTokens sum, so the extraction is a pure seam.
func TestTokenEstimatorDefaultMatchesHeuristic(t *testing.T) {
	corpus := estimatorCorpus()
	want := 0
	for _, m := range corpus {
		want += estimateMessageTokens(m)
	}
	got := DefaultTokenEstimator().EstimateMessages(corpus)
	if got != want {
		t.Fatalf("EstimateMessages = %d, want %d (sum of estimateMessageTokens)", got, want)
	}
}

// TestTokenEstimatorContextMatchesMessages documents that the heuristic has no
// extra context framing cost: context and message estimates coincide.
func TestTokenEstimatorContextMatchesMessages(t *testing.T) {
	corpus := estimatorCorpus()
	l := DefaultTokenEstimator()
	if got, want := l.EstimateContext(corpus), l.EstimateMessages(corpus); got != want {
		t.Fatalf("EstimateContext = %d, want %d", got, want)
	}
}

// TestTokenEstimatorEmpty guards the empty-slice boundary.
func TestTokenEstimatorEmpty(t *testing.T) {
	if got := DefaultTokenEstimator().EstimateMessages(nil); got != 0 {
		t.Fatalf("EstimateMessages(nil) = %d, want 0", got)
	}
	if got := DefaultTokenEstimator().EstimateContext(nil); got != 0 {
		t.Fatalf("EstimateContext(nil) = %d, want 0", got)
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
