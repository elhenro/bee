package loop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/llm"
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

// trySend delivers v without blocking: a slow consumer drops it rather than
// stalling the loop. A nil channel drops everything.
func trySend[T any](ch chan<- T, v T) {
	select {
	case ch <- v:
	default:
	}
}

func (e *Engine) appendMessage(ctx context.Context, m types.Message) error {
	var err error
	if e.Sessions != nil {
		err = e.Sessions.Append(ctx, m)
	}
	// fan out to a live UI so tool_use / tool_result cards render mid-Run.
	// skip user role: the TUI shows an optimistic copy before Run starts.
	if m.Role != types.RoleUser {
		trySend(e.LiveMsgCh, m)
	}
	return err
}

// push persists m and adds it to the Run's transcript.
func (e *Engine) push(ctx context.Context, res *RunResult, m types.Message) error {
	if err := e.appendMessage(ctx, m); err != nil {
		return err
	}
	res.Messages = append(res.Messages, m)
	return nil
}

// newMessage builds a message with a fresh id and timestamp.
func newMessage(role types.Role, parentID string, content ...types.ContentBlock) types.Message {
	return types.Message{ID: newID(), ParentID: parentID, Role: role, Content: content, Time: time.Now().UTC()}
}

func textBlock(text string) types.ContentBlock {
	return types.ContentBlock{Type: types.BlockText, Text: text}
}

// textMessage is newMessage with a single text block.
func textMessage(role types.Role, parentID, text string) types.Message {
	return newMessage(role, parentID, textBlock(text))
}

// plainMessage is a bare text message for requests and views: no id, no
// timestamp, never persisted.
func plainMessage(role types.Role, text string) types.Message {
	return types.Message{Role: role, Content: []types.ContentBlock{textBlock(text)}}
}

func lastID(ms []types.Message) string {
	// skip trailing ephemeral notices (e.g. auto-compact cards) so persisted
	// messages chain ParentID to the last real message, not a UI-only echo.
	for i := len(ms) - 1; i >= 0; i-- {
		if ms[i].Ephemeral {
			continue
		}
		return ms[i].ID
	}
	return ""
}

func newID() string { return uuid.NewString() }

// hasBlock reports whether m carries a block of type t.
func hasBlock(m types.Message, t types.BlockType) bool {
	return slices.ContainsFunc(m.Content, func(b types.ContentBlock) bool { return b.Type == t })
}

// containsAny reports whether s contains any of subs.
func containsAny(s string, subs ...string) bool {
	return slices.ContainsFunc(subs, func(sub string) bool { return strings.Contains(s, sub) })
}

// hash8 is a short stable digest for fingerprints and dedupe keys.
func hash8(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// looksLikeAttemptedToolCall heuristically detects when the final text from
// a turn contains a tool-call attempt that the textmode parser couldn't
// recognize: typical when a model emits bare JSON envelopes
// (`{"type":"shell",...}`) or unclosed XML (`<shell>` no body). Used by the
// stall recovery in turn_run.go to fire a format-correction nudge instead of
// terminating silently.
//
// Fenced regions (triple-backtick code blocks) are stripped before scanning
// so the model can legitimately quote tool-call shapes inside a code fence
// when explaining usage without triggering the nudge.
func looksLikeAttemptedToolCall(text string, specs []llm.ToolSpec) bool {
	if text == "" || len(specs) == 0 {
		return false
	}
	low := strings.ToLower(stripFencedRegions(text))
	_, ok := mentionedSpec(low, specs, containsAny(low, `"type"`, `"name"`))
	return ok
}

// mentionedSpec returns the first spec that lowercased text refers to as an
// XML-ish opening tag, a quoted JSON value (quoted: only when it sits in a
// type/name field) or a markdown summary slip: `(toolname)` paren mention.
// Qwen3-A3B drift — model writes a prose recap (`(edit) path - did X`,
// `(read)\n path/to/file`) instead of emitting the envelope.
func mentionedSpec(low string, specs []llm.ToolSpec, quoted bool) (llm.ToolSpec, bool) {
	for _, s := range specs {
		name := strings.ToLower(s.Name)
		if name == "" {
			continue
		}
		if strings.Contains(low, "<"+name) ||
			quoted && strings.Contains(low, `"`+name+`"`) ||
			strings.Contains(low, "("+name+")") {
			return s, true
		}
	}
	return llm.ToolSpec{}, false
}

// stripFencedRegions removes ```...``` blocks from s. Unbalanced opening
// fence drops the rest of the string (treats it as one open fence). Inline
// single-backtick spans are left intact.
func stripFencedRegions(s string) string {
	const fence = "```"
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		j := strings.Index(s[i:], fence)
		if j < 0 {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(s[i : i+j])
		k := strings.Index(s[i+j+len(fence):], fence)
		if k < 0 {
			break
		}
		i = i + j + len(fence) + k + len(fence)
	}
	return b.String()
}

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

// expandAtPathsInContent returns content with `@path` expansions applied to its
// text blocks. Image / tool blocks pass through untouched. Empty cwd disables
// expansion. The caller's slice is never written: a UI may share it.
func expandAtPathsInContent(content []types.ContentBlock, cwd string) []types.ContentBlock {
	if cwd == "" {
		return content
	}
	out := append([]types.ContentBlock(nil), content...)
	for i, c := range out {
		if c.Type != types.BlockText || c.Text == "" {
			continue
		}
		out[i].Text = prompt.ExpandAtPaths(c.Text, cwd)
	}
	return out
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
