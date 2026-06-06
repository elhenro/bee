package commands

import "context"

// registerExternal adds /edit, /vim, and /term. These are intercepted in the
// TUI (runSlash) to launch a tmux window or suspend-and-run; registration here
// gives them palette discovery and lets them fire mid-turn (AllowDuringRun) so
// the editor opens while bee is still generating. Run is the headless fallback.
func registerExternal(r *Registry) {
	hint := func(_ context.Context, _ []string, _ Side) (string, error) {
		return "external commands run in the TUI only", nil
	}
	r.Register(Command{
		Name:           "edit",
		Description:    "open file in editor (tmux window): /edit [path[:line]]",
		AllowDuringRun: true,
		Run:            hint,
	})
	r.Register(Command{
		Name:           "vim",
		Description:    "open editor on cwd or file: /vim [path[:line]]",
		AllowDuringRun: true,
		Run:            hint,
	})
	r.Register(Command{
		Name:           "term",
		Description:    "run a command in a tmux window: /term [cmd]",
		AllowDuringRun: true,
		Run:            hint,
	})
}
