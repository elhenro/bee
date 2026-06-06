// External command launch: open the editor (ctrl+o, /edit) or any command
// (/term) as an independent tmux window so bee keeps running live in its own
// window. When not inside tmux, bee suspends and runs the command inline,
// returning to the TUI on exit.
package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elhenro/bee/internal/mux"
)

// externalDoneMsg reports the outcome of a launched external command.
type externalDoneMsg struct {
	what string
	err  error
}

// splitPathLine parses "path:line" into (path, line). A missing or
// non-numeric suffix yields line 0 and the original arg as the path.
func splitPathLine(arg string) (string, int) {
	arg = strings.TrimSpace(arg)
	if i := strings.LastIndex(arg, ":"); i > 0 {
		if n, err := strconv.Atoi(arg[i+1:]); err == nil {
			return arg[:i], n
		}
	}
	return arg, 0
}

// editorConfig returns the configured editor command, empty when no engine.
func (m Model) editorConfig() string {
	if m.eng == nil {
		return ""
	}
	return m.eng.Cfg.Editor
}

// openEditorCmd opens arg ("path[:line]", empty = cwd root) in the editor.
func (m Model) openEditorCmd(arg string) tea.Cmd {
	file, line := splitPathLine(arg)
	editor := mux.ResolveEditor(m.editorConfig())
	return m.runExternal("edit", mux.EditorCommand(editor, file, line))
}

// openTermCmd runs arg (empty = login shell) in a new window.
func (m Model) openTermCmd(arg string) tea.Cmd {
	cmd := strings.TrimSpace(arg)
	if cmd == "" {
		cmd = "${SHELL:-sh}"
	}
	return m.runExternal("term", cmd)
}

// onExternalDone surfaces a launch failure; success is silent.
func (m Model) onExternalDone(msg externalDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.lastErr = msg.what + ": " + msg.err.Error()
		m.state = StateError
	}
	return m, nil
}

// runExternal dispatches cmdline to a tmux window named name, falling back to
// a suspend-and-run when bee is not inside tmux.
func (m Model) runExternal(name, cmdline string) tea.Cmd {
	dir := m.cwd
	if mux.InTmux() {
		return func() tea.Msg {
			err := mux.OpenWindow(mux.Opts{Name: name, Dir: dir, Cmd: cmdline})
			return externalDoneMsg{what: name, err: err}
		}
	}
	return tea.ExecProcess(mux.ExecCmd(cmdline, dir), func(err error) tea.Msg {
		return externalDoneMsg{what: name, err: err}
	})
}
