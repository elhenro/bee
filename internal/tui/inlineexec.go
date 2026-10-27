// Inline ! / !! shell exec from the editor. The default bang behavior is
// configurable (cfg.ShellBangSilent — silent by default). `!!cmd` always
// runs in the opposite mode, so users can override per-invocation either
// way. Execution goes through the same shell tool the model uses so the
// permission/sandbox model stays consistent.
package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// inlineExecResult is the outcome of an inline ! or !! shell call.
type inlineExecResult struct {
	Output  string
	IsError bool // shell exit≠0, timeout, or tool error
}

// runInlineShell runs cmd through the bash tool registry entry and returns
// the result. registry must contain a tool named "bash".
func runInlineShell(ctx context.Context, registry *tools.Registry, cmd string) inlineExecResult {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return inlineExecResult{}
	}
	var sh tools.Tool
	ok := false
	if registry != nil {
		sh, ok = registry.Get("bash")
	}
	if !ok {
		return inlineExecResult{Output: "no bash tool registered", IsError: true}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := sh.Run(ctx, map[string]any{"command": cmd})
	if err != nil {
		return inlineExecResult{Output: err.Error(), IsError: true}
	}
	return inlineExecResult{Output: res.Content, IsError: res.IsError}
}

// parseInlinePrefix returns (cmd, bangCount, isInline). bangCount is 1 for
// `!cmd`, 2 for `!!cmd`. The caller resolves silent vs forward using the
// default behavior + bangCount (count=2 inverts the default).
func parseInlinePrefix(text string) (cmd string, bangCount int, isInline bool) {
	if after, ok := strings.CutPrefix(text, "!!"); ok {
		return after, 2, true
	}
	if after, ok := strings.CutPrefix(text, "!"); ok {
		return after, 1, true
	}
	return text, 0, false
}

// resolveBangSilent returns whether a bang command should suppress the LLM
// turn. defaultSilent is the user's configured default; bangCount=2 inverts
// it so `!!` is always the escape hatch from whichever mode is active.
func resolveBangSilent(defaultSilent bool, bangCount int) bool {
	return defaultSilent != (bangCount == 2)
}

// inlineShellDoneMsg carries a finished `!cmd` back to Update.
type inlineShellDoneMsg struct {
	payload string
	silent  bool
}

// inlineShellCmd runs cmd off the update loop: the shell tool may block on an
// approval prompt that only the loop can answer, and a slow command must not
// freeze the UI.
func (m Model) inlineShellCmd(cmd string, silent bool) tea.Cmd {
	ctx, reg := m.ctx, m.eng.Tools
	return func() tea.Msg {
		res := runInlineShell(ctx, reg, cmd)
		return inlineShellDoneMsg{payload: formatInlineShell(cmd, res.Output, res.IsError), silent: silent}
	}
}

func (m Model) onInlineShellDone(msg inlineShellDoneMsg) (tea.Model, tea.Cmd) {
	// forwarding to the model needs an idle engine; if a turn started while the
	// shell ran, keep the record local instead of overlapping runs.
	if msg.silent || m.state != StateIdle {
		m.messages = append(m.messages, types.Message{
			Role:    types.RoleUser,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: msg.payload}},
		})
		return m, m.flush()
	}
	// the LLM sees the cmd+output once; scrollback shows a single styled card.
	return m.submit(msg.payload)
}
