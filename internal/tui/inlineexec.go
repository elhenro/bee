// Inline ! / !! shell exec from the editor. ! runs the command and forwards
// the output to the LLM as a user message; !! runs locally and only echoes
// to scrollback (no LLM turn). Execution goes through the same shell tool
// the model uses, so the permission/sandbox model stays consistent.
package tui

import (
	"context"
	"strings"

	"github.com/elhenro/bee/internal/tools"
)

// inlineExecResult is the outcome of an inline ! or !! shell call.
type inlineExecResult struct {
	Output  string
	Silent  bool // true for !!, false for !
	IsError bool // shell exit≠0, timeout, or tool error
	Err     error
}

// runInlineShell runs cmd through the bash tool registry entry and returns
// the result. registry must contain a tool named "bash".
func runInlineShell(ctx context.Context, registry *tools.Registry, cmd string, silent bool) inlineExecResult {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return inlineExecResult{Silent: silent}
	}
	if registry == nil {
		return inlineExecResult{Output: "no bash tool registered", Silent: silent, IsError: true}
	}
	sh, ok := registry.Get("bash")
	if !ok {
		return inlineExecResult{Output: "no bash tool registered", Silent: silent, IsError: true}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := sh.Run(ctx, map[string]any{"command": cmd})
	if err != nil {
		return inlineExecResult{Output: err.Error(), Silent: silent, IsError: true, Err: err}
	}
	return inlineExecResult{Output: res.Content, Silent: silent, IsError: res.IsError}
}

// parseInlinePrefix returns (cmd, silent, isInline). If the input doesn't start
// with ! or !!, isInline is false and cmd is the original text.
func parseInlinePrefix(text string) (cmd string, silent bool, isInline bool) {
	if strings.HasPrefix(text, "!!") {
		return strings.TrimPrefix(text, "!!"), true, true
	}
	if strings.HasPrefix(text, "!") {
		return strings.TrimPrefix(text, "!"), false, true
	}
	return text, false, false
}
