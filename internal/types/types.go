// Package types holds the agent-owned message and session model.
//
// Provider adapters translate to/from these — providers do NOT leak into the
// rest of the codebase. Keep this package small and stable; everything
// downstream depends on it.
package types

import "time"

// Role of a message in the conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ContentBlock is a typed piece of message content. Assistant messages may
// contain a mix of text and tool_use blocks. User messages may carry text,
// tool_result, and image blocks.
type ContentBlock struct {
	Type      BlockType   `json:"type"`
	Text      string      `json:"text,omitempty"`
	Use       *ToolUse    `json:"tool_use,omitempty"`
	Result    *ToolResult `json:"tool_result,omitempty"`
	MediaType string      `json:"media_type,omitempty"` // for BlockImage e.g. "image/png"
	Data      []byte      `json:"data,omitempty"`       // raw bytes (provider adapters base64-encode)
}

type BlockType string

const (
	BlockText       BlockType = "text"
	BlockToolUse    BlockType = "tool_use"
	BlockToolResult BlockType = "tool_result"
	BlockImage      BlockType = "image"
	// BlockThinking carries provider-emitted chain-of-thought / reasoning
	// summaries. Stored locally for display but NEVER sent back to providers
	// (wire adapters drop unknown block types via their case-based switches).
	BlockThinking BlockType = "thinking"
)

// ToolUse is the model's request to invoke a tool.
type ToolUse struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ToolResult is the agent's response carrying the tool's output.
type ToolResult struct {
	UseID   string `json:"use_id"`
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// Message is one turn in the conversation. Content is an ordered list of
// blocks; concatenation of plain text blocks recreates the natural text.
type Message struct {
	ID       string         `json:"id"`
	ParentID string         `json:"parent_id,omitempty"`
	Role     Role           `json:"role"`
	Content  []ContentBlock `json:"content"`
	Time     time.Time      `json:"ts"`
	// Ephemeral marks a scrollback-only UI echo (slash-command confirmations,
	// queued/steer notices). Shown in the TUI but never replayed to the LLM,
	// so the model can't mistake "(/new done)" for a finish signal and parrot it.
	Ephemeral bool `json:"ephemeral,omitempty"`
}

// Session is a tree of messages. Each Message has a ParentID; the root has
// empty ParentID. Branches share a parent and let users explore alternate
// agent paths without losing prior work.
type Session struct {
	ID       string    `json:"id"`
	Created  time.Time `json:"created"`
	Cwd      string    `json:"cwd"`
	Model    string    `json:"model"`
	Provider string    `json:"provider"`
}

// MessageNode is a session-graph node held in memory during a live session.
// Children are computed by traversal; not persisted.
type MessageNode struct {
	Msg      Message
	Children []*MessageNode
}
