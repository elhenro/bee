package session

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func toolExchange(payload types.ContentBlock) []types.Message {
	return []types.Message{
		{ID: "1", Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "hi"}}},
		{ID: "2", Role: types.RoleAssistant, ParentID: "1", Content: []types.ContentBlock{{Type: types.BlockToolUse, Use: &types.ToolUse{ID: "u1", Name: "read"}}}},
		{ID: "3", Role: types.RoleTool, ParentID: "2", Content: []types.ContentBlock{
			{Type: types.BlockToolResult, Result: &types.ToolResult{UseID: "u1", Content: "ok"}}, payload}},
		{ID: "4", Role: types.RoleAssistant, ParentID: "3", Content: []types.ContentBlock{{Type: types.BlockText, Text: "done"}}},
	}
}

// an oversized image must not make the reader drop the whole tool message.
func TestAppendOversizedImageKeepsMessage(t *testing.T) {
	t.Setenv("BEE_SESSIONS_DIR", t.TempDir())
	r, err := Open("big-image")
	if err != nil {
		t.Fatal(err)
	}
	img := types.ContentBlock{Type: types.BlockImage, MediaType: "image/png", Data: make([]byte, 9*1024*1024)}
	for _, m := range toolExchange(img) {
		if err := r.Append(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
	_ = r.Close()
	got, err := Read("big-image")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("read back %d messages, want 4", len(got))
	}
	for _, c := range got[2].Content {
		if c.Type == types.BlockImage {
			t.Fatal("oversized image should be replaced by a placeholder")
		}
	}
	if got[2].Content[0].Result == nil {
		t.Fatal("tool_result block must survive")
	}
}

// text that cannot fit is refused loudly instead of vanishing on read.
func TestAppendOversizedTextErrors(t *testing.T) {
	t.Setenv("BEE_SESSIONS_DIR", t.TempDir())
	r, err := Open("big-text")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	m := types.Message{ID: "1", Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: strings.Repeat("x", maxLineBytes)}}}
	if err := r.Append(context.Background(), m); err == nil {
		t.Fatal("want an error for a message over the line limit")
	}
}
