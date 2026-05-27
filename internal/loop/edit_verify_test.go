package loop

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func newVerifyEngine() *Engine {
	return &Engine{
		editsByFile:        make(map[string]int),
		nudgedEditNoVerify: make(map[string]bool),
	}
}

// freshBlocks returns a one-element tool_result block slice. Warnings are
// prepended to a tool_result block's Content, so tests need a fresh block
// per call to inspect output independently.
func freshBlocks(id string) []types.ContentBlock {
	return []types.ContentBlock{{
		Type:   types.BlockToolResult,
		Result: &types.ToolResult{UseID: id, Content: "ok"},
	}}
}

func editUse(id, path string) types.ToolUse {
	return types.ToolUse{ID: id, Name: "edit", Input: map[string]any{"path": path}}
}

func editResult(id string, isErr bool) types.ToolResult {
	return types.ToolResult{UseID: id, Content: "ok", IsError: isErr}
}

func TestEditVerify_NudgeAfterThreshold(t *testing.T) {
	e := newVerifyEngine()
	path := "/x/foo.go"

	// 1st + 2nd edit: no nudge yet.
	for i := 0; i < 2; i++ {
		uses := []types.ToolUse{editUse("u", path)}
		results := []types.ToolResult{editResult("u", false)}
		blocks := observeEditNoVerify(e, uses, results, freshBlocks("u"))
		if hasVerifyNudge(blocks) {
			t.Fatalf("nudge fired too early on edit #%d", i+1)
		}
	}
	// 3rd edit: nudge fires.
	uses := []types.ToolUse{editUse("u", path)}
	results := []types.ToolResult{editResult("u", false)}
	blocks := observeEditNoVerify(e, uses, results, freshBlocks("u"))
	if !hasVerifyNudge(blocks) {
		t.Fatalf("expected nudge on 3rd edit, got: %v", blocks)
	}
}

func TestEditVerify_NudgeOnlyOncePerFile(t *testing.T) {
	e := newVerifyEngine()
	path := "/x/foo.go"
	// push to threshold
	for i := 0; i < 3; i++ {
		uses := []types.ToolUse{editUse("u", path)}
		results := []types.ToolResult{editResult("u", false)}
		_ = observeEditNoVerify(e, uses, results, nil)
	}
	// 4th edit: should NOT re-nudge.
	uses := []types.ToolUse{editUse("u", path)}
	results := []types.ToolResult{editResult("u", false)}
	blocks := observeEditNoVerify(e, uses, results, nil)
	if hasVerifyNudge(blocks) {
		t.Errorf("expected no repeat nudge on 4th edit")
	}
}

func TestEditVerify_BashVerifyResets(t *testing.T) {
	e := newVerifyEngine()
	path := "/x/foo.go"
	for i := 0; i < 3; i++ {
		uses := []types.ToolUse{editUse("u", path)}
		_ = observeEditNoVerify(e, uses, []types.ToolResult{editResult("u", false)}, nil)
	}
	// run `go test`: resets counter and nudge flag.
	uses := []types.ToolUse{{ID: "b", Name: "bash", Input: map[string]any{"command": "go test ./..."}}}
	_ = observeEditNoVerify(e, uses, []types.ToolResult{{UseID: "b"}}, nil)
	if e.editsByFile[path] != 0 {
		t.Errorf("expected counter reset, got %d", e.editsByFile[path])
	}
	if e.nudgedEditNoVerify[path] {
		t.Error("expected nudge flag reset")
	}
	// 3 more edits should trigger nudge again.
	for i := 0; i < 2; i++ {
		uses := []types.ToolUse{editUse("u", path)}
		_ = observeEditNoVerify(e, uses, []types.ToolResult{editResult("u", false)}, freshBlocks("u"))
	}
	uses = []types.ToolUse{editUse("u", path)}
	blocks := observeEditNoVerify(e, uses, []types.ToolResult{editResult("u", false)}, freshBlocks("u"))
	if !hasVerifyNudge(blocks) {
		t.Error("expected nudge to fire again after verify reset")
	}
}

func TestEditVerify_ReadResetsOnlyThatPath(t *testing.T) {
	e := newVerifyEngine()
	a := "/x/a.go"
	b := "/x/b.go"
	for i := 0; i < 2; i++ {
		_ = observeEditNoVerify(e,
			[]types.ToolUse{editUse("u", a), editUse("v", b)},
			[]types.ToolResult{editResult("u", false), editResult("v", false)},
			nil)
	}
	// read only a — should reset only a's counter.
	_ = observeEditNoVerify(e,
		[]types.ToolUse{{ID: "r", Name: "read", Input: map[string]any{"path": a}}},
		[]types.ToolResult{{UseID: "r"}},
		nil)
	if e.editsByFile[a] != 0 {
		t.Errorf("expected a reset, got %d", e.editsByFile[a])
	}
	if e.editsByFile[b] != 2 {
		t.Errorf("expected b unchanged at 2, got %d", e.editsByFile[b])
	}
}

func TestEditVerify_FailedEditNotCounted(t *testing.T) {
	e := newVerifyEngine()
	path := "/x/foo.go"
	for i := 0; i < 5; i++ {
		uses := []types.ToolUse{editUse("u", path)}
		results := []types.ToolResult{editResult("u", true)} // all fail
		blocks := observeEditNoVerify(e, uses, results, nil)
		if hasVerifyNudge(blocks) {
			t.Fatalf("failed edits should not count toward threshold")
		}
	}
	if e.editsByFile[path] != 0 {
		t.Errorf("expected zero count for failed edits, got %d", e.editsByFile[path])
	}
}

func TestEditVerify_NonVerifyBashIgnored(t *testing.T) {
	// `ls` is not a verify command; counter should keep climbing.
	e := newVerifyEngine()
	path := "/x/foo.go"
	for i := 0; i < 2; i++ {
		_ = observeEditNoVerify(e,
			[]types.ToolUse{editUse("u", path)},
			[]types.ToolResult{editResult("u", false)},
			nil)
	}
	_ = observeEditNoVerify(e,
		[]types.ToolUse{{ID: "b", Name: "bash", Input: map[string]any{"command": "ls -la"}}},
		[]types.ToolResult{{UseID: "b"}},
		nil)
	if e.editsByFile[path] != 2 {
		t.Errorf("ls should not reset counter, got %d", e.editsByFile[path])
	}
}

func hasVerifyNudge(blocks []types.ContentBlock) bool {
	for _, b := range blocks {
		text := b.Text
		if b.Result != nil {
			text += b.Result.Content
		}
		if strings.Contains(text, "[verify]") {
			return true
		}
	}
	return false
}
