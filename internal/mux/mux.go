// Package mux delegates external commands (editor, shell, any program) to
// tmux windows so bee keeps running live in its own window while the command
// runs independently in a tab. No terminal handoff: tmux new-window returns
// instantly and the caller's process is untouched.
package mux

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Opts describes a window to open.
type Opts struct {
	Name string // tmux window name (also used to reuse an existing window)
	Dir  string // working directory (-c)
	Cmd  string // command line run in the window
}

// InTmux reports whether the current process runs inside a tmux client.
func InTmux() bool { return os.Getenv("TMUX") != "" }

// ResolveEditor picks the editor: explicit cfg value, else $VISUAL, else
// $EDITOR, else vim.
func ResolveEditor(cfg string) string {
	for _, c := range []string{cfg, os.Getenv("VISUAL"), os.Getenv("EDITOR")} {
		if strings.TrimSpace(c) != "" {
			return c
		}
	}
	return "vim"
}

// EditorCommand builds the command line opening file in editor, jumping to
// line when >0. Empty file opens the directory (".").
func EditorCommand(editor, file string, line int) string {
	target := "."
	if file != "" {
		target = shellQuote(file)
	}
	if line > 0 {
		return editor + " +" + strconv.Itoa(line) + " " + target
	}
	return editor + " " + target
}

// windowArgs returns the tmux subcommand args: select-window when a window
// named name already exists (reuse), else new-window.
func windowArgs(name, dir, cmd string, existing []string) []string {
	for _, w := range existing {
		if w == name {
			return []string{"select-window", "-t", name}
		}
	}
	return []string{"new-window", "-n", name, "-c", dir, cmd}
}

// OpenWindow opens (or focuses) a tmux window per o.
func OpenWindow(o Opts) error {
	args := windowArgs(o.Name, o.Dir, o.Cmd, listWindows())
	return exec.Command("tmux", args...).Run()
}

// ExecCmd builds a foreground *exec.Cmd running cmdline via the shell in dir.
// Used for the suspend fallback when not inside tmux (tea.ExecProcess).
// cmdline is operator-typed (same trust model as the inline ! shell), so the
// shell interpolation here is intended, not an injection surface.
func ExecCmd(cmdline, dir string) *exec.Cmd {
	c := exec.Command("sh", "-c", cmdline)
	c.Dir = dir
	return c
}

// listWindows returns current tmux window names, empty on any error.
func listWindows() []string {
	out, err := exec.Command("tmux", "list-windows", "-F", "#{window_name}").Output()
	if err != nil {
		return nil
	}
	var names []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			names = append(names, l)
		}
	}
	return names
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
