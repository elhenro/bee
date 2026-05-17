package loop

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/prompt"
	"github.com/elhenro/bee/internal/types"
)

// prependWarningToToolResult injects a context-warning prefix into the first
// tool_result block so the model sees it in the next turn. Mutates the
// ToolResult.Content via the pointer carried in the block. Safe when blocks
// is empty (no-op).
func prependWarningToToolResult(blocks []types.ContentBlock, warning string) []types.ContentBlock {
	for i := range blocks {
		if blocks[i].Type == types.BlockToolResult && blocks[i].Result != nil {
			blocks[i].Result.Content = warning + blocks[i].Result.Content
			return blocks
		}
	}
	return blocks
}

// toolResultBlocks renders results as ContentBlock list for a tool message.
func toolResultBlocks(rs []types.ToolResult) []types.ContentBlock {
	out := make([]types.ContentBlock, len(rs))
	for i := range rs {
		r := rs[i]
		out[i] = types.ContentBlock{Type: types.BlockToolResult, Result: &r}
	}
	return out
}

func (e *Engine) appendMessage(ctx context.Context, m types.Message) error {
	var err error
	if e.Sessions != nil {
		err = e.Sessions.Append(ctx, m)
	}
	// fan out to a live UI so tool_use / tool_result cards render mid-Run.
	// skip user role: the TUI shows an optimistic copy before Run starts.
	if e.LiveMsgCh != nil && m.Role != types.RoleUser {
		select {
		case e.LiveMsgCh <- m:
		default:
		}
	}
	return err
}

func lastID(ms []types.Message) string {
	if len(ms) == 0 {
		return ""
	}
	return ms[len(ms)-1].ID
}

func newID() string { return uuid.NewString() }

// hasThinkingOnly reports whether msg carries a thinking block but no text
// and no tool_use. provider produced reasoning then stopped — turn would
// otherwise terminate silently.
func hasThinkingOnly(msg types.Message) bool {
	sawThinking := false
	for _, b := range msg.Content {
		switch b.Type {
		case types.BlockThinking:
			sawThinking = true
		case types.BlockText:
			if strings.TrimSpace(b.Text) != "" {
				return false
			}
		case types.BlockToolUse:
			return false
		}
	}
	return sawThinking
}

// expandAtPathsInContent rewrites text blocks in-place with `@path` expansions.
// Image / tool blocks pass through untouched. Empty cwd disables expansion.
func expandAtPathsInContent(content []types.ContentBlock, cwd string) []types.ContentBlock {
	if cwd == "" {
		return content
	}
	for i, c := range content {
		if c.Type != types.BlockText || c.Text == "" {
			continue
		}
		content[i].Text = prompt.ExpandAtPaths(c.Text, cwd)
	}
	return content
}

// collectUserText concatenates the text blocks in a user message — used to
// build a memory-selection query from multimodal content.
func collectUserText(content []types.ContentBlock) string {
	var b strings.Builder
	for _, c := range content {
		if c.Type == types.BlockText {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(c.Text)
		}
	}
	return b.String()
}
