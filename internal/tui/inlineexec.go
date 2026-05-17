// Inline ! / !! shell exec from the editor. The default bang behavior is
// configurable (cfg.ShellBangSilent — silent by default). `!!cmd` always
// runs in the opposite mode, so users can override per-invocation either
// way. Execution goes through the same shell tool the model uses so the
// permission/sandbox model stays consistent.
package tui

import (
	"context"
	"strings"

	"github.com/elhenro/bee/internal/tools"
)

// inlineExecResult is the outcome of an inline ! or !! shell call.
type inlineExecResult struct {
	Output  string
	Silent  bool // resolved at dispatch time (default XOR doubleBang)
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

// parseInlinePrefix returns (cmd, bangCount, isInline). bangCount is 1 for
// `!cmd`, 2 for `!!cmd`. The caller resolves silent vs forward using the
// default behavior + bangCount (count=2 inverts the default).
func parseInlinePrefix(text string) (cmd string, bangCount int, isInline bool) {
	if strings.HasPrefix(text, "!!") {
		return strings.TrimPrefix(text, "!!"), 2, true
	}
	if strings.HasPrefix(text, "!") {
		return strings.TrimPrefix(text, "!"), 1, true
	}
	return text, 0, false
}

// resolveBangSilent returns whether a bang command should suppress the LLM
// turn. defaultSilent is the user's configured default; bangCount=2 inverts
// it so `!!` is always the escape hatch from whichever mode is active.
func resolveBangSilent(defaultSilent bool, bangCount int) bool {
	if bangCount == 2 {
		return !defaultSilent
	}
	return defaultSilent
}
