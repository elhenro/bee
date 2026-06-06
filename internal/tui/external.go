// External command launch: open the editor (ctrl+o, /edit, /vim) or any
// command (/term) without leaving bee. Three strategies, picked at runtime:
//
//   - inside tmux: open an independent tmux window (bee keeps rendering in
//     its own window, switch with tmux tabs).
//   - unix, no tmux: job control. The child runs fullscreen; Ctrl-Z parks it
//     and drops back to live bee; ctrl+o resumes the same process where it
//     was left. One terminal window, quick toggle, no tmux required.
//   - elsewhere: plain suspend-and-run, returns to bee when the child exits.
package tui

import (
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elhenro/bee/internal/mux"
	"github.com/elhenro/bee/internal/types"
)

// ephemeral builds a transient assistant note (cleared on next turn).
func ephemeral(text string) types.Message {
	return types.Message{
		Role:      types.RoleAssistant,
		Content:   []types.ContentBlock{{Type: types.BlockText, Text: text}},
		Ephemeral: true,
	}
}

// suspendedJob is a parked (Ctrl-Z'd) child process.
type suspendedJob struct {
	pid  int
	what string
	dir  string
}

// externalDoneMsg reports the outcome of a launched external command.
type externalDoneMsg struct {
	what      string
	err       error
	suspended bool // child stopped (Ctrl-Z) and is still alive
	pid       int  // child pid when suspended (or resumed)
	dir       string
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

// runExternal dispatches cmdline: tmux window, job-control child, or plain
// suspend-and-run depending on the environment.
func (m Model) runExternal(name, cmdline string) tea.Cmd {
	dir := m.cwd
	if mux.InTmux() {
		return func() tea.Msg {
			err := mux.OpenWindow(mux.Opts{Name: name, Dir: dir, Cmd: cmdline})
			return externalDoneMsg{what: name, err: err}
		}
	}
	return m.launchJob(name, mux.ExecCmd(cmdline, dir), dir, 0)
}

// resumeJob continues a parked child (ctrl+o while a job is suspended).
func (m Model) resumeJob() tea.Cmd {
	j := m.suspendedJob
	return m.launchJob(j.what, nil, j.dir, j.pid)
}

// launchJob runs cmd under job control (resumePid==0) or resumes an existing
// stopped child (resumePid>0). Without job-control support it falls back to a
// plain blocking run.
func (m Model) launchJob(name string, cmd *exec.Cmd, dir string, resumePid int) tea.Cmd {
	if !mux.JobSupported() {
		return tea.Exec(ttyExec{cmd}, func(err error) tea.Msg {
			return externalDoneMsg{what: name, err: err}
		})
	}
	je := &jobExec{cmd: cmd, resumePid: resumePid}
	return tea.Exec(je, func(err error) tea.Msg {
		return externalDoneMsg{what: name, err: err, suspended: je.stopped, pid: je.pid, dir: dir}
	})
}

// onExternalDone updates job tracking and surfaces launch failures.
func (m Model) onExternalDone(msg externalDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.lastErr = msg.what + ": " + msg.err.Error()
		m.state = StateError
		m.suspendedJob = nil
		return m, nil
	}
	if msg.suspended {
		m.suspendedJob = &suspendedJob{pid: msg.pid, what: msg.what, dir: msg.dir}
		m.messages = append(m.messages, ephemeral(msg.what+" parked — ctrl+o to resume"))
		return m, m.flush()
	}
	m.suspendedJob = nil
	return m, nil
}

// jobExec is a tea.ExecCommand that runs (or resumes) a child under job
// control, toggling bee's keyboard-protocol modes off for the child. SetStd*
// are no-ops: ExecCmd already wired the real terminal.
type jobExec struct {
	cmd       *exec.Cmd
	resumePid int
	stopped   bool // child was Ctrl-Z'd (still alive) rather than exited
	pid       int
}

func (j *jobExec) Run() error {
	io.WriteString(os.Stdout, modifyOtherKeysDisable)
	defer io.WriteString(os.Stdout, modifyOtherKeysEnable)
	fd := os.Stdin.Fd()
	if j.resumePid != 0 {
		stopped, err := mux.ResumeJob(j.resumePid, fd)
		j.stopped = stopped
		j.pid = j.resumePid
		return err
	}
	stopped, pid, err := mux.StartJob(j.cmd, fd)
	j.stopped, j.pid = stopped, pid
	return err
}

func (*jobExec) SetStdin(io.Reader)  {}
func (*jobExec) SetStdout(io.Writer) {}
func (*jobExec) SetStderr(io.Writer) {}

// ttyExec is the plain (non-job-control) fallback: hand the child the real
// terminal and toggle keyboard-protocol modes off for its duration.
type ttyExec struct{ cmd *exec.Cmd }

func (t ttyExec) Run() error {
	io.WriteString(os.Stdout, modifyOtherKeysDisable)
	err := t.cmd.Run()
	io.WriteString(os.Stdout, modifyOtherKeysEnable)
	return err
}

func (ttyExec) SetStdin(io.Reader)  {}
func (ttyExec) SetStdout(io.Writer) {}
func (ttyExec) SetStderr(io.Writer) {}
